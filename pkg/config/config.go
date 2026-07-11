package config

import (
	"errors"
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Config is the top-level configuration for atlasctl.
type Config struct {
	Snapshot     string        `yaml:"snapshot"`
	State        string        `yaml:"state"`
	TagPrefix    string        `yaml:"tag_prefix"`
	Cohorts      []Cohort      `yaml:"cohorts"`
	Measurements []Measurement `yaml:"measurements"`
	Scoring      ScoringConfig `yaml:"scoring"`
	ExcludeTags  []string      `yaml:"exclude_tags"`
	GeoDiversity GeoConfig     `yaml:"geo_diversity"`
	Cities       []CityConfig  `yaml:"cities"`
}

// Cohort defines a frequency tier for probe selection.
type Cohort struct {
	Name             string `yaml:"name"`
	ProbeCount       int    `yaml:"probe_count"`
	IntervalSeconds  int    `yaml:"interval_seconds"`
	MaxProbesPerCell int    `yaml:"max_probes_per_cell"`
}

// MeasurementType is one of the supported RIPE Atlas measurement types.
type MeasurementType string

const (
	TypeDNS        MeasurementType = "dns"
	TypePing       MeasurementType = "ping"
	TypeTLS        MeasurementType = "tls"
	TypeTraceroute MeasurementType = "traceroute"
)

// Measurement defines a single measurement target and its cohort assignments.
type Measurement struct {
	Name      string          `yaml:"name"`
	Type      MeasurementType `yaml:"type"`
	Target    string          `yaml:"target"`
	QueryType string          `yaml:"query_type"` // DNS only: A, AAAA, NS, MX, ...
	AF        int             `yaml:"af"`         // address family: 4 or 6, default 4
	Cohorts   []string        `yaml:"cohorts"`
}

// BandThresholds defines the minimum score for each band tier.
// Scores below C fall into BandD. Defaults: A=15, B=8, C=3.
type BandThresholds struct {
	A int `yaml:"a"`
	B int `yaml:"b"`
	C int `yaml:"c"`
}

// Effective returns the thresholds with defaults applied for any zero value.
// This allows ScoringConfig to be constructed without going through Load.
func (t BandThresholds) Effective() BandThresholds {
	if t.A == 0 {
		t.A = 15
	}
	if t.B == 0 {
		t.B = 8
	}
	if t.C == 0 {
		t.C = 3
	}
	return t
}

// ScoringConfig holds additive weights for probe scoring.
type ScoringConfig struct {
	ASN            map[uint32]int `yaml:"asn"`
	Tags           map[string]int `yaml:"tags"`
	Countries      map[string]int `yaml:"countries"`
	Stability      map[string]int `yaml:"stability"`
	BandThresholds BandThresholds `yaml:"band_thresholds"`
}

// GeoConfig controls geographic diversity parameters.
type GeoConfig struct {
	H3Resolution int `yaml:"h3_resolution"`
}

// CityConfig defines a city cluster with optional probe scoring and H3 cell capacity overrides.
type CityConfig struct {
	Name               string  `yaml:"name"`
	Lat                float64 `yaml:"lat"`
	Lon                float64 `yaml:"lon"`
	RadiusKm           float64 `yaml:"radius_km"`
	DensityCoefficient float64 `yaml:"density_coefficient"`
	Score              int     `yaml:"score"` // additive score bonus for probes within this city's radius; 0 = no effect
}

// Load reads and validates a Config from a YAML file at path.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	cfg.applyDefaults()
	if err := cfg.validate(); err != nil {
		return nil, fmt.Errorf("invalid config %s: %w", path, err)
	}
	return &cfg, nil
}

func (c *Config) applyDefaults() {
	if c.TagPrefix == "" {
		c.TagPrefix = "[atlasctl:"
	}
	if c.GeoDiversity.H3Resolution == 0 {
		c.GeoDiversity.H3Resolution = 3
	}
	t := &c.Scoring.BandThresholds
	if t.A == 0 {
		t.A = 15
	}
	if t.B == 0 {
		t.B = 8
	}
	if t.C == 0 {
		t.C = 3
	}
	for i := range c.Measurements {
		if c.Measurements[i].AF == 0 {
			c.Measurements[i].AF = 4
		}
	}
}

