package config

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"os"
	"reflect"

	"gopkg.in/yaml.v3"
)

// Config is the top-level configuration for atlasctl.
type Config struct {
	Snapshot      string               `yaml:"snapshot"`
	State         string               `yaml:"state"`
	Namespace     string               `yaml:"namespace"`
	CohortConfigs map[string]CohortCfg `yaml:"cohort_configs"`
	Measurements  []Measurement        `yaml:"measurements"`
	ExcludeTags   []string             `yaml:"exclude_tags"`
	GeoDiversity  GeoConfig            `yaml:"geo_diversity"`
}

// CohortCfg is the full ordering config for one cohort tier. It is the input
// to both ProbeWeighter (for per-probe scoring) and ProbeOrderer (for global
// reordering knobs). Named presets are defined in Config.CohortConfigs.
//
// Cities covers both scoring bonuses (Score field) and H3 density coefficients
// (DensityCoefficient field). Both are per-cohort preferences.
type CohortCfg struct {
	ScoringConfig             `yaml:",inline"`
	Cities                    []CityConfig `yaml:"cities"`
	DisableContinentalShuffle bool         `yaml:"disable_continental_shuffle"`
}

// CacheKey returns a stable hash of the cohort config suitable for use as a
// cache key. It JSON-marshals the struct (encoding/json sorts map keys
// deterministically) and hashes the result with FNV-1a.
func (c CohortCfg) CacheKey() string {
	b, _ := json.Marshal(c)
	h := fnv.New64a()
	h.Write(b)
	return hex.EncodeToString(h.Sum(nil))
}

// MeasurementCohort is one tier within a measurement's cohort list.
// ProbeCount and MaxProbesPerCell are selection policy: they are enforced by
// the selection loop, not by the ProbeOrderer.
type MeasurementCohort struct {
	Name             string    `yaml:"name"`
	ProbeCount       int       `yaml:"probe_count"`
	MaxProbesPerCell int       `yaml:"max_probes_per_cell"`
	IntervalSeconds  int       `yaml:"interval_seconds"`
	IncludeProbeIDs  []uint32  `yaml:"include_probe_ids"`
	ExcludeProbeIDs  []uint32  `yaml:"exclude_probe_ids"`
	CfgPreset        string    `yaml:"cfg_preset"`
	Cfg              CohortCfg `yaml:"cfg"`
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
	Name      string              `yaml:"name"`
	Type      MeasurementType     `yaml:"type"`
	Target    string              `yaml:"target"`
	AF        int                 `yaml:"af"`         // address family: 4 or 6, default 4
	QueryType string              `yaml:"query_type"` // DNS only: A, AAAA, NS, MX, ...
	Cohorts   []MeasurementCohort `yaml:"cohorts"`
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
	if c.GeoDiversity.H3Resolution == 0 {
		c.GeoDiversity.H3Resolution = 3
	}
	for i := range c.Measurements {
		if c.Measurements[i].AF == 0 {
			c.Measurements[i].AF = 4
		}
		for j := range c.Measurements[i].Cohorts {
			cohort := &c.Measurements[i].Cohorts[j]
			if cohort.CfgPreset == "" {
				continue
			}
			preset, ok := c.CohortConfigs[cohort.CfgPreset]
			if !ok {
				continue // validate will catch the unknown preset
			}
			if reflect.DeepEqual(cohort.Cfg, CohortCfg{}) {
				cohort.Cfg = preset
			}
			cohort.CfgPreset = ""
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

	// Validate named cohort config presets.
	for name, cfg := range c.CohortConfigs {
		errs = append(errs, validateCities(cfg.Cities,
			fmt.Sprintf("cohort_configs[%q]", name))...)
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
		if m.AF != 4 && m.AF != 6 {
			errs = append(errs, fmt.Errorf("measurement %q: af must be 4 or 6, got %d", m.Name, m.AF))
		}
		if len(m.Cohorts) == 0 {
			errs = append(errs, fmt.Errorf("measurement %q: at least one cohort is required", m.Name))
		}

		cohortNames := make(map[string]bool, len(m.Cohorts))
		for j, cohort := range m.Cohorts {
			label := fmt.Sprintf("measurement %q cohorts[%d]", m.Name, j)
			if cohort.Name == "" {
				errs = append(errs, fmt.Errorf("%s: name is required", label))
			}
			if cohortNames[cohort.Name] {
				errs = append(errs, fmt.Errorf("measurement %q: duplicate cohort name %q", m.Name, cohort.Name))
			}
			cohortNames[cohort.Name] = true
			if cohort.ProbeCount <= 0 {
				errs = append(errs, fmt.Errorf("measurement %q cohort %q: probe_count must be positive", m.Name, cohort.Name))
			}
			if cohort.MaxProbesPerCell <= 0 {
				errs = append(errs, fmt.Errorf("measurement %q cohort %q: max_probes_per_cell must be positive", m.Name, cohort.Name))
			}
			if cohort.IntervalSeconds <= 0 {
				errs = append(errs, fmt.Errorf("measurement %q cohort %q: interval_seconds must be positive", m.Name, cohort.Name))
			}
			if cohort.CfgPreset != "" {
				if _, ok := c.CohortConfigs[cohort.CfgPreset]; !ok {
					errs = append(errs, fmt.Errorf("measurement %q cohort %q: unknown cfg_preset %q", m.Name, cohort.Name, cohort.CfgPreset))
				}
			}
			errs = append(errs, validateCities(cohort.Cfg.Cities,
				fmt.Sprintf("measurement %q cohort %q", m.Name, cohort.Name))...)
		}
	}

	res := c.GeoDiversity.H3Resolution
	if res < 1 || res > 15 {
		errs = append(errs, fmt.Errorf("geo_diversity.h3_resolution must be between 1 and 15, got %d", res))
	}

	return errors.Join(errs...)
}

func validateCities(cities []CityConfig, context string) []error {
	var errs []error
	for i, city := range cities {
		if city.Name == "" {
			errs = append(errs, fmt.Errorf("%s cities[%d]: name is required", context, i))
		}
		if city.RadiusKm <= 0 {
			errs = append(errs, fmt.Errorf("%s city %q: radius_km must be positive", context, city.Name))
		}
		if city.DensityCoefficient <= 0 {
			errs = append(errs, fmt.Errorf("%s city %q: density_coefficient must be > 0", context, city.Name))
		}
	}
	return errs
}
