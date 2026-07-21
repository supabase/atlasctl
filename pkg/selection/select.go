package selection

import (
	"context"
	"encoding/binary"
	"encoding/hex"
	"hash/fnv"
	"math"
	"sort"

	h3 "github.com/ThingsIXFoundation/h3-light"

	"github.com/supabase/atlasctl/pkg/config"
	"github.com/supabase/atlasctl/pkg/snapshot"
)

// SelectedCohort holds the output for one cohort tier after selection.
type SelectedCohort struct {
	Measurement string
	Cohort      config.MeasurementCohort
	Probes      []snapshot.Probe
}

// Probes is an immutable, hashed probe set. Call Append until the full set
// is loaded, then call Close. After Close, CacheKey and Slice are valid;
// further Appends panic.
type Probes struct {
	probes   []snapshot.Probe
	cacheKey string
	closed   bool
}

// NewProbes returns an empty Probes with the given initial capacity.
func NewProbes(capacity int) *Probes {
	return &Probes{probes: make([]snapshot.Probe, 0, capacity)}
}

// Append adds a probe to the set. Panics if called after Close.
func (p *Probes) Append(probe snapshot.Probe) {
	if p.closed {
		panic("selection.Probes: Append called after Close")
	}
	p.probes = append(p.probes, probe)
}

// Close sorts the probe set by ID and computes the cache key. After Close,
// Append panics and CacheKey/Slice are valid.
func (p *Probes) Close() {
	sort.Slice(p.probes, func(i, j int) bool {
		return p.probes[i].ID < p.probes[j].ID
	})
	h := fnv.New64a()
	var buf [4]byte
	for _, probe := range p.probes {
		binary.BigEndian.PutUint32(buf[:], probe.ID)
		h.Write(buf[:])
	}
	p.cacheKey = hex.EncodeToString(h.Sum(nil))
	p.closed = true
}

// CacheKey returns the stable hash of this probe set. Valid after Close.
func (p *Probes) CacheKey() string {
	return p.cacheKey
}

// Slice returns the probe slice in ID-sorted order. Valid after Close.
// Callers must not modify the returned slice.
func (p *Probes) Slice() []snapshot.Probe {
	return p.probes
}

// ProbeOrderer produces a total ordering of probes for a given cohort config.
// The probe at index 0 is considered first during selection. The orderer is a
// pure function: it has no knowledge of count, exclusions, or H3 cell state.
type ProbeOrderer func(probes *Probes, cfg config.CohortCfg) []snapshot.Probe

// NewDefaultOrderer constructs the standard orderer. It uses NewDefaultWeighter
// to score probes, sorts by (band DESC, hash ASC, ID ASC), then optionally
// applies continental interleaving based on cfg.DisableContinentalShuffle.
// Results are cached in memory per (probes.CacheKey(), cfg.CacheKey()).
//
// h3Resolution is closed over at construction time as global geographic config.
func NewDefaultOrderer(h3Resolution int) ProbeOrderer {
	_ = h3Resolution // reserved for future H3-based ordering knobs
	type cacheKey struct{ probes, cfg string }
	cache := make(map[cacheKey][]snapshot.Probe)
	w := NewDefaultWeighter()

	return func(probes *Probes, cfg config.CohortCfg) []snapshot.Probe {
		k := cacheKey{probes.CacheKey(), cfg.CacheKey()}
		if cached, ok := cache[k]; ok {
			return cached
		}

		ps := probes.Slice()
		candidates := make([]candidate, len(ps))
		for i, p := range ps {
			score := w(p, cfg)
			band, hash := SortKey(p, score, cfg.BandThresholds.Effective())
			candidates[i] = candidate{
				probe: p,
				band:  band,
				hash:  hash,
			}
		}

		sort.Slice(candidates, func(i, j int) bool {
			a, b := candidates[i], candidates[j]
			if a.band != b.band {
				return a.band > b.band
			}
			if a.hash != b.hash {
				return a.hash < b.hash
			}
			return a.probe.ID < b.probe.ID
		})

		if !cfg.DisableContinentalShuffle {
			candidates = interleaveContinents(candidates)
		}

		result := make([]snapshot.Probe, len(candidates))
		for i, c := range candidates {
			result[i] = c.probe
		}
		cache[k] = result
		return result
	}
}

