// Package plan contains the declarative core of atlasctl: state file management,
// desired-vs-current diffing, and the apply logic that executes a changeset.
package plan

import (
	"errors"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"
)

// ErrStateNotFound is returned by LoadState when the state file does not exist.
// Callers should treat this as an empty (first-run) state, not a fatal error.
var ErrStateNotFound = errors.New("state file not found")

// StateFile is the on-disk record of all measurements managed by atlasctl.
// It is the source of truth for which RIPE Atlas measurement IDs correspond to
// which (measurement name, round) pairs.
type StateFile struct {
	// Measurements is keyed by measurement name, then round name.
	Measurements         map[string]map[string]MsmRecord `yaml:"measurements"`
	LastApplied          time.Time                       `yaml:"last_applied,omitempty"`
	ProbeSnapshot        string                          `yaml:"probe_snapshot,omitempty"`
	ProbeSnapshotFetched time.Time                       `yaml:"probe_snapshot_fetched,omitempty"`
}

// MsmRecord holds the live attributes of one RIPE Atlas measurement that are
// needed to detect drift and compute diffs.
type MsmRecord struct {
	MsmID    uint64   `yaml:"msm_id"`
	Target   string   `yaml:"target"`
	Type     string   `yaml:"type"`
	Interval int      `yaml:"interval"`
	ProbeIDs []uint32 `yaml:"probe_ids"`
}

// NewStateFile returns an empty, initialised StateFile.
func NewStateFile() StateFile {
	return StateFile{Measurements: make(map[string]map[string]MsmRecord)}
}

// GetRecord returns the MsmRecord for (name, round) and whether it exists.
func (sf StateFile) GetRecord(name, round string) (MsmRecord, bool) {
	rounds, ok := sf.Measurements[name]
	if !ok {
		return MsmRecord{}, false
	}
	rec, ok := rounds[round]
	return rec, ok
}

// SetRecord stores rec under (name, round), creating the outer map entry if needed.
func (sf *StateFile) SetRecord(name, round string, rec MsmRecord) {
	if sf.Measurements == nil {
		sf.Measurements = make(map[string]map[string]MsmRecord)
	}
	if sf.Measurements[name] == nil {
		sf.Measurements[name] = make(map[string]MsmRecord)
	}
	sf.Measurements[name][round] = rec
}

// DeleteRecord removes the (name, round) entry, cleaning up the outer map if empty.
func (sf *StateFile) DeleteRecord(name, round string) {
	if rounds, ok := sf.Measurements[name]; ok {
		delete(rounds, round)
		if len(rounds) == 0 {
			delete(sf.Measurements, name)
		}
	}
}

// LoadState reads a StateFile from path.
// Returns ErrStateNotFound if the file does not exist (treat as empty state).
func LoadState(path string) (StateFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return StateFile{}, ErrStateNotFound
		}
		return StateFile{}, err
	}
	var sf StateFile
	if err := yaml.Unmarshal(data, &sf); err != nil {
		return StateFile{}, err
	}
	if sf.Measurements == nil {
		sf.Measurements = make(map[string]map[string]MsmRecord)
	}
	return sf, nil
}

// SaveState writes sf to path atomically (temp file + rename).
func SaveState(path string, sf StateFile) error {
	data, err := yaml.Marshal(sf)
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	f, err := os.CreateTemp(dir, ".state-*.yaml.tmp")
	if err != nil {
		return err
	}
	tmpName := f.Name()
	defer func() { _ = os.Remove(tmpName) }()
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}
