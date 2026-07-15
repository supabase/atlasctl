// Package selection implements probe scoring, band assignment, and
// multi-round probe selection with H3 geographic diversity.
package selection

import (
	"encoding/binary"
	"hash/fnv"
	"math"

	"github.com/supabase/atlasctl/pkg/config"
	"github.com/supabase/atlasctl/pkg/snapshot"
)

// Band represents a score tier used for deterministic probe ordering.
// Higher values sort first: BandA > BandB > BandC > BandD.
type Band int

const (
	BandD Band = 1 // score 1–2
	BandC Band = 2 // score 3–7
	BandB Band = 3 // score 8–14
	BandA Band = 4 // score 15+
)

// String returns the letter label for the band, for display purposes.
func (b Band) String() string {
	switch b {
	case BandA:
		return "A"
	case BandB:
		return "B"
	case BandC:
		return "C"
	default:
		return "D"
	}
}

// ProbeWeighter returns the numeric priority of a single probe given the
// cohort config. It is a pure function: stateless, no knowledge of other
// probes, no knowledge of how many to select or which are excluded.
// Higher weight means higher priority.
type ProbeWeighter func(probe snapshot.Probe, cfg config.CohortCfg) int

// NewDefaultWeighter returns the standard additive weighter. It combines
// ScoringConfig weights (ASN, country, tag, stability) with geographic city
// score bonuses from cfg.Cities.
func NewDefaultWeighter() ProbeWeighter {
	return func(p snapshot.Probe, cfg config.CohortCfg) int {
		return Score(p, cfg.ScoringConfig) + cityScoreBonus(p, cfg.Cities)
	}
}

// CombineWeighters returns a ProbeWeighter that sums the results of all
// provided weighters. Useful for composing independent weighting strategies.
func CombineWeighters(ws ...ProbeWeighter) ProbeWeighter {
	return func(p snapshot.Probe, cfg config.CohortCfg) int {
		total := 0
		for _, w := range ws {
			total += w(p, cfg)
		}
		return total
	}
}

// Score computes the additive score for probe p given the scoring config.
//
// The formula is:
//
//	1 (base) + ASN weight + country weight + sum(tag weights) + sum(stability weights)
//
// All four contribution axes are independent. Tags and stability slugs are
// both matched against the probe's tag list, so a tag slug should earn weight
// from at most one of the two maps
//
// config validation rejects overlapping slugs.
func Score(p snapshot.Probe, cfg config.ScoringConfig) int {
	score := 1 // every connected probe starts from 1

	if w, ok := cfg.ASN[p.ASN4]; ok {
		score += w
	}

	if w, ok := cfg.Countries[p.CountryCode]; ok {
		score += w
	}

	for _, tag := range p.Tags {
		if w, ok := cfg.Tags[tag]; ok {
			score += w
		}
		if w, ok := cfg.Stability[tag]; ok {
			score += w
		}
	}

	return score
}

// AssignBand maps an integer score to a Band tier using the provided thresholds.
// Scores below t.C fall into BandD to handle unexpected negative weights.
func AssignBand(score int, t config.BandThresholds) Band {
	switch {
	case score >= t.A:
		return BandA
	case score >= t.B:
		return BandB
	case score >= t.C:
		return BandC
	default:
		return BandD
	}
}

// SortKey returns the (Band, hash) pair used to impose a stable, deterministic
// ordering on candidates. Probes are sorted by Band descending, then by hash
// ascending. The hash is FNV-1a over the probe ID, which is permanent and
// unique — so the tiebreaker is stable across snapshots as long as the probe
// population doesn't change.
func SortKey(p snapshot.Probe, score int, t config.BandThresholds) (Band, uint64) {
	return AssignBand(score, t), probeHash(p.ID)
}

// probeHash returns a 64-bit FNV-1a hash of the probe ID.
// FNV-1a is fast, has good avalanche, and is trivially reproducible.
func probeHash(id uint32) uint64 {
	h := fnv.New64a()
	var b [4]byte
	binary.BigEndian.PutUint32(b[:], id)
	_, _ = h.Write(b[:])
	return h.Sum64()
}

// HardExcluded reports whether p carries any tag that appears in excludeTags.
// Probes that match are never selected, regardless of score.
func HardExcluded(p snapshot.Probe, excludeTags []string) bool {
	for _, tag := range p.Tags {
		for _, ex := range excludeTags {
			if tag == ex {
				return true
			}
		}
	}
	return false
}

// cityScoreBonus returns the sum of score weights from all cities whose radius
// covers probe p. Returns 0 if no city applies or all matching cities have Score=0.
func cityScoreBonus(p snapshot.Probe, cities []config.CityConfig) int {
	bonus := 0
	for _, city := range cities {
		if city.Score != 0 && haversineKm(p.Lat, p.Lon, city.Lat, city.Lon) <= city.RadiusKm {
			bonus += city.Score
		}
	}
	return bonus
}

// maxCityCoef returns the density coefficient to apply to probe p. If the probe
// falls within multiple city radii, the highest coefficient wins (boost takes
// priority). Returns 1.0 if no city applies.
func maxCityCoef(p snapshot.Probe, cities []config.CityConfig) float64 {
	coef := 1.0
	matched := false
	for _, city := range cities {
		if haversineKm(p.Lat, p.Lon, city.Lat, city.Lon) <= city.RadiusKm {
			if !matched || city.DensityCoefficient > coef {
				coef = city.DensityCoefficient
			}
			matched = true
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
