package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/supabase/atlasctl/pkg/config"
)

// writeTemp writes yaml content to a temp file and returns its path.
func writeTemp(t *testing.T, content string) string {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "*.yaml")
	require.NoError(t, err)
	_, err = f.WriteString(content)
	require.NoError(t, err)
	require.NoError(t, f.Close())
	return f.Name()
}

func TestLoad_ValidFixture(t *testing.T) {
	path := filepath.Join("fixtures", "valid.yaml")
	cfg, err := config.Load(path)
	require.NoError(t, err)
	require.NotNil(t, cfg)

	assert.Len(t, cfg.Measurements, 3)
	assert.Equal(t, 3, cfg.GeoDiversity.H3Resolution)

	assert.Len(t, cfg.CohortConfigs, 2)
	standard := cfg.CohortConfigs["standard"]
	assert.Len(t, standard.Cities, 2)
	assert.False(t, standard.DisableContinentalShuffle)

	latam := cfg.CohortConfigs["latam-focus"]
	assert.True(t, latam.DisableContinentalShuffle)

	// Preset resolution: dns-canary high-freq should have standard cfg applied.
	dnsCohorts := cfg.Measurements[0].Cohorts
	require.Len(t, dnsCohorts, 3)
	assert.Equal(t, "high-freq", dnsCohorts[0].Name)
	assert.Equal(t, 60, dnsCohorts[0].IntervalSeconds)
	assert.Len(t, dnsCohorts[0].Cfg.Cities, 2, "standard preset cities should be resolved")
	assert.Empty(t, dnsCohorts[0].CfgPreset, "CfgPreset should be cleared after resolution")
}

func TestLoad_MissingFile(t *testing.T) {
	_, err := config.Load("/nonexistent/path/config.yaml")
	require.Error(t, err)
	assert.True(t, os.IsNotExist(err) || strings.Contains(err.Error(), "no such file"),
		"expected file-not-found error, got: %v", err)
}

// minimalValid is the smallest possible valid config. No global cohorts; cohorts
// live inside each measurement.
const minimalValid = `
measurements:
  - name: dns-test
    type: dns
    target: example.com
    cohorts:
      - name: fast
        probe_count: 10
        max_probes_per_cell: 1
        interval_seconds: 60
`

