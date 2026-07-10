package selection_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/supabase/atlasctl/pkg/config"
	"github.com/supabase/atlasctl/pkg/selection"
	"github.com/supabase/atlasctl/pkg/snapshot"
)

// referenceScoringConfig returns the scoring config from the design docs,
// used as the baseline for most scoring tests.
func referenceScoringConfig() config.ScoringConfig {
	return config.ScoringConfig{
		ASN: map[uint32]int{
			7018:  10, // AT&T
			7922:  8,  // Comcast
			28573: 8,  // Claro Brazil
		},
		Tags: map[string]int{
			"office":     5,
			"datacentre": 4,
			"fibre":      2,
			"cable":      2,
			"lte":        3,
			"home":       1,
		},
		Countries: map[string]int{
			"BR": 5,
			"US": 1,
			"DE": 2,
			"JP": 3,
		},
		Stability: map[string]int{
			"system-ipv4-stable-90d": 5,
			"system-ipv4-stable-30d": 3,
		},
	}
}

func TestScore(t *testing.T) {
	cfg := referenceScoringConfig()

	tests := []struct {
		name  string
		probe snapshot.Probe
		cfg   config.ScoringConfig
		want  int
	}{
		{
			name:  "base only — empty config, no weights",
			probe: snapshot.Probe{ID: 1, ASN4: 9999, CountryCode: "XX", Tags: nil},
			cfg:   config.ScoringConfig{},
			want:  1,
		},
		{
			name:  "ASN match adds weight",
			probe: snapshot.Probe{ID: 1, ASN4: 7018},
			cfg:   cfg,
			want:  1 + 10, // base + AT&T
		},
		{
			name:  "ASN not in map adds nothing",
			probe: snapshot.Probe{ID: 1, ASN4: 9999},
			cfg:   cfg,
			want:  1,
		},
		{
			name:  "ASN4=0 with no map entry adds nothing",
			probe: snapshot.Probe{ID: 1, ASN4: 0},
			cfg:   cfg,
			want:  1,
		},
		{
			name:  "country match adds weight",
			probe: snapshot.Probe{ID: 1, CountryCode: "BR"},
			cfg:   cfg,
			want:  1 + 5, // base + BR
		},
		{
			name:  "country not in map adds nothing",
			probe: snapshot.Probe{ID: 1, CountryCode: "ZZ"},
			cfg:   cfg,
			want:  1,
		},
		{
			name:  "single tag match (Tags map)",
			probe: snapshot.Probe{ID: 1, Tags: []string{"office"}},
			cfg:   cfg,
			want:  1 + 5,
		},
		{
			name:  "single stability tag match",
			probe: snapshot.Probe{ID: 1, Tags: []string{"system-ipv4-stable-90d"}},
			cfg:   cfg,
			want:  1 + 5,
		},
		{
			name:  "tag not in any map adds nothing",
			probe: snapshot.Probe{ID: 1, Tags: []string{"unknown-tag"}},
			cfg:   cfg,
			want:  1,
		},
		{
			name:  "multiple tags all matching",
			probe: snapshot.Probe{ID: 1, Tags: []string{"office", "fibre"}},
			cfg:   cfg,
			want:  1 + 5 + 2, // base + office + fibre
		},
		{
			name:  "multiple stability tags both matching",
			probe: snapshot.Probe{ID: 1, Tags: []string{"system-ipv4-stable-90d", "system-ipv4-stable-30d"}},
			cfg:   cfg,
			want:  1 + 5 + 3,
		},
		{
			name: "tag in both Tags and Stability maps earns both weights",
			probe: snapshot.Probe{ID: 1, Tags: []string{"dual"}},
			cfg: config.ScoringConfig{
				Tags:      map[string]int{"dual": 7},
				Stability: map[string]int{"dual": 3},
			},
			want: 1 + 7 + 3,
		},
		{
			name:  "partial tag match — only matched tags contribute",
			probe: snapshot.Probe{ID: 1, Tags: []string{"office", "unknown-tag", "lte"}},
			cfg:   cfg,
			want:  1 + 5 + 3, // base + office + lte
		},
		{
			// From the design docs: AT&T probe with office+fibre in the US.
			// 1 (base) + 10 (AT&T) + 5 (office) + 2 (fibre) + 1 (US) = 19
			name: "full example from design docs",
			probe: snapshot.Probe{
				ID:          42,
				ASN4:        7018,
				CountryCode: "US",
				Tags:        []string{"office", "fibre"},
			},
			cfg:  cfg,
			want: 19,
		},
		{
			name: "all axes combined",
			probe: snapshot.Probe{
				ID:          1,
				ASN4:        28573,
				CountryCode: "BR",
				Tags:        []string{"home", "cable", "system-ipv4-stable-30d"},
			},
			cfg:  cfg,
			want: 1 + 8 + 5 + 1 + 2 + 3, // base+Claro+BR+home+cable+stable-30d
		},
		{
			name: "negative weight in config is applied correctly",
			probe: snapshot.Probe{ID: 1, Tags: []string{"bad-tag"}},
			cfg: config.ScoringConfig{
				Tags: map[string]int{"bad-tag": -3},
			},
			want: 1 - 3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := selection.Score(tt.probe, tt.cfg)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestBand(t *testing.T) {
	thresholds := config.BandThresholds{A: 15, B: 8, C: 3}
	tests := []struct {
		score int
		want  selection.Band
	}{
		{score: 1, want: selection.BandD},
		{score: 2, want: selection.BandD},
		{score: 3, want: selection.BandC},
		{score: 7, want: selection.BandC},
		{score: 8, want: selection.BandB},
		{score: 14, want: selection.BandB},
		{score: 15, want: selection.BandA},
		{score: 100, want: selection.BandA},
		// Scores ≤0 (from negative config weights) land in BandD.
		{score: 0, want: selection.BandD},
		{score: -5, want: selection.BandD},
	}

	for _, tt := range tests {
		got := selection.AssignBand(tt.score, thresholds)
		assert.Equal(t, tt.want, got, "AssignBand(%d)", tt.score)
	}
}

func TestBand_CustomThresholds(t *testing.T) {
	thresholds := config.BandThresholds{A: 30, B: 20, C: 10}
	assert.Equal(t, selection.BandD, selection.AssignBand(9, thresholds))
	assert.Equal(t, selection.BandC, selection.AssignBand(10, thresholds))
	assert.Equal(t, selection.BandB, selection.AssignBand(20, thresholds))
	assert.Equal(t, selection.BandA, selection.AssignBand(30, thresholds))
}

func TestBand_Order(t *testing.T) {
	// BandA must sort higher than all others; BandD must sort lowest.
	assert.Greater(t, int(selection.BandA), int(selection.BandB))
	assert.Greater(t, int(selection.BandB), int(selection.BandC))
	assert.Greater(t, int(selection.BandC), int(selection.BandD))
}

func TestHardExcluded(t *testing.T) {
	excludes := []string{
		"broken",
		"system-flakey-connection",
		"system-flakey-power",
		"system-ipv4-doesnt-work",
	}

	tests := []struct {
		name  string
		probe snapshot.Probe
		want  bool
	}{
		{
			name:  "no tags — not excluded",
			probe: snapshot.Probe{Tags: nil},
			want:  false,
		},
		{
			name:  "irrelevant tags only — not excluded",
			probe: snapshot.Probe{Tags: []string{"office", "fibre"}},
			want:  false,
		},
		{
			name:  "broken tag — excluded",
			probe: snapshot.Probe{Tags: []string{"office", "broken"}},
			want:  true,
		},
		{
			name:  "system-flakey-connection — excluded",
			probe: snapshot.Probe{Tags: []string{"system-flakey-connection"}},
			want:  true,
		},
		{
			name:  "system-flakey-power — excluded",
			probe: snapshot.Probe{Tags: []string{"system-ipv4-stable-90d", "system-flakey-power"}},
			want:  true,
		},
		{
			name:  "system-ipv4-doesnt-work — excluded",
			probe: snapshot.Probe{Tags: []string{"system-ipv4-doesnt-work", "home"}},
			want:  true,
		},
		{
			name:  "empty exclude list — never excluded",
			probe: snapshot.Probe{Tags: []string{"broken", "system-flakey-connection"}},
			want:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ex := excludes
			if tt.name == "empty exclude list — never excluded" {
				ex = nil
			}
			got := selection.HardExcluded(tt.probe, ex)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestSortKey_Determinism(t *testing.T) {
	probe := snapshot.Probe{ID: 12345, ASN4: 7018, CountryCode: "US", Tags: []string{"office"}}
	cfg := referenceScoringConfig()
	score := selection.Score(probe, cfg)
	thresholds := config.BandThresholds{A: 15, B: 8, C: 3}

	band1, hash1 := selection.SortKey(probe, score, thresholds)
	band2, hash2 := selection.SortKey(probe, score, thresholds)

	assert.Equal(t, band1, band2, "Band must be deterministic")
	assert.Equal(t, hash1, hash2, "Hash must be deterministic")
}

func TestSortKey_DifferentProbes(t *testing.T) {
	// Two probes in the same band must have different hashes (collision is
	// possible but vanishingly unlikely for small probe sets).
	p1 := snapshot.Probe{ID: 1}
	p2 := snapshot.Probe{ID: 2}
	thresholds := config.BandThresholds{A: 15, B: 8, C: 3}

	_, h1 := selection.SortKey(p1, 1, thresholds)
	_, h2 := selection.SortKey(p2, 1, thresholds)

	assert.NotEqual(t, h1, h2, "distinct probe IDs should produce distinct hashes")
}

// BenchmarkScore measures scoring throughput for a 50k-probe snapshot.
// The benchmark must complete in <100ms (enforced by the plan's exit criteria).
func BenchmarkScore(b *testing.B) {
	cfg := referenceScoringConfig()

	// Build a representative probe population.
	probes := make([]snapshot.Probe, 50_000)
	for i := range probes {
		probes[i] = snapshot.Probe{
			ID:          uint32(i + 1),
			ASN4:        uint32(7000 + (i % 10)),
			CountryCode: []string{"US", "DE", "BR", "JP", "GB"}[i%5],
			Tags: []string{
				[]string{"office", "home", "datacentre", "lte", ""}[i%5],
				[]string{"fibre", "cable", "", "system-ipv4-stable-90d", ""}[i%5],
			},
		}
	}

	b.ResetTimer()
	for range b.N {
		for _, p := range probes {
			_ = selection.Score(p, cfg)
		}
	}
}
