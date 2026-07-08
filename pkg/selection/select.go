package selection

import (
	"context"
	"math"
	"sort"

	h3 "github.com/ThingsIXFoundation/h3-light"

	"github.com/supabase/atlascli/pkg/config"
	"github.com/supabase/atlascli/pkg/snapshot"
)

// SelectedRound is the output of one round of probe selection.
type SelectedRound struct {
	Round  config.Round
	Probes []snapshot.Probe
}

// H3Occupancy tracks how many probes have been placed in each H3 cell
// during a single round. It is reset between rounds.
type H3Occupancy struct {
	counts map[h3.Cell]int
}

// NewH3Occupancy returns an empty occupancy tracker.
func NewH3Occupancy() *H3Occupancy {
	return &H3Occupancy{counts: make(map[h3.Cell]int)}
}

// Count returns the number of probes already assigned to cell.
func (o *H3Occupancy) Count(cell h3.Cell) int {
	return o.counts[cell]
}

// Add increments the count for cell.
func (o *H3Occupancy) Add(cell h3.Cell) {
	o.counts[cell]++
}

// candidate is the pre-computed representation of a probe used during selection.
type candidate struct {
	probe snapshot.Probe
	band  Band
	hash  uint64
	cell  h3.Cell
	coef  float64 // effective density coefficient from city overrides; ≥1.0
}

// Select runs the multi-round probe selection algorithm against snap using the
// parameters in cfg. Each probe appears in at most one round (no overlap).
//
// The algorithm:
//  1. Filter hard-excluded probes.
//  2. Score remaining probes and sort by (Band DESC, hash ASC, ID ASC).
//  3. Pre-compute per-cell density coefficient from city config.
//  4. For each round, walk the sorted list in order, skipping already-selected
//     probes and H3 cells that have reached capacity, until the round target is met.
//
// Rounds are processed in the order they appear in cfg. Context cancellation is
// checked between rounds.
func Select(ctx context.Context, snap snapshot.Snapshot, cfg config.Config) ([]SelectedRound, error) {
	res := cfg.GeoDiversity.H3Resolution

	// Step 1+2: build candidates, filter excluded, score, sort.
	candidates := buildCandidates(snap.Probes, cfg, res)

	// Step 3: pre-compute per-cell max density coefficient.
	// A cell inherits the highest coefficient of any probe whose coordinates
	// fall within a city's radius. This is a proxy for the cell center: at
	// resolution 3 (~12 000 km² cells, ~60 km edge) the difference is small.
	cellCoef := make(map[h3.Cell]float64, len(candidates))
	for _, c := range candidates {
		if c.coef > cellCoef[c.cell] {
			cellCoef[c.cell] = c.coef
		}
	}

	// Step 4: round-by-round selection.
	selected := make(map[uint32]struct{}, len(candidates))
	rounds := make([]SelectedRound, 0, len(cfg.Rounds))

	for _, roundCfg := range cfg.Rounds {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		occ := NewH3Occupancy()
		probes := make([]snapshot.Probe, 0, roundCfg.Count)

		for i := range candidates {
			if len(probes) >= roundCfg.Count {
				break
			}
			cand := &candidates[i]
			if _, seen := selected[cand.probe.ID]; seen {
				continue
			}
			cap := cellCapacity(roundCfg.MaxProbesPerCell, cellCoef[cand.cell])
			if occ.Count(cand.cell) >= cap {
				continue
			}
			probes = append(probes, cand.probe)
			occ.Add(cand.cell)
			selected[cand.probe.ID] = struct{}{}
		}

		rounds = append(rounds, SelectedRound{Round: roundCfg, Probes: probes})
	}

	return rounds, nil
}

// buildCandidates filters hard-excluded probes, computes per-probe scoring
// metadata, and returns a sorted candidate slice.
func buildCandidates(probes []snapshot.Probe, cfg config.Config, res int) []candidate {
	candidates := make([]candidate, 0, len(probes))
	for _, p := range probes {
		if HardExcluded(p, cfg.ExcludeTags) {
			continue
		}
		score := Score(p, cfg.Scoring)
		band, hash := SortKey(p, score)
		candidates = append(candidates, candidate{
			probe: p,
			band:  band,
			hash:  hash,
			cell:  h3.LatLonToCell(p.Lat, p.Lon, res),
			coef:  maxCityCoef(p, cfg.Cities),
		})
	}

	// Sort: Band DESC, hash ASC, probe ID ASC (total order — no undefined ties).
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

	return candidates
}

// cellCapacity returns the maximum number of probes allowed in a cell for one
// round. If coef > 1.0 (city density override), the base maximum is scaled up
// using ceiling division.
func cellCapacity(baseMax int, coef float64) int {
	if coef <= 1.0 {
		return baseMax
	}
	return int(math.Ceil(float64(baseMax) * coef))
}

// maxCityCoef returns the highest density coefficient from any city whose
// radius covers probe p. Returns 1.0 if no city applies.
func maxCityCoef(p snapshot.Probe, cities []config.CityConfig) float64 {
	coef := 1.0
	for _, city := range cities {
		if haversineKm(p.Lat, p.Lon, city.Lat, city.Lon) <= city.RadiusKm {
			if city.DensityCoefficient > coef {
				coef = city.DensityCoefficient
			}
		}
	}
	return coef
}

// haversineKm returns the great-circle distance in kilometres between two
// (lat, lon) coordinate pairs using the haversine formula.
func haversineKm(lat1, lon1, lat2, lon2 float64) float64 {
	const earthRadiusKm = 6371.0

	dLat := (lat2 - lat1) * math.Pi / 180
	dLon := (lon2 - lon1) * math.Pi / 180

	a := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(lat1*math.Pi/180)*math.Cos(lat2*math.Pi/180)*
			math.Sin(dLon/2)*math.Sin(dLon/2)

	return earthRadiusKm * 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
}