func TestLoad(t *testing.T) {
	tests := []struct {
		name        string
		yaml        string
		wantErr     bool
		errContains string
		check       func(t *testing.T, cfg *config.Config)
	}{
		{
			name: "minimal valid",
			yaml: minimalValid,
			check: func(t *testing.T, cfg *config.Config) {
				assert.Equal(t, 3, cfg.GeoDiversity.H3Resolution, "h3_resolution should default to 3")
			},
		},
		{
			name: "no measurements",
			yaml: ``,
		},
		{
			name: "duplicate cohort names within a measurement",
			yaml: `
measurements:
  - name: m
    type: dns
    target: x.com
    cohorts:
      - name: r1
        probe_count: 10
        max_probes_per_cell: 1
        interval_seconds: 60
      - name: r1
        probe_count: 20
        max_probes_per_cell: 2
        interval_seconds: 120
`,
			wantErr:     true,
			errContains: `duplicate cohort name "r1"`,
		},
		{
			name: "duplicate measurement names",
			yaml: `
measurements:
  - name: m
    type: dns
    target: x.com
    cohorts:
      - name: c1
        probe_count: 10
        max_probes_per_cell: 1
        interval_seconds: 60
  - name: m
    type: ping
    target: x.com
    cohorts:
      - name: c1
        probe_count: 10
        max_probes_per_cell: 1
        interval_seconds: 30
`,
			wantErr:     true,
			errContains: `duplicate measurement name "m"`,
		},
		{
			name: "zero interval_seconds on cohort",
			yaml: `
measurements:
  - name: m
    type: dns
    target: x.com
    cohorts:
      - name: r
        probe_count: 10
        max_probes_per_cell: 1
        interval_seconds: 0
`,
			wantErr:     true,
			errContains: "interval_seconds must be positive",
		},
		{
			name: "negative interval_seconds on cohort",
			yaml: `
measurements:
  - name: m
    type: dns
    target: x.com
    cohorts:
      - name: r
        probe_count: 10
        max_probes_per_cell: 1
        interval_seconds: -60
`,
			wantErr:     true,
			errContains: "interval_seconds must be positive",
		},
		{
			name: "zero probe_count",
			yaml: `
measurements:
  - name: m
    type: dns
    target: x.com
    cohorts:
      - name: r
        probe_count: 0
        max_probes_per_cell: 1
        interval_seconds: 60
`,
			wantErr:     true,
			errContains: "probe_count must be positive",
		},
		{
			name: "zero max_probes_per_cell",
			yaml: `
measurements:
  - name: m
    type: dns
    target: x.com
    cohorts:
      - name: r
        probe_count: 10
        max_probes_per_cell: 0
        interval_seconds: 60
`,
			wantErr:     true,
			errContains: "max_probes_per_cell must be positive",
		},
		{
			name: "unknown measurement type",
			yaml: `
measurements:
  - name: m
    type: http
    target: x.com
    cohorts:
      - name: r
        probe_count: 10
        max_probes_per_cell: 1
        interval_seconds: 60
`,
			wantErr:     true,
			errContains: `unknown type "http"`,
		},
		{
			name: "measurement missing target",
			yaml: `
measurements:
  - name: m
    type: dns
    cohorts:
      - name: r
        probe_count: 10
        max_probes_per_cell: 1
        interval_seconds: 60
`,
			wantErr:     true,
			errContains: "target is required",
		},
		{
			name: "measurement missing cohorts list",
			yaml: `
measurements:
  - name: m
    type: dns
    target: x.com
`,
			wantErr:     true,
			errContains: "at least one cohort is required",
		},
		{
			name: "h3_resolution out of range high",
			yaml: minimalValid + `
geo_diversity:
  h3_resolution: 16
`,
			wantErr:     true,
			errContains: "h3_resolution must be between 1 and 15",
		},
		{
			name: "city density_coefficient between 0 and 1 is valid",
			yaml: `
measurements:
  - name: m
    type: dns
    target: x.com
    cohorts:
      - name: r
        probe_count: 10
        max_probes_per_cell: 1
        interval_seconds: 60
        cfg:
          cities:
            - name: Ashburn
              lat: 39.04
              lon: -77.49
              radius_km: 40
              density_coefficient: 0.5
`,
		},
		{
			name: "city density_coefficient zero is invalid",
			yaml: `
measurements:
  - name: m
    type: dns
    target: x.com
    cohorts:
      - name: r
        probe_count: 10
        max_probes_per_cell: 1
        interval_seconds: 60
        cfg:
          cities:
            - name: Ashburn
              lat: 39.04
              lon: -77.49
              radius_km: 40
              density_coefficient: 0
`,
			wantErr:     true,
			errContains: "density_coefficient must be > 0",
		},
		{
			name: "city missing radius_km",
			yaml: `
measurements:
  - name: m
    type: dns
    target: x.com
    cohorts:
      - name: r
        probe_count: 10
        max_probes_per_cell: 1
        interval_seconds: 60
        cfg:
          cities:
            - name: Ashburn
              lat: 39.04
              lon: -77.49
              density_coefficient: 2.0
`,
			wantErr:     true,
			errContains: "radius_km must be positive",
		},
		{
			name: "all measurement types accepted",
			yaml: `
measurements:
  - name: m-dns
    type: dns
    target: x.com
    cohorts:
      - name: c
        probe_count: 10
        max_probes_per_cell: 1
        interval_seconds: 60
  - name: m-ping
    type: ping
    target: 1.2.3.4
    cohorts:
      - name: c
        probe_count: 10
        max_probes_per_cell: 1
        interval_seconds: 10
  - name: m-tls
    type: tls
    target: x.com
    cohorts:
      - name: c
        probe_count: 10
        max_probes_per_cell: 1
        interval_seconds: 90
  - name: m-trace
    type: traceroute
    target: 1.2.3.4
    cohorts:
      - name: c
        probe_count: 10
        max_probes_per_cell: 1
        interval_seconds: 95
`,
			check: func(t *testing.T, cfg *config.Config) {
				assert.Len(t, cfg.Measurements, 4)
			},
		},
		{
			name: "preset resolution applies cohort_configs to cohort",
			yaml: `
cohort_configs:
  mypreset:
    countries:
      BR: 10
    cities:
      - name: Ashburn
        lat: 39.04
        lon: -77.49
        radius_km: 40
        density_coefficient: 2.0
        score: 5
measurements:
  - name: m
    type: dns
    target: x.com
    cohorts:
      - name: r
        probe_count: 10
        max_probes_per_cell: 1
        interval_seconds: 60
        cfg_preset: mypreset
`,
			check: func(t *testing.T, cfg *config.Config) {
				cohort := cfg.Measurements[0].Cohorts[0]
				assert.Empty(t, cohort.CfgPreset, "CfgPreset cleared after resolution")
				assert.Equal(t, 10, cohort.Cfg.Countries["BR"])
				assert.Len(t, cohort.Cfg.Cities, 1)
			},
		},
		{
			name: "unknown cfg_preset is an error",
			yaml: `
measurements:
  - name: m
    type: dns
    target: x.com
    cohorts:
      - name: r
        probe_count: 10
        max_probes_per_cell: 1
        interval_seconds: 60
        cfg_preset: doesnotexist
`,
			wantErr:     true,
			errContains: `unknown cfg_preset "doesnotexist"`,
		},
		{
			name: "inline cfg wins over cfg_preset",
			yaml: `
cohort_configs:
  mypreset:
    countries:
      BR: 10
measurements:
  - name: m
    type: dns
    target: x.com
    cohorts:
      - name: r
        probe_count: 10
        max_probes_per_cell: 1
        interval_seconds: 60
        cfg_preset: mypreset
        cfg:
          countries:
            US: 99
`,
			check: func(t *testing.T, cfg *config.Config) {
				cohort := cfg.Measurements[0].Cohorts[0]
				// Inline cfg wins: US=99, not BR=10 from preset.
				assert.Equal(t, 99, cohort.Cfg.Countries["US"])
				assert.Zero(t, cohort.Cfg.Countries["BR"])
			},
		},
		{
			name: "cities in cohort_configs preset are validated",
			yaml: `
cohort_configs:
  bad:
    cities:
      - name: Ashburn
        lat: 39.04
        lon: -77.49
        radius_km: 0
        density_coefficient: 2.0
measurements:
  - name: m
    type: dns
    target: x.com
    cohorts:
      - name: r
        probe_count: 10
        max_probes_per_cell: 1
        interval_seconds: 60
        cfg_preset: bad
`,
			wantErr:     true,
			errContains: "radius_km must be positive",
		},
		{
			name: "cohort name is required",
			yaml: `
measurements:
  - name: m
    type: dns
    target: x.com
    cohorts:
      - probe_count: 10
        max_probes_per_cell: 1
        interval_seconds: 60
`,
			wantErr:     true,
			errContains: "name is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := writeTemp(t, tt.yaml)
			cfg, err := config.Load(path)
			if tt.wantErr {
				require.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
				assert.Nil(t, cfg)
				return
			}
			require.NoError(t, err)
			require.NotNil(t, cfg)
			if tt.check != nil {
				tt.check(t, cfg)
			}
		})
	}
}

func TestCohortCfg_CacheKey(t *testing.T) {
	a := config.CohortCfg{}
	b := config.CohortCfg{}
	assert.Equal(t, a.CacheKey(), b.CacheKey(), "identical configs must produce identical cache keys")

	c := config.CohortCfg{
		DisableContinentalShuffle: true,
	}
	assert.NotEqual(t, a.CacheKey(), c.CacheKey(), "different configs must produce different cache keys")

	// Cache key must be stable across multiple calls.
	assert.Equal(t, c.CacheKey(), c.CacheKey())
}