// Select runs cohort selection for one measurement's cohort list against a
// pre-built Probes set.
//
// Cohorts are processed in definition order. Each cohort depletes a shared
// excluded set before the next cohort runs. IncludeProbeIDs are placed first
// (up to ProbeCount) then the orderer fills remaining slots. ExcludeProbeIDs
// are skipped silently throughout. Included probes bypass H3 cell capacity.
//
// H3 cell capacity enforcement uses cohort.Cfg.Cities for per-cell density
// coefficients. h3Resolution is the global geo_diversity setting.
//
// probes must be closed before calling Select.
func Select(
	ctx context.Context,
	probes *Probes,
	cohorts []config.MeasurementCohort,
	orderer ProbeOrderer,
	h3Resolution int,
) ([]SelectedCohort, error) {
	// Build a probe map by ID for O(1) IncludeProbeIDs lookup.
	probeByID := make(map[uint32]snapshot.Probe, len(probes.Slice()))
	for _, p := range probes.Slice() {
		probeByID[p.ID] = p
	}

	// interCohortExcluded tracks IDs consumed by earlier cohorts in this run.
	interCohortExcluded := make(map[uint32]struct{})
	results := make([]SelectedCohort, 0, len(cohorts))

	for _, cohort := range cohorts {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		// cohortExcluded = inter-cohort excluded ∪ cohort.ExcludeProbeIDs.
		cohortExcluded := make(map[uint32]struct{},
			len(interCohortExcluded)+len(cohort.ExcludeProbeIDs))
		for id := range interCohortExcluded {
			cohortExcluded[id] = struct{}{}
		}
		for _, id := range cohort.ExcludeProbeIDs {
			cohortExcluded[id] = struct{}{}
		}

		// Build per-cohort cell density coefficient map from cohort.Cfg.Cities.
		cellCoef := make(map[h3.Cell]float64)
		for _, p := range probes.Slice() {
			cell := h3.LatLonToCell(p.Lat, p.Lon, h3Resolution)
			coef := maxCityCoef(p, cohort.Cfg.Cities)
			if coef > cellCoef[cell] {
				cellCoef[cell] = coef
			}
		}

		occ := NewH3Occupancy()
		selected := make([]snapshot.Probe, 0, cohort.ProbeCount)
		remaining := cohort.ProbeCount

		// Process IncludeProbeIDs first. They bypass H3 cell capacity.
		for _, id := range cohort.IncludeProbeIDs {
			if remaining <= 0 {
				break
			}
			if _, excluded := cohortExcluded[id]; excluded {
				continue
			}
			p, ok := probeByID[id]
			if !ok {
				continue
			}
			selected = append(selected, p)
			cohortExcluded[id] = struct{}{}
			remaining--
		}

		// Fill remaining slots from the ordered probe list.
		if remaining > 0 {
			ordered := orderer(probes, cohort.Cfg)
			for _, p := range ordered {
				if remaining <= 0 {
					break
				}
				if _, excluded := cohortExcluded[p.ID]; excluded {
					continue
				}
				cell := h3.LatLonToCell(p.Lat, p.Lon, h3Resolution)
				cap := cellCapacity(cohort.MaxProbesPerCell, cellCoef[cell])
				if occ.Count(cell) >= cap {
					continue
				}
				selected = append(selected, p)
				occ.Add(cell)
				cohortExcluded[p.ID] = struct{}{}
				remaining--
			}
		}

		// Add all selected IDs to inter-cohort excluded set for subsequent cohorts.
		for _, p := range selected {
			interCohortExcluded[p.ID] = struct{}{}
		}

		results = append(results, SelectedCohort{Cohort: cohort, Probes: selected})
	}

	return results, nil
}

// H3Occupancy tracks how many probes have been placed in each H3 cell
// during a single cohort. It is reset between cohorts.
type H3Occupancy struct {
	probeCounts map[h3.Cell]int
}

// NewH3Occupancy returns an empty occupancy tracker.
func NewH3Occupancy() *H3Occupancy {
	return &H3Occupancy{probeCounts: make(map[h3.Cell]int)}
}

// Count returns the number of probes already assigned to cell.
func (o *H3Occupancy) Count(cell h3.Cell) int {
	return o.probeCounts[cell]
}

// Add increments the count for cell.
func (o *H3Occupancy) Add(cell h3.Cell) {
	o.probeCounts[cell]++
}

// candidate is the pre-computed scoring representation of a probe used during
// ordering. The cell and coef fields are unused by the orderer; only probe,
// band, and hash are needed for sorting and interleaving.
type candidate struct {
	probe snapshot.Probe
	band  Band
	hash  uint64
	// cell  h3.Cell // unused by orderer; reserved for future use
	// coef  float64 // unused by orderer; reserved for future use
}

// cellCapacity returns the maximum number of probes allowed in a cell for one
// cohort. coef scales the base maximum: >1.0 relaxes it, <1.0 tightens it.
// The result is always at least 1 so a city never blocks a region entirely.
func cellCapacity(baseMax int, coef float64) int {
	if coef == 1.0 {
		return baseMax
	}
	n := int(math.Ceil(float64(baseMax) * coef))
	if n < 1 {
		return 1
	}
	return n
}

// interleaveContinents reorders a sorted candidate slice so that within each
// band tier, probes are distributed across geographic zones in round-robin
// order before any zone gets a second pick.
//
// Input must already be sorted by (Band DESC, hash ASC) — as produced by
// NewDefaultOrderer. The function preserves that per-zone order within each band.
//
// Example with Band-A candidates NA=[1,2,3], EU=[4,5,6], APAC=[7], LATAM=[8,9]:
//
//	pass 0: NA-1, EU-4, APAC-7, LATAM-8
//	pass 1: NA-2, EU-5,         LATAM-9
//	pass 2: NA-3, EU-6
//
// The H3 cell filter in Select is applied to this reordered slice unchanged.
func interleaveContinents(candidates []candidate) []candidate {
	type zbKey struct {
		zone Zone
		band Band
	}
	buckets := make(map[zbKey][]candidate, len(zoneOrder)*4)
	for _, c := range candidates {
		k := zbKey{ZoneOf(c.probe.CountryCode), c.band}
		buckets[k] = append(buckets[k], c)
	}

	result := make([]candidate, 0, len(candidates))

	for _, band := range []Band{BandA, BandB, BandC, BandD} {
		maxDepth := 0
		for _, z := range zoneOrder {
			if n := len(buckets[zbKey{z, band}]); n > maxDepth {
				maxDepth = n
			}
		}
		for pass := 0; pass < maxDepth; pass++ {
			for _, z := range zoneOrder {
				k := zbKey{z, band}
				if pass < len(buckets[k]) {
					result = append(result, buckets[k][pass])
				}
			}
		}
	}

	return result
}
