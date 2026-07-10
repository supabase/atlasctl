package snapshot_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/supabase/atlasctl/pkg/snapshot"
)

// sampleProbes returns a small deterministic set of probes for use in tests.
func sampleProbes() []snapshot.Probe {
	return []snapshot.Probe{
		{
			ID:          1001,
			ASN4:        7018,
			CountryCode: "US",
			Tags:        []string{"office", "fibre", "system-ipv4-stable-90d"},
			Lat:         38.9,
			Lon:         -77.0,
			StatusID:    1,
		},
		{
			ID:          2002,
			ASN4:        28573,
			CountryCode: "BR",
			Tags:        []string{"home", "cable"},
			Lat:         -23.55,
			Lon:         -46.63,
			StatusID:    1,
		},
		{
			ID:          3003,
			ASN4:        0,
			CountryCode: "DE",
			Tags:        []string{"datacentre"},
			Lat:         52.5,
			Lon:         13.4,
			StatusID:    1,
		},
	}
}

func TestSnapshot_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "snapshot.json")

	original := snapshot.Snapshot{
		Probes:    sampleProbes(),
		FetchedAt: time.Date(2026, 7, 7, 12, 0, 0, 0, time.UTC),
	}

	require.NoError(t, snapshot.Save(path, original))

	loaded, err := snapshot.Load(path)
	require.NoError(t, err)

	assert.Equal(t, original.FetchedAt.UTC(), loaded.FetchedAt.UTC())
	require.Len(t, loaded.Probes, len(original.Probes))
	for i, want := range original.Probes {
		got := loaded.Probes[i]
		assert.Equal(t, want.ID, got.ID, "probe[%d] ID", i)
		assert.Equal(t, want.ASN4, got.ASN4, "probe[%d] ASN4", i)
		assert.Equal(t, want.CountryCode, got.CountryCode, "probe[%d] CountryCode", i)
		assert.Equal(t, want.Tags, got.Tags, "probe[%d] Tags", i)
		assert.InDelta(t, want.Lat, got.Lat, 1e-9, "probe[%d] Lat", i)
		assert.InDelta(t, want.Lon, got.Lon, 1e-9, "probe[%d] Lon", i)
		assert.Equal(t, want.StatusID, got.StatusID, "probe[%d] StatusID", i)
	}
}

func TestLoad_Missing(t *testing.T) {
	_, err := snapshot.Load("/nonexistent/path/snapshot.json")
	require.Error(t, err)
	assert.ErrorIs(t, err, snapshot.ErrNotFound)
}

func TestFetchAndSave(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "snapshot.json")

	probes := sampleProbes()
	client := &snapshot.FakeClient{Probes: probes}

	fetched, err := client.FetchProbes(context.Background())
	require.NoError(t, err)
	require.Len(t, fetched, len(probes))

	snap := snapshot.Snapshot{
		Probes:    fetched,
		FetchedAt: time.Now().UTC(),
	}
	require.NoError(t, snapshot.Save(path, snap))

	loaded, err := snapshot.Load(path)
	require.NoError(t, err)
	assert.Len(t, loaded.Probes, len(probes))
	for i, want := range probes {
		assert.Equal(t, want.ID, loaded.Probes[i].ID, "probe[%d] ID", i)
		assert.Equal(t, want.ASN4, loaded.Probes[i].ASN4, "probe[%d] ASN4", i)
	}
}

func TestFakeClient_Error(t *testing.T) {
	sentinel := errors.New("api unavailable")
	client := &snapshot.FakeClient{Err: sentinel}

	_, err := client.FetchProbes(context.Background())
	require.ErrorIs(t, err, sentinel)
}

func TestSave_AtomicWrite(t *testing.T) {
	// Verify that Save does not leave a temp file behind on success.
	dir := t.TempDir()
	path := filepath.Join(dir, "snapshot.json")

	snap := snapshot.Snapshot{Probes: sampleProbes(), FetchedAt: time.Now().UTC()}
	require.NoError(t, snapshot.Save(path, snap))

	entries, err := filepath.Glob(filepath.Join(dir, ".snapshot-*.json.tmp"))
	require.NoError(t, err)
	assert.Empty(t, entries, "no temp files should remain after successful Save")
}
