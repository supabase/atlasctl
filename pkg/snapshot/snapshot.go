package snapshot

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"
)

// ErrNotFound is returned by Load when no snapshot file exists at the given path.
var ErrNotFound = errors.New("snapshot not found")

// Probe is a stripped-down representation of a RIPE Atlas probe containing
// only the fields needed for scoring and selection.
type Probe struct {
	ID          uint32   `json:"id"`
	ASN4        uint32   `json:"asn4"`         // 0 if the probe has no IPv4 ASN
	CountryCode string   `json:"country_code"`
	Tags        []string `json:"tags"`         // tag slugs
	Lat         float64  `json:"lat"`
	Lon         float64  `json:"lon"`
	StatusID    uint     `json:"status_id"`    // 1 = Connected; see goat.ProbeStatus*
}

// Snapshot is a point-in-time capture of probes fetched from the RIPE Atlas API.
type Snapshot struct {
	Probes    []Probe   `json:"probes"`
	FetchedAt time.Time `json:"fetched_at"`
}

// Client fetches probes from the RIPE Atlas API.
// It is implemented by pkg/atlasapi and by FakeClient for tests.
type Client interface {
	FetchProbes(ctx context.Context) ([]Probe, error)
}

// Save writes s to path atomically by writing to a sibling temp file and
// then renaming it into place, so readers never see a partial file.
func Save(path string, s Snapshot) error {
	data, err := json.Marshal(s)
	if err != nil {
		return err
	}

	dir := filepath.Dir(path)
	f, err := os.CreateTemp(dir, ".snapshot-*.json.tmp")
	if err != nil {
		return err
	}
	tmpName := f.Name()
	// Always attempt cleanup; no-op if rename succeeded.
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

// Load reads a Snapshot from path.
// Returns ErrNotFound if the file does not exist.
func Load(path string) (Snapshot, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Snapshot{}, ErrNotFound
		}
		return Snapshot{}, err
	}

	var s Snapshot
	if err := json.Unmarshal(data, &s); err != nil {
		return Snapshot{}, err
	}
	return s, nil
}
