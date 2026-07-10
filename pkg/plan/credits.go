package plan

import (
	"github.com/supabase/atlasctl/pkg/config"
)

// CreditLine is the credit burn breakdown for one (measurement, round) pair.
type CreditLine struct {
	Key          MsmKey
	Type         MsmType
	ProbeCount   int
	IntervalSecs int
	PerDay       int64
}

// CreditEstimate is the projected credit burn across all desired measurements.
type CreditEstimate struct {
	Lines   []CreditLine
	Daily   int64
	Weekly  int64
}

// EstimateCredits computes the projected daily and weekly RIPE Atlas credit
// burn for a desired state map, as returned by DesiredState.
//
// The formula per (measurement, round):
//
//	credits/day = probe_count × credits_per_result(type) × (86400 / interval_seconds)
//
// Lines is sorted by descending PerDay so the most expensive entries appear first.
func EstimateCredits(desired map[MsmKey]DesiredMsm) CreditEstimate {
	var est CreditEstimate
	for key, d := range desired {
		cpd := int64(len(d.ProbeIDs)) *
			int64(config.CreditsPerResult(config.MeasurementType(d.Type))) *
			86400 / int64(d.Interval)
		est.Lines = append(est.Lines, CreditLine{
			Key:          key,
			Type:         d.Type,
			ProbeCount:   len(d.ProbeIDs),
			IntervalSecs: d.Interval,
			PerDay:       cpd,
		})
		est.Daily += cpd
	}
	est.Weekly = est.Daily * 7

	// Sort descending by daily cost for consistent, human-readable output.
	for i := 1; i < len(est.Lines); i++ {
		for j := i; j > 0 && est.Lines[j].PerDay > est.Lines[j-1].PerDay; j-- {
			est.Lines[j], est.Lines[j-1] = est.Lines[j-1], est.Lines[j]
		}
	}

	return est
}
