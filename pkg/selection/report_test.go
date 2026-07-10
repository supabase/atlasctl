package selection_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/supabase/atlasctl/pkg/config"
	"github.com/supabase/atlasctl/pkg/selection"
	"github.com/supabase/atlasctl/pkg/snapshot"
)

// knownProbes returns 10 probes with a fully deterministic distribution across
// countries, ASNs, and tags so TestReport_Counts can assert exact bucket values.
//
// Distribution:
//   - 3 probes: US / ASN 7018 / tags: office, fibre, system-ipv4-stable-90d
//     score = 1+10+5+2+5+1 = 24 → Band A
//   - 4 probes: BR / ASN 28573 / tags: home
//     score = 1+8+5+1 = 15 → Band A  (threshold: ≥15)
//   - 3 probes: DE / ASN 1111 / tags: (none)
//     score = 1+0+2 = 3 → Band C
//
// Probes are spread across distinct geographic areas so H3 cells are unique.
func knownProbes() []snapshot.Probe {
	return []snapshot.Probe{
		// US probes — spread across eastern, central, western US
		{ID: 1, ASN4: 7018, CountryCode: "US", Tags: []string{"office", "fibre", "system-ipv4-stable-90d"}, Lat: 38.9, Lon: -77.0, StatusID: 1},
		{ID: 2, ASN4: 7018, CountryCode: "US", Tags: []string{"office", "fibre", "system-ipv4-stable-90d"}, Lat: 41.8, Lon: -87.6, StatusID: 1},
		{ID: 3, ASN4: 7018, CountryCode: "US", Tags: []string{"office", "fibre", "system-ipv4-stable-90d"}, Lat: 34.0, Lon: -118.2, StatusID: 1},
		// BR probes — spread across Brazil
		{ID: 4, ASN4: 28573, CountryCode: "BR", Tags: []string{"home"}, Lat: -23.5, Lon: -46.6, StatusID: 1},
		{ID: 5, ASN4: 28573, CountryCode: "BR", Tags: []string{"home"}, Lat: -15.8, Lon: -47.9, StatusID: 1},
		{ID: 6, ASN4: 28573, CountryCode: "BR", Tags: []string{"home"}, Lat: -3.7, Lon: -38.5, StatusID: 1},
		{ID: 7, ASN4: 28573, CountryCode: "BR", Tags: []string{"home"}, Lat: -30.0, Lon: -51.2, StatusID: 1},
		// DE probes — spread across Germany
		{ID: 8, ASN4: 1111, CountryCode: "DE", Tags: nil, Lat: 52.5, Lon: 13.4, StatusID: 1},
		{ID: 9, ASN4: 1111, CountryCode: "DE", Tags: nil, Lat: 48.1, Lon: 11.6, StatusID: 1},
		{ID: 10, ASN4: 1111, CountryCode: "DE", Tags: nil, Lat: 53.6, Lon: 10.0, StatusID: 1},
	}
}

func knownScoringCfg() config.ScoringConfig {
	return config.ScoringConfig{
		ASN: map[uint32]int{
			7018:  10,
			28573: 8,
		},
		Tags: map[string]int{
			"office": 5,
			"fibre":  2,
			"home":   1,
		},
		Countries: map[string]int{
			"US": 1,
			"BR": 5,
			"DE": 2,
		},
		Stability: map[string]int{
			"system-ipv4-stable-90d": 5,
		},
	}
}

// buildSingleRound runs selection with all 10 known probes in one round.
func buildSingleRound(t *testing.T, probes []snapshot.Probe) []selection.SelectedRound {
	t.Helper()
	cfg := config.Config{
		Rounds:       []config.Round{minimalRound("r1", len(probes), 5)},
		Measurements: []config.Measurement{{Name: "m", Type: "dns", Target: "x.com", Rounds: []string{"r1"}}},
		GeoDiversity: config.GeoConfig{H3Resolution: 3},
		Scoring:      knownScoringCfg(),
	}
	snap := snapshot.Snapshot{Probes: probes}

	rounds, err := selection.Select(context.Background(), snap, cfg)
	require.NoError(t, err)
	return rounds
}

