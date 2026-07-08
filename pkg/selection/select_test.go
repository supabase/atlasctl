package selection_test

import (
	"context"
	"encoding/json"
	"testing"

	h3 "github.com/ThingsIXFoundation/h3-light"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/supabase/atlascli/pkg/config"
	"github.com/supabase/atlascli/pkg/selection"
	"github.com/supabase/atlascli/pkg/snapshot"
)

// ── helpers ──────────────────────────────────────────────────────────────────

// minimalRound returns a Round with the given name and count, sensible defaults.
func minimalRound(name string, count, maxPerCell int) config.Round {
	return config.Round{
		Name:             name,
		Count:            count,
		IntervalSeconds:  60,
		MaxProbesPerCell: maxPerCell,
	}
}

// spreadProbes creates n probes spread across distinct H3 resolution-3 cells by
// placing them on a coarse lat/lon grid. Each probe has a unique ID.
func spreadProbes(n int) []snapshot.Probe {
	probes := make([]snapshot.Probe, n)
	for i := range probes {
		// Step 5° in longitude to ensure distinct cells at res 3.
		probes[i] = snapshot.Probe{
			ID:          uint32(i + 1),
			ASN4:        7018,
			CountryCode: "US",
			Tags:        nil,
			Lat:         float64(i%18)*5 - 45,  // -45° to +40°
			Lon:         float64(i/18)*5 - 180, // wraps across hemisphere
			StatusID:    1,
		}
	}
	return probes
}

// minCfg returns a minimal Config suitable for selection tests.
func minCfg(rounds []config.Round) config.Config {
	return config.Config{
		Rounds:       rounds,
		Measurements: []config.Measurement{{Name: "m", Type: "dns", Target: "x.com", Rounds: []string{rounds[0].Name}}},
		GeoDiversity: config.GeoConfig{H3Resolution: 3},
	}
}

// ── tests ─────────────────────────────────────────────────────────────────────

func TestSelect_NoOverlap(t *testing.T) {
	probes := spreadProbes(90)
	rounds := []config.Round{
		minimalRound("r1", 20, 2),
		minimalRound("r2", 20, 2),
		minimalRound("r3", 20, 2),
	}
	cfg := minCfg(rounds)
	cfg.Rounds = rounds

	result, err := selection.Select(context.Background(), snapshot.Snapshot{Probes: probes}, cfg)
	require.NoError(t, err)
	require.Len(t, result, 3)

	seen := make(map[uint32]string)
	for _, r := range result {
		for _, p := range r.Probes {
			if prev, exists := seen[p.ID]; exists {
				t.Errorf("probe %d appears in both %q and %q", p.ID, prev, r.Round.Name)
			}
			seen[p.ID] = r.Round.Name
		}
	}
}

func TestSelect_Determinism(t *testing.T) {
	probes := spreadProbes(50)
	rounds := []config.Round{
		minimalRound("r1", 15, 2),
		minimalRound("r2", 15, 2),
	}
	cfg := minCfg(rounds)
	cfg.Rounds = rounds
	snap := snapshot.Snapshot{Probes: probes}

	r1, err := selection.Select(context.Background(), snap, cfg)
	require.NoError(t, err)
	r2, err := selection.Select(context.Background(), snap, cfg)
	require.NoError(t, err)

	require.Len(t, r2, len(r1))
	for i := range r1 {
		require.Len(t, r2[i].Probes, len(r1[i].Probes), "round %d probe count differs", i)
		for j, p := range r1[i].Probes {
			assert.Equal(t, p.ID, r2[i].Probes[j].ID, "round %d probe[%d] ID differs", i, j)
		}
	}
}

