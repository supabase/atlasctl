package snapshot_test

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/supabase/atlasctl/pkg/snapshot"
)

// ── FileProbeSource ───────────────────────────────────────────────────────────

func TestFileProbeSource_ReturnsProbes(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "snapshot.json")

	probes := sampleProbes()
	require.NoError(t, snapshot.Save(path, snapshot.Snapshot{
		Probes:    probes,
		FetchedAt: time.Now().UTC(),
	}))

	src := &snapshot.FileProbeSource{Path: path}
	got, err := src.Probes(context.Background())
	require.NoError(t, err)
	require.Len(t, got, len(probes))
	assert.Equal(t, probes[0].ID, got[0].ID)
}

func TestFileProbeSource_MissingFile(t *testing.T) {
	src := &snapshot.FileProbeSource{Path: "/nonexistent/snapshot.json"}
	_, err := src.Probes(context.Background())
	require.Error(t, err)
	assert.ErrorIs(t, err, snapshot.ErrNotFound)
}

// ── FakeProbeSource ───────────────────────────────────────────────────────────

func TestFakeProbeSource_ReturnsProbes(t *testing.T) {
	probes := sampleProbes()
	src := &snapshot.FakeProbeSource{ProbeList: probes}
	got, err := src.Probes(context.Background())
	require.NoError(t, err)
	assert.Equal(t, probes, got)
}

func TestFakeProbeSource_Error(t *testing.T) {
	sentinel := errors.New("source unavailable")
	src := &snapshot.FakeProbeSource{Err: sentinel}
	_, err := src.Probes(context.Background())
	require.ErrorIs(t, err, sentinel)
}

// ── CachedProbeSource ─────────────────────────────────────────────────────────

func TestCachedProbeSource_FreshCache_NoFetch(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "probes.json")

	probes := sampleProbes()
	require.NoError(t, snapshot.Save(path, snapshot.Snapshot{
		Probes:    probes,
		FetchedAt: time.Now().UTC(),
	}))

	// Client that fails if called — it must not be called for a fresh cache.
	client := &snapshot.FakeClient{Err: errors.New("should not fetch")}
	src := &snapshot.CachedProbeSource{
		Path:   path,
		TTL:    time.Hour,
		Client: client,
	}

	got, err := src.Probes(context.Background())
	require.NoError(t, err)
	require.Len(t, got, len(probes))
}

func TestCachedProbeSource_MissingCache_FetchesAndWrites(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "probes.json")
	// No file written — cache is absent.

	probes := sampleProbes()
	client := &snapshot.FakeClient{Probes: probes}
	src := &snapshot.CachedProbeSource{
		Path:   path,
		TTL:    time.Hour,
		Client: client,
	}

	got, err := src.Probes(context.Background())
	require.NoError(t, err)
	require.Len(t, got, len(probes))

	// Cache file must exist on disk after a successful fetch.
	loaded, err := snapshot.Load(path)
	require.NoError(t, err)
	assert.Len(t, loaded.Probes, len(probes))
}

func TestCachedProbeSource_StaleCache_FetchesAndWrites(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "probes.json")

	// Write a snapshot and then artificially age it beyond the TTL by using a
	// very short TTL and a negative offset for the file's modification time.
	old := sampleProbes()[:1]
	require.NoError(t, snapshot.Save(path, snapshot.Snapshot{
		Probes:    old,
		FetchedAt: time.Now().Add(-10 * time.Minute).UTC(),
	}))
	// Use a 1ns TTL so the file is immediately stale.
	ttl := time.Nanosecond

	fresh := sampleProbes()
	client := &snapshot.FakeClient{Probes: fresh}
	src := &snapshot.CachedProbeSource{
		Path:   path,
		TTL:    ttl,
		Client: client,
	}

	got, err := src.Probes(context.Background())
	require.NoError(t, err)
	// We get the fresh probes from the API, not the stale file.
	assert.Len(t, got, len(fresh))

	loaded, err := snapshot.Load(path)
	require.NoError(t, err)
	assert.Len(t, loaded.Probes, len(fresh))
}

