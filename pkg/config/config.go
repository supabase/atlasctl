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
	Rounds       []Round       `yaml:"rounds"`
	Measurements []Measurement `yaml:"measurements"`
	Scoring      ScoringConfig `yaml:"scoring"`
	ExcludeTags  []string      `yaml:"exclude_tags"`
	GeoDiversity GeoConfig     `yaml:"geo_diversity"`
	Cities       []CityConfig  `yaml:"cities"`
}

// Round defines a frequency tier for probe selection.
type Round struct {
	Name             string `yaml:"name"`
	Count            int    `yaml:"count"`
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

// Measurement defines a single measurement target and its round assignments.
type Measurement struct {
	Name      string          `yaml:"name"`
	Type      MeasurementType `yaml:"type"`
	Target    string          `yaml:"target"`
	QueryType string          `yaml:"query_type"` // DNS only: A, AAAA, NS, MX, ...
	Rounds    []string        `yaml:"rounds"`
}

// ScoringConfig holds additive weights for probe scoring.
type ScoringConfig struct {
	ASN       map[uint32]int `yaml:"asn"`
	Tags      map[string]int `yaml:"tags"`
	Countries map[string]int `yaml:"countries"`
	Stability map[string]int `yaml:"stability"`
}

// GeoConfig controls geographic diversity parameters.
type GeoConfig struct {
	H3Resolution int `yaml:"h3_resolution"`
}

// CityConfig defines a city cluster with a relaxed H3 cell capacity.
type CityConfig struct {
	Name               string  `yaml:"name"`
	Lat                float64 `yaml:"lat"`
	Lon                float64 `yaml:"lon"`
	RadiusKm           float64 `yaml:"radius_km"`
	DensityCoefficient float64 `yaml:"density_coefficient"`
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
}

var validMeasurementTypes = map[MeasurementType]bool{
	TypeDNS:        true,
	TypePing:       true,
	TypeTLS:        true,
	TypeTraceroute: true,
}

func (c *Config) validate() error {
	var errs []error

	if len(c.Rounds) == 0 {
		errs = append(errs, errors.New("at least one round is required"))
	}

	roundNames := make(map[string]bool, len(c.Rounds))
	for i, r := range c.Rounds {
		if r.Name == "" {
			errs = append(errs, fmt.Errorf("rounds[%d]: name is required", i))
		}
		if roundNames[r.Name] {
			errs = append(errs, fmt.Errorf("duplicate round name %q", r.Name))
		}
		roundNames[r.Name] = true
		if r.Count <= 0 {
			errs = append(errs, fmt.Errorf("round %q: count must be positive", r.Name))
		}
		if r.IntervalSeconds <= 0 {
			errs = append(errs, fmt.Errorf("round %q: interval_seconds must be positive", r.Name))
		}
		if r.MaxProbesPerCell <= 0 {
			errs = append(errs, fmt.Errorf("round %q: max_probes_per_cell must be positive", r.Name))
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
		if len(m.Rounds) == 0 {
			errs = append(errs, fmt.Errorf("measurement %q: at least one round reference is required", m.Name))
		}
		for _, ref := range m.Rounds {
			if !roundNames[ref] {
				errs = append(errs, fmt.Errorf("measurement %q: references unknown round %q", m.Name, ref))
			}
		}
	}

	res := c.GeoDiversity.H3Resolution
	if res < 1 || res > 15 {
		errs = append(errs, fmt.Errorf("geo_diversity.h3_resolution must be between 1 and 15, got %d", res))
	}

	for i, city := range c.Cities {
		if city.Name == "" {
			errs = append(errs, fmt.Errorf("cities[%d]: name is required", i))
		}
		if city.RadiusKm <= 0 {
			errs = append(errs, fmt.Errorf("city %q: radius_km must be positive", city.Name))
		}
		if city.DensityCoefficient < 1.0 {
			errs = append(errs, fmt.Errorf("city %q: density_coefficient must be >= 1.0", city.Name))
		}
	}

	return errors.Join(errs...)
}