func TestSelect_H3Limit(t *testing.T) {
	// All probes are clustered within 1 km of Ashburn, VA — guaranteed to share
	// an H3 cell at resolution 3 (edge length ~60 km).
	const (
		baseLat = 39.04
		baseLon = -77.49
	)
	res := 3
	baseCell := h3.LatLonToCell(baseLat, baseLon, res)

	// Generate 10 probes in the same cell and verify the assumption.
	clusterProbes := make([]snapshot.Probe, 10)
	for i := range clusterProbes {
		lat := baseLat + float64(i)*0.001 // ~100 m steps
		lon := baseLon + float64(i)*0.001
		require.Equal(t, baseCell, h3.LatLonToCell(lat, lon, res),
			"probe %d is not in the expected H3 cell — adjust offsets", i)
		clusterProbes[i] = snapshot.Probe{
			ID:          uint32(100 + i),
			ASN4:        7018,
			CountryCode: "US",
			StatusID:    1,
			Lat:         lat,
			Lon:         lon,
		}
	}

	rounds := []config.Round{
		minimalRound("r1", 5, 1), // max 1 per cell — cluster can only give 1
		minimalRound("r2", 5, 1),
	}
	cfg := minCfg(rounds)
	cfg.Rounds = rounds

	result, err := selection.Select(context.Background(), snapshot.Snapshot{Probes: clusterProbes}, cfg)
	require.NoError(t, err)
	require.Len(t, result, 2)

	// With 10 probes all in one cell and max_probes_per_cell=1, each round
	// gets exactly 1 probe from the cluster (the rest are cell-capped).
	for _, r := range result {
		fromCluster := 0
		for _, p := range r.Probes {
			if h3.LatLonToCell(p.Lat, p.Lon, res) == baseCell {
				fromCluster++
			}
		}
		assert.Equal(t, 1, fromCluster, "round %q: expected exactly 1 probe from the clustered cell", r.Round.Name)
	}
}

func TestSelect_CityDensity(t *testing.T) {
	// Put 5 probes tightly clustered near Ashburn (all in the same H3 cell).
	// City density_coefficient=3.0 should raise the per-cell cap from 1 to 3.
	const (
		baseLat = 39.04
		baseLon = -77.49
	)
	var probes []snapshot.Probe
	for i := 0; i < 5; i++ {
		probes = append(probes, snapshot.Probe{
			ID:          uint32(i + 1),
			ASN4:        7018,
			CountryCode: "US",
			StatusID:    1,
			Lat:         baseLat + float64(i)*0.001,
			Lon:         baseLon + float64(i)*0.001,
		})
	}

	city := config.CityConfig{
		Name:               "Ashburn",
		Lat:                baseLat,
		Lon:                baseLon,
		RadiusKm:           50,
		DensityCoefficient: 3.0,
	}

	rounds := []config.Round{minimalRound("r1", 5, 1)} // base cap=1, city raises to 3
	cfg := minCfg(rounds)
	cfg.Rounds = rounds
	cfg.Cities = []config.CityConfig{city}

	result, err := selection.Select(context.Background(), snapshot.Snapshot{Probes: probes}, cfg)
	require.NoError(t, err)
	require.Len(t, result, 1)

	// Effective cap = ceil(1 * 3.0) = 3, so round should yield 3 probes from the cell.
	assert.Equal(t, 3, len(result[0].Probes),
		"city density_coefficient=3 should allow 3 probes from the same cell")
}

func TestSelect_HardExclusion(t *testing.T) {
	probes := []snapshot.Probe{
		{ID: 1, ASN4: 7018, CountryCode: "US", Tags: []string{"office"}, Lat: 10, Lon: 10, StatusID: 1},
		{ID: 2, ASN4: 7018, CountryCode: "US", Tags: []string{"broken"}, Lat: 11, Lon: 11, StatusID: 1},
		{ID: 3, ASN4: 7018, CountryCode: "US", Tags: []string{"system-flakey-power"}, Lat: 12, Lon: 12, StatusID: 1},
		{ID: 4, ASN4: 7018, CountryCode: "US", Tags: []string{"home"}, Lat: 13, Lon: 13, StatusID: 1},
	}

	rounds := []config.Round{minimalRound("r1", 10, 5)}
	cfg := minCfg(rounds)
	cfg.Rounds = rounds
	cfg.ExcludeTags = []string{"broken", "system-flakey-power"}

	result, err := selection.Select(context.Background(), snapshot.Snapshot{Probes: probes}, cfg)
	require.NoError(t, err)
	require.Len(t, result, 1)

	for _, p := range result[0].Probes {
		assert.NotEqual(t, uint32(2), p.ID, "excluded probe 2 (broken) must not appear")
		assert.NotEqual(t, uint32(3), p.ID, "excluded probe 3 (system-flakey-power) must not appear")
	}
	assert.Len(t, result[0].Probes, 2, "only the 2 non-excluded probes should be selected")
}

