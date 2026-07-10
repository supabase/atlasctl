// Package selection implements probe scoring, band assignment, and
// multi-round probe selection with H3 geographic diversity.
package selection

import (
	"encoding/binary"
	"hash/fnv"

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

// Score computes the additive score for probe p given the scoring config.
//
// The formula is:
//
//	1 (base) + ASN weight + country weight + sum(tag weights) + sum(stability weights)
//
// All four contribution axes are independent. Tags and stability slugs are
// both matched against the probe's tag list, so a tag slug can earn weight
// from at most one of the two maps (they are conventionally disjoint sets).
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
