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

	assert.Len(t, cfg.Rounds, 3)
	assert.Len(t, cfg.Measurements, 3)
	assert.Equal(t, 3, cfg.GeoDiversity.H3Resolution)
	assert.Equal(t, 10, cfg.Scoring.ASN[7018])
	assert.Equal(t, 5, cfg.Scoring.Tags["office"])
	assert.Equal(t, 5, cfg.Scoring.Countries["BR"])
	assert.Equal(t, 5, cfg.Scoring.Stability["system-ipv4-stable-90d"])
	assert.Len(t, cfg.Cities, 2)
}

func TestLoad_MissingFile(t *testing.T) {
	_, err := config.Load("/nonexistent/path/config.yaml")
	require.Error(t, err)
	assert.True(t, os.IsNotExist(err) || strings.Contains(err.Error(), "no such file"),
		"expected file-not-found error, got: %v", err)
}

func TestLoad(t *testing.T) {
	// minimalValid is the smallest possible valid config — used as a base for error cases.
	const minimalValid = `
rounds:
  - name: fast
    count: 10
    interval_seconds: 60
    max_probes_per_cell: 1
measurements:
  - name: dns-test
    type: dns
    target: example.com
    rounds: [fast]
`
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
			name:        "no rounds",
			yaml:        `measurements: [{name: m, type: dns, target: x.com, rounds: [r]}]`,
			wantErr:     true,
			errContains: "at least one round",
		},
		{
			name:    "no measurements",
			yaml:    `rounds: [{name: r, count: 10, interval_seconds: 60, max_probes_per_cell: 1}]`,
			wantErr: false,
		},
		{
			name: "duplicate round names",
			yaml: `
rounds:
  - name: r1
    count: 10
    interval_seconds: 60
    max_probes_per_cell: 1
  - name: r1
    count: 20
    interval_seconds: 120
    max_probes_per_cell: 2
measurements:
  - name: m
    type: dns
    target: x.com
    rounds: [r1]
`,
			wantErr:     true,
			errContains: `duplicate round name "r1"`,
		},
		{
			name: "duplicate measurement names",
			yaml: `
rounds:
  - name: r1
    count: 10
    interval_seconds: 60
    max_probes_per_cell: 1
measurements:
  - name: m
    type: dns
    target: x.com
    rounds: [r1]
  - name: m
    type: ping
    target: x.com
    rounds: [r1]
`,
			wantErr:     true,
			errContains: `duplicate measurement name "m"`,
		},
		{
			name: "zero interval_seconds",
			yaml: `
rounds:
  - name: r
    count: 10
    interval_seconds: 0
    max_probes_per_cell: 1
measurements:
  - name: m
    type: dns
    target: x.com
    rounds: [r]
`,
			wantErr:     true,
			errContains: "interval_seconds must be positive",
		},
		{
			name: "negative interval_seconds",
			yaml: `
rounds:
  - name: r
    count: 10
    interval_seconds: -60
    max_probes_per_cell: 1
measurements:
  - name: m
    type: dns
    target: x.com
    rounds: [r]
`,
			wantErr:     true,
			errContains: "interval_seconds must be positive",
		},
		{
			name: "zero count",
			yaml: `
rounds:
  - name: r
    count: 0
    interval_seconds: 60
    max_probes_per_cell: 1
measurements:
  - name: m
    type: dns
    target: x.com
    rounds: [r]
`,
			wantErr:     true,
			errContains: "count must be positive",
		},
		{
			name: "zero max_probes_per_cell",
			yaml: `
rounds:
  - name: r
    count: 10
    interval_seconds: 60
    max_probes_per_cell: 0
measurements:
  - name: m
    type: dns
    target: x.com
    rounds: [r]
`,
			wantErr:     true,
			errContains: "max_probes_per_cell must be positive",
		},
		{
			name: "unknown measurement type",
			yaml: `
rounds:
  - name: r
    count: 10
    interval_seconds: 60
    max_probes_per_cell: 1
measurements:
  - name: m
    type: http
    target: x.com
    rounds: [r]
`,
			wantErr:     true,
			errContains: `unknown type "http"`,
		},
		{
			name: "measurement references unknown round",
			yaml: `
rounds:
  - name: r
    count: 10
    interval_seconds: 60
    max_probes_per_cell: 1
measurements:
  - name: m
    type: dns
    target: x.com
    rounds: [r, nonexistent]
`,
			wantErr:     true,
			errContains: `unknown round "nonexistent"`,
		},
		{
			name: "measurement missing target",
			yaml: `
rounds:
  - name: r
    count: 10
    interval_seconds: 60
    max_probes_per_cell: 1
measurements:
  - name: m
    type: dns
    rounds: [r]
`,
			wantErr:     true,
			errContains: "target is required",
		},
		{
			name: "measurement missing rounds list",
			yaml: `
rounds:
  - name: r
    count: 10
    interval_seconds: 60
    max_probes_per_cell: 1
measurements:
  - name: m
    type: dns
    target: x.com
`,
			wantErr:     true,
			errContains: "at least one round reference",
		},
		{
			name: "h3_resolution out of range high",
			yaml: `
rounds:
  - name: r
    count: 10
    interval_seconds: 60
    max_probes_per_cell: 1
measurements:
  - name: m
    type: dns
    target: x.com
    rounds: [r]
geo_diversity:
  h3_resolution: 16
`,
			wantErr:     true,
			errContains: "h3_resolution must be between 1 and 15",
		},
		{
			name: "city density_coefficient between 0 and 1 is valid",
			yaml: `
rounds:
  - name: r
    count: 10
    interval_seconds: 60
    max_probes_per_cell: 1
measurements:
  - name: m
    type: dns
    target: x.com
    rounds: [r]
cities:
  - name: Ashburn
    lat: 39.04
    lon: -77.49
    radius_km: 40
    density_coefficient: 0.5
`,
			wantErr: false,
		},
		{
			name: "city density_coefficient zero is invalid",
			yaml: `
rounds:
  - name: r
    count: 10
    interval_seconds: 60
    max_probes_per_cell: 1
measurements:
  - name: m
    type: dns
    target: x.com
    rounds: [r]
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
rounds:
  - name: r
    count: 10
    interval_seconds: 60
    max_probes_per_cell: 1
measurements:
  - name: m
    type: dns
    target: x.com
    rounds: [r]
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
rounds:
  - name: r
    count: 10
    interval_seconds: 60
    max_probes_per_cell: 1
measurements:
  - name: m-dns
    type: dns
    target: x.com
    rounds: [r]
  - name: m-ping
    type: ping
    target: 1.2.3.4
    rounds: [r]
  - name: m-tls
    type: tls
    target: x.com
    rounds: [r]
  - name: m-trace
    type: traceroute
    target: 1.2.3.4
    rounds: [r]
`,
			check: func(t *testing.T, cfg *config.Config) {
				assert.Len(t, cfg.Measurements, 4)
			},
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