var validMeasurementTypes = map[MeasurementType]bool{
	TypeDNS:        true,
	TypePing:       true,
	TypeTLS:        true,
	TypeTraceroute: true,
}

func (c *Config) validate() error {
	var errs []error

	if len(c.Cohorts) == 0 {
		errs = append(errs, errors.New("at least one cohort is required"))
	}

	// in validate()
	for slug := range c.Scoring.Tags {
		if _, conflict := c.Scoring.Stability[slug]; conflict {
			errs = append(errs,
				fmt.Errorf("scoring: tag slug %q appears in both tags and stability",
					slug))
		}
	}

	cohortNames := make(map[string]bool, len(c.Cohorts))
	for i, r := range c.Cohorts {
		if r.Name == "" {
			errs = append(errs, fmt.Errorf("cohorts[%d]: name is required", i))
		}
		if cohortNames[r.Name] {
			errs = append(errs, fmt.Errorf("duplicate cohort name %q", r.Name))
		}
		cohortNames[r.Name] = true
		if r.ProbeCount <= 0 {
			errs = append(errs, fmt.Errorf("cohort %q: probe_count must be positive", r.Name))
		}
		if r.IntervalSeconds <= 0 {
			errs = append(errs, fmt.Errorf("cohort %q: interval_seconds must be positive", r.Name))
		}
		if r.MaxProbesPerCell <= 0 {
			errs = append(errs, fmt.Errorf("cohort %q: max_probes_per_cell must be positive", r.Name))
		}
	}

	msmNames := make(map[string]bool, len(c.Measurements))
	for i, m := range c.Measurements {
		if m.Name == "" {
			errs = append(errs, fmt.Errorf("measurements[%d]: name is required", i))
		}
		if msmNames[m.Name] {
			errs = append(errs, fmt.Errorf("duplicate measurement name %q", m.Name))
		}
		msmNames[m.Name] = true
		if !validMeasurementTypes[m.Type] {
			errs = append(errs, fmt.Errorf("measurement %q: unknown type %q (must be dns, ping, tls, traceroute)", m.Name, m.Type))
		}
		if m.Target == "" {
			errs = append(errs, fmt.Errorf("measurement %q: target is required", m.Name))
		}
		if len(m.Cohorts) == 0 {
			errs = append(errs, fmt.Errorf("measurement %q: at least one cohort reference is required", m.Name))
		}
		if m.AF != 4 && m.AF != 6 {
			errs = append(errs, fmt.Errorf("measurement %q: af must be 4 or 6, got %d", m.Name, m.AF))
		}
		for _, ref := range m.Cohorts {
			if !cohortNames[ref] {
				errs = append(errs, fmt.Errorf("measurement %q: references unknown cohort %q", m.Name, ref))
			}
		}
	}

	res := c.GeoDiversity.H3Resolution
	if res < 1 || res > 15 {
		errs = append(errs, fmt.Errorf("geo_diversity.h3_resolution must be between 1 and 15, got %d", res))
	}

	t := c.Scoring.BandThresholds
	if t.C <= 0 {
		errs = append(errs, fmt.Errorf("scoring.band_thresholds.c must be > 0, got %d", t.C))
	}
	if t.B <= t.C {
		errs = append(errs, fmt.Errorf("scoring.band_thresholds.b (%d) must be greater than c (%d)", t.B, t.C))
	}
	if t.A <= t.B {
		errs = append(errs, fmt.Errorf("scoring.band_thresholds.a (%d) must be greater than b (%d)", t.A, t.B))
	}

	for i, city := range c.Cities {
		if city.Name == "" {
			errs = append(errs, fmt.Errorf("cities[%d]: name is required", i))
		}
		if city.RadiusKm <= 0 {
			errs = append(errs, fmt.Errorf("city %q: radius_km must be positive", city.Name))
		}
		if city.DensityCoefficient <= 0 {
			errs = append(errs, fmt.Errorf("city %q: density_coefficient must be > 0", city.Name))
		}
	}

	return errors.Join(errs...)
}
