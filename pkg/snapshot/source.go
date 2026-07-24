package snapshot

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/gofrs/flock"
)

// ErrRefreshDisabled is returned by CachedProbeSource when the snapshot is
// absent or stale and ProbeRefreshDisabled is set. CI environments set this
// flag and are expected to supply a fresh snapshot as a pre-step.
var ErrRefreshDisabled = errors.New("probe snapshot absent or stale and refresh is disabled")

// DefaultCachePath is the on-disk location used by CachedProbeSource when
// Path is not set.
const DefaultCachePath = "/tmp/atlasctl-probes.json"

// DefaultCacheTTL is the freshness window used by CachedProbeSource when TTL
// is not set.
const DefaultCacheTTL = 2 * time.Hour

// ProbeSource is the single abstraction over probe data. Callers obtain the
// full connected-probe list without knowing whether it came from disk, a
// cache, or the live API.
type ProbeSource interface {
	Probes(ctx context.Context) ([]Probe, error)
}

// FileProbeSource reads probes from a fixed path on disk. It has no freshness
// logic: if the file is missing it returns ErrNotFound; if it exists it
// returns all probes regardless of age.
//
// Use this for the CLI when the user has explicitly run "atlasctl refresh".
type FileProbeSource struct {
	Path string
}

// Probes loads and returns the probe list from the configured path.
// Returns ErrNotFound if the file does not exist.
func (s *FileProbeSource) Probes(_ context.Context) ([]Probe, error) {
	snap, err := Load(s.Path)
	if err != nil {
		return nil, err
	}
	return snap.Probes, nil
}

// CachedProbeSource is a read-through cache backed by a file on disk. It
// serves probes from the cache file when fresh, and re-fetches from the
// RIPE Atlas API (via Client) when the cache is absent or stale.
//
// File locking via flock serialises concurrent goroutines and processes, so
// only one writer ever hits the API at a time regardless of how many callers
// race. The double-check inside the exclusive lock ensures the winner of the
// race does the real fetch while the others coalesce onto the freshly written
// file.
//
// Use this for the Pulumi provider, which has no pre-fetched snapshot.
type CachedProbeSource struct {
	// Path is the on-disk cache location. Defaults to DefaultCachePath.
	Path string
	// TTL controls how long a cached snapshot stays fresh. Defaults to DefaultCacheTTL.
	TTL time.Duration
	// Client fetches probes from the RIPE Atlas API when a refresh is needed.
	// If nil and a refresh is needed, Probes returns an error.
	Client Client
	// ProbeRefreshDisabled, when true, prevents any API calls. If the snapshot
	// is absent or stale, Probes returns ErrRefreshDisabled. Set this in CI
	// environments that supply a snapshot as a separate step.
	ProbeRefreshDisabled bool
}

func (s *CachedProbeSource) cachePath() string {
	if s.Path != "" {
		return s.Path
	}
	return DefaultCachePath
}

func (s *CachedProbeSource) ttl() time.Duration {
	if s.TTL > 0 {
		return s.TTL
	}
	return DefaultCacheTTL
}

// isFresh reports whether the file at path has an mtime within ttl of now.
// Returns false (not an error) when the file does not exist.
func isFresh(path string, ttl time.Duration) (bool, error) {
	info, err := os.Stat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	return time.Since(info.ModTime()) <= ttl, nil
}

// Probes returns the full connected-probe list.
//
// Fast path (shared lock): if the on-disk cache is within TTL, read and
// return it immediately.
//
// Slow path (exclusive lock): if the cache is absent or stale, one caller
// wins the exclusive lock, re-checks freshness (another writer may have
// beaten it), then fetches from the API and writes to disk before returning.
// Subsequent callers find a fresh file on their double-check and skip the
// fetch.
func (s *CachedProbeSource) Probes(ctx context.Context) ([]Probe, error) {
	path := s.cachePath()
	ttl := s.ttl()

	// Ensure the cache directory exists before opening any lock file in it.
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return nil, fmt.Errorf("creating probe cache directory: %w", err)
	}

	fl := flock.New(path + ".lock")

	// ── fast path: shared lock ────────────────────────────────────────────────
	if err := fl.RLock(); err != nil {
		return nil, fmt.Errorf("acquiring shared lock on probe cache: %w", err)
	}
	fresh, err := isFresh(path, ttl)
	if err != nil {
		_ = fl.Unlock()
		return nil, err
	}
	if fresh {
		snap, loadErr := Load(path)
		_ = fl.Unlock()
		if loadErr != nil {
			return nil, loadErr
		}
		return snap.Probes, nil
	}
	_ = fl.Unlock()

	// ── stale or missing: check policy before doing anything expensive ────────
	if s.ProbeRefreshDisabled {
		return nil, ErrRefreshDisabled
	}
	if s.Client == nil {
		return nil, fmt.Errorf("probe cache at %s is absent or stale: provide an API client or run 'atlasctl refresh'", path)
	}

	// ── slow path: exclusive lock ─────────────────────────────────────────────
	if err := fl.Lock(); err != nil {
		return nil, fmt.Errorf("acquiring exclusive lock on probe cache: %w", err)
	}
	defer func() { _ = fl.Unlock() }()

	// Double-check: another writer may have refreshed while we waited for the
	// exclusive lock.
	fresh, err = isFresh(path, ttl)
	if err != nil {
		return nil, err
	}
	if fresh {
		snap, err := Load(path)
		if err != nil {
			return nil, err
		}
		return snap.Probes, nil
	}

	// We hold the exclusive lock and the cache is still stale — we are the
	// designated writer.
	probes, err := s.Client.FetchProbes(ctx)
	if err != nil {
		return nil, fmt.Errorf("fetching probes from RIPE Atlas API: %w", err)
	}
	snap := Snapshot{Probes: probes, FetchedAt: time.Now().UTC()}
	if err := Save(path, snap); err != nil {
		return nil, fmt.Errorf("saving probe cache: %w", err)
	}
	return probes, nil
}