func TestReport_Counts(t *testing.T) {
	probes := knownProbes()
	rounds := buildSingleRound(t, probes)
	scoring := knownScoringCfg()

	report := selection.Report(rounds, scoring)

	// Total and per-round counts.
	assert.Equal(t, 10, report.TotalProbes)
	require.Len(t, report.ProbesByRound, 1)
	assert.Equal(t, "r1", report.ProbesByRound[0].Round)
	assert.Equal(t, 10, report.ProbesByRound[0].Count)

	// Country histogram.
	assert.Equal(t, 3, report.Countries["US"], "US probe count")
	assert.Equal(t, 4, report.Countries["BR"], "BR probe count")
	assert.Equal(t, 3, report.Countries["DE"], "DE probe count")

	// ASN histogram.
	assert.Equal(t, 3, report.ASNs[7018], "ASN 7018 count")
	assert.Equal(t, 4, report.ASNs[28573], "ASN 28573 count")
	assert.Equal(t, 3, report.ASNs[1111], "ASN 1111 count")

	// Band histogram.
	// US: 1+10+1+5+2+5 = 24 → A; BR: 1+8+5+1 = 15 → A; DE: 1+2 = 3 → C
	assert.Equal(t, 7, report.Bands["A"], "Band A (US+BR probes)")
	assert.Equal(t, 3, report.Bands["C"], "Band C (DE probes with no ASN or tag weight beyond country)")

	// H3 cells — at least one unique cell per probe at every resolution.
	for _, res := range []int{2, 3, 4} {
		assert.Greater(t, report.UniqueH3Cells[res], 0, "resolution %d: should have at least one cell", res)
	}
}

func TestReport_H3Resolution(t *testing.T) {
	// Probes spread globally so that higher resolutions always subdivide into more cells.
	probes := spreadProbes(30)
	rounds := buildSingleRound(t, probes)
	scoring := config.ScoringConfig{}

	report := selection.Report(rounds, scoring)

	r2 := report.UniqueH3Cells[2]
	r3 := report.UniqueH3Cells[3]
	r4 := report.UniqueH3Cells[4]

	assert.Greater(t, r2, 0, "resolution 2 must have at least one cell")
	assert.GreaterOrEqual(t, r3, r2, "resolution 3 must have >= cells as resolution 2")
	assert.GreaterOrEqual(t, r4, r3, "resolution 4 must have >= cells as resolution 3")
}

func TestReport_MultipleRounds(t *testing.T) {
	// Two rounds of 5 probes each, no overlap.
	probes := spreadProbes(20)
	cfg := config.Config{
		Rounds: []config.Round{
			minimalRound("r1", 5, 5),
			minimalRound("r2", 5, 5),
		},
		Measurements: []config.Measurement{{Name: "m", Type: "dns", Target: "x.com", Rounds: []string{"r1"}}},
		GeoDiversity: config.GeoConfig{H3Resolution: 3},
	}
	snap := snapshot.Snapshot{Probes: probes}

	rounds, err := selection.Select(context.Background(), snap, cfg)
	require.NoError(t, err)

	report := selection.Report(rounds, config.ScoringConfig{})

	assert.Equal(t, 10, report.TotalProbes)
	require.Len(t, report.ProbesByRound, 2)
	assert.Equal(t, 5, report.ProbesByRound[0].Count)
	assert.Equal(t, 5, report.ProbesByRound[1].Count)
}

func TestReport_JSON(t *testing.T) {
	probes := knownProbes()
	rounds := buildSingleRound(t, probes)
	report := selection.Report(rounds, knownScoringCfg())

	data, err := report.JSON()
	require.NoError(t, err)

	var decoded map[string]any
	require.NoError(t, json.Unmarshal(data, &decoded))
	assert.Equal(t, float64(10), decoded["total_probes"])
	assert.NotNil(t, decoded["countries"])
	assert.NotNil(t, decoded["asns"])
	assert.NotNil(t, decoded["bands"])
	assert.NotNil(t, decoded["unique_h3_cells"])
}

func TestReport_Format(t *testing.T) {
	probes := knownProbes()
	rounds := buildSingleRound(t, probes)
	report := selection.Report(rounds, knownScoringCfg())

	text := report.Format()

	assert.True(t, strings.Contains(text, "Total probes: 10"), "total probes in text")
	assert.True(t, strings.Contains(text, "r1:"), "round name in text")
	assert.True(t, strings.Contains(text, "US:"), "country in text")
	assert.True(t, strings.Contains(text, "resolution 3:"), "H3 resolution in text")
	assert.True(t, strings.Contains(text, "A:"), "band A in text")
}