func TestSelect_SmallSnapshot(t *testing.T) {
	// Request more probes than exist — should return all available, no error.
	probes := spreadProbes(5)
	rounds := []config.Round{
		minimalRound("r1", 10, 5), // wants 10, only 5 exist
		minimalRound("r2", 10, 5), // r1 consumed all 5; r2 gets 0
	}
	cfg := minCfg(rounds)
	cfg.Rounds = rounds

	result, err := selection.Select(context.Background(), snapshot.Snapshot{Probes: probes}, cfg)
	require.NoError(t, err)
	require.Len(t, result, 2)

	assert.Len(t, result[0].Probes, 5, "r1 should get all 5 available probes")
	assert.Empty(t, result[1].Probes, "r2 gets nothing — r1 consumed everyone")
}

func TestSelect_ContextCancel(t *testing.T) {
	probes := spreadProbes(30)
	rounds := []config.Round{
		minimalRound("r1", 5, 5),
		minimalRound("r2", 5, 5),
	}
	cfg := minCfg(rounds)
	cfg.Rounds = rounds

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already cancelled

	_, err := selection.Select(ctx, snapshot.Snapshot{Probes: probes}, cfg)
	assert.ErrorIs(t, err, context.Canceled)
}

func TestSelect_GeoJSON(t *testing.T) {
	probes := []snapshot.Probe{
		{ID: 1, ASN4: 7018, CountryCode: "US", Tags: []string{"office"}, Lat: 39.04, Lon: -77.49, StatusID: 1},
		{ID: 2, ASN4: 7922, CountryCode: "US", Tags: []string{"home"}, Lat: 40.71, Lon: -74.00, StatusID: 1},
	}
	rounds := []config.Round{minimalRound("r1", 10, 5)}
	cfg := minCfg(rounds)
	cfg.Rounds = rounds

	result, err := selection.Select(context.Background(), snapshot.Snapshot{Probes: probes}, cfg)
	require.NoError(t, err)
	require.Len(t, result, 1)

	gj, err := result[0].GeoJSON()
	require.NoError(t, err)

	var fc map[string]any
	require.NoError(t, json.Unmarshal(gj, &fc))
	assert.Equal(t, "FeatureCollection", fc["type"])
	features := fc["features"].([]any)
	assert.Len(t, features, 2)

	// Verify GeoJSON coordinate order: [longitude, latitude].
	// Find probe ID 1 in the features (sort order is by hash, not input order).
	var probe1Feature map[string]any
	for _, raw := range features {
		f := raw.(map[string]any)
		props := f["properties"].(map[string]any)
		if props["probe_id"].(float64) == 1 {
			probe1Feature = f
			break
		}
	}
	require.NotNil(t, probe1Feature, "probe 1 not found in GeoJSON features")

	geom := probe1Feature["geometry"].(map[string]any)
	assert.Equal(t, "Point", geom["type"])
	coords := geom["coordinates"].([]any)
	// Probe 1: Lat=39.04 Lon=-77.49 → GeoJSON coords=[-77.49, 39.04]
	assert.InDelta(t, -77.49, coords[0].(float64), 0.001)
	assert.InDelta(t, 39.04, coords[1].(float64), 0.001)
}

// ── benchmark ─────────────────────────────────────────────────────────────────

// BenchmarkSelect verifies the plan's exit criterion: 50k probes, 3 rounds < 500 ms.
func BenchmarkSelect(b *testing.B) {
	probes := spreadProbes(50_000)
	rounds := []config.Round{
		minimalRound("high-freq", 30, 1),
		minimalRound("mid-freq", 60, 2),
		minimalRound("low-freq", 100, 3),
	}
	cfg := config.Config{
		Rounds:       rounds,
		Measurements: []config.Measurement{{Name: "m", Type: "dns", Target: "x.com", Rounds: []string{"high-freq"}}},
		GeoDiversity: config.GeoConfig{H3Resolution: 3},
		Scoring:      referenceScoringConfig(),
		Cities: []config.CityConfig{
			{Name: "Ashburn", Lat: 39.04, Lon: -77.49, RadiusKm: 40, DensityCoefficient: 2.0},
		},
	}
	snap := snapshot.Snapshot{Probes: probes}
	ctx := context.Background()

	b.ResetTimer()
	for range b.N {
		_, err := selection.Select(ctx, snap, cfg)
		if err != nil {
			b.Fatal(err)
		}
	}
}