func TestCachedProbeSource_RefreshDisabled_MissingCache(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "probes.json")
	// No file — cache is absent.

	src := &snapshot.CachedProbeSource{
		Path:                 path,
		TTL:                  time.Hour,
		ProbeRefreshDisabled: true,
	}

	_, err := src.Probes(context.Background())
	require.ErrorIs(t, err, snapshot.ErrRefreshDisabled)
}

func TestCachedProbeSource_RefreshDisabled_StaleCache(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "probes.json")

	require.NoError(t, snapshot.Save(path, snapshot.Snapshot{
		Probes:    sampleProbes(),
		FetchedAt: time.Now().UTC(),
	}))

	src := &snapshot.CachedProbeSource{
		Path:                 path,
		TTL:                  time.Nanosecond, // immediately stale
		ProbeRefreshDisabled: true,
	}

	_, err := src.Probes(context.Background())
	require.ErrorIs(t, err, snapshot.ErrRefreshDisabled)
}

func TestCachedProbeSource_RefreshDisabled_FreshCache(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "probes.json")

	probes := sampleProbes()
	require.NoError(t, snapshot.Save(path, snapshot.Snapshot{
		Probes:    probes,
		FetchedAt: time.Now().UTC(),
	}))

	// ProbeRefreshDisabled must not block reads from a fresh cache.
	src := &snapshot.CachedProbeSource{
		Path:                 path,
		TTL:                  time.Hour,
		ProbeRefreshDisabled: true,
	}

	got, err := src.Probes(context.Background())
	require.NoError(t, err)
	assert.Len(t, got, len(probes))
}

func TestCachedProbeSource_NilClient_StaleCache(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "probes.json")

	require.NoError(t, snapshot.Save(path, snapshot.Snapshot{
		Probes:    sampleProbes(),
		FetchedAt: time.Now().UTC(),
	}))

	src := &snapshot.CachedProbeSource{
		Path:   path,
		TTL:    time.Nanosecond, // immediately stale
		Client: nil,
	}

	_, err := src.Probes(context.Background())
	require.Error(t, err)
	// Not ErrRefreshDisabled — a different, more specific error.
	assert.NotErrorIs(t, err, snapshot.ErrRefreshDisabled)
}

func TestCachedProbeSource_DefaultPath(t *testing.T) {
	// Verify DefaultCachePath is non-empty and sane; this is a compile-time
	// sanity check rather than a behavioural test.
	assert.NotEmpty(t, snapshot.DefaultCachePath)
	assert.NotZero(t, snapshot.DefaultCacheTTL)
}

// TestCachedProbeSource_ConcurrentCallers verifies that when multiple goroutines
// race on a missing cache, the Client is called exactly once. All goroutines
// must receive the correct probe list.
func TestCachedProbeSource_ConcurrentCallers(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "probes.json")
	// No file — every goroutine will see a stale/missing cache.

	probes := sampleProbes()
	var callCount atomic.Int32
	client := &countingClient{
		FakeClient: snapshot.FakeClient{Probes: probes},
		counter:    &callCount,
	}

	src := &snapshot.CachedProbeSource{
		Path:   path,
		TTL:    time.Hour,
		Client: client,
	}

	const goroutines = 8
	results := make([][]snapshot.Probe, goroutines)
	errs := make([]error, goroutines)

	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := range goroutines {
		go func() {
			defer wg.Done()
			results[i], errs[i] = src.Probes(context.Background())
		}()
	}
	wg.Wait()

	for i := range goroutines {
		require.NoError(t, errs[i], "goroutine %d returned an error", i)
		assert.Len(t, results[i], len(probes), "goroutine %d got wrong probe count", i)
	}
	assert.Equal(t, int32(1), callCount.Load(), "FetchProbes must be called exactly once")
}

// countingClient wraps FakeClient and tracks the number of FetchProbes calls.
type countingClient struct {
	snapshot.FakeClient
	counter *atomic.Int32
}

func (c *countingClient) FetchProbes(ctx context.Context) ([]snapshot.Probe, error) {
	c.counter.Add(1)
	return c.FakeClient.FetchProbes(ctx)
}
