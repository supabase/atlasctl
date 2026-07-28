package selection_test

import (
	"context"
	"encoding/json"
	"testing"

	h3 "github.com/ThingsIXFoundation/h3-light"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/supabase/atlasctl/pkg/config"
	"github.com/supabase/atlasctl/pkg/selection"
	"github.com/supabase/atlasctl/pkg/snapshot"
)

// ── helpers ──────────────────────────────────────────────────────────────────

// minimalCohort returns a MeasurementCohort with sensible defaults.
func minimalCohort(name string, count, maxPerCell int) config.MeasurementCohort {
	return config.MeasurementCohort{
		Name:             name,
		ProbeCount:       count,
		MaxProbesPerCell: maxPerCell,
		IntervalSeconds:  60,
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

// makeProbes builds a closed *selection.Probes from a probe slice.
func makeProbes(ps []snapshot.Probe) *selection.Probes {
	p := selection.NewProbes(len(ps))
	for _, probe := range ps {
		p.Append(probe)
	}
	p.Close()
	return p
}

// defaultOrderer wraps NewDefaultOrderer at H3 resolution 3 for test convenience.
func defaultOrderer() selection.ProbeOrderer {
	return selection.NewDefaultOrderer()
}

// ── tests ─────────────────────────────────────────────────────────────────────

func TestSelect_NoOverlap(t *testing.T) {
	probes := makeProbes(spreadProbes(90))
	cohorts := []config.MeasurementCohort{
		minimalCohort("r1", 20, 2),
		minimalCohort("r2", 20, 2),
		minimalCohort("r3", 20, 2),
	}

	result, err := selection.Select(context.Background(), probes, cohorts, defaultOrderer())
	require.NoError(t, err)
	require.Len(t, result, 3)

	seen := make(map[uint32]string)
	for _, r := range result {
		for _, p := range r.Probes {
			if prev, exists := seen[p.ID]; exists {
				t.Errorf("probe %d appears in both %q and %q", p.ID, prev, r.Cohort.Name)
			}
			seen[p.ID] = r.Cohort.Name
		}
	}
}

func TestSelect_Determinism(t *testing.T) {
	probes := makeProbes(spreadProbes(50))
	cohorts := []config.MeasurementCohort{
		minimalCohort("r1", 15, 2),
		minimalCohort("r2", 15, 2),
	}

	// Use the same orderer for both runs to exercise the cache on the second run.
	ord := defaultOrderer()
	r1, err := selection.Select(context.Background(), probes, cohorts, ord)
	require.NoError(t, err)
	r2, err := selection.Select(context.Background(), probes, cohorts, ord)
	require.NoError(t, err)

	require.Len(t, r2, len(r1))
	for i := range r1 {
		require.Len(t, r2[i].Probes, len(r1[i].Probes), "cohort %d probe count differs", i)
		for j, p := range r1[i].Probes {
			assert.Equal(t, p.ID, r2[i].Probes[j].ID, "cohort %d probe[%d] ID differs", i, j)
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

	probes := makeProbes(clusterProbes)
	cohorts := []config.MeasurementCohort{
		minimalCohort("r1", 5, 1), // max 1 per cell — cluster can only give 1
		minimalCohort("r2", 5, 1),
	}

	result, err := selection.Select(context.Background(), probes, cohorts, defaultOrderer())
	require.NoError(t, err)
	require.Len(t, result, 2)

	// With 10 probes all in one cell and max_probes_per_cell=1, each cohort
	// gets exactly 1 probe from the cluster (the rest are cell-capped).
	for _, r := range result {
		fromCluster := 0
		for _, p := range r.Probes {
			if h3.LatLonToCell(p.Lat, p.Lon, res) == baseCell {
				fromCluster++
			}
		}
		assert.Equal(t, 1, fromCluster, "cohort %q: expected exactly 1 probe from the clustered cell", r.Cohort.Name)
	}
}

func TestSelect_CityDensityReduction(t *testing.T) {
	// 5 probes in the same H3 cell near Ashburn.
	// Base cap=4, coefficient=0.5 → ceil(4 * 0.5) = 2 probes allowed.
	const (
		baseLat = 39.04
		baseLon = -77.49
	)
	var rawProbes []snapshot.Probe
	for i := 0; i < 5; i++ {
		rawProbes = append(rawProbes, snapshot.Probe{
			ID:       uint32(i + 1),
			StatusID: 1,
			Lat:      baseLat + float64(i)*0.001,
			Lon:      baseLon + float64(i)*0.001,
		})
	}

	city := config.CityConfig{
		Name:               "Ashburn",
		Lat:                baseLat,
		Lon:                baseLon,
		RadiusKm:           50,
		DensityCoefficient: 0.5,
	}

	cohort := minimalCohort("r1", 5, 4) // base cap=4, city reduces to ceil(4*0.5)=2
	cohort.Cfg.Cities = []config.CityConfig{city}

	result, err := selection.Select(context.Background(), makeProbes(rawProbes), []config.MeasurementCohort{cohort}, defaultOrderer())
	require.NoError(t, err)
	require.Len(t, result, 1)

	assert.Equal(t, 2, len(result[0].Probes),
		"density_coefficient=0.5 should reduce cap from 4 to ceil(4*0.5)=2")
}

func TestSelect_CityDensityIncrease(t *testing.T) {
	// 5 probes tightly clustered near Ashburn (all in the same H3 cell).
	// City density_coefficient=3.0 should raise the per-cell cap from 1 to 3.
	const (
		baseLat = 39.04
		baseLon = -77.49
	)
	var rawProbes []snapshot.Probe
	for i := 0; i < 5; i++ {
		rawProbes = append(rawProbes, snapshot.Probe{
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

	cohort := minimalCohort("r1", 5, 1) // base cap=1, city raises to ceil(1*3.0)=3
	cohort.Cfg.Cities = []config.CityConfig{city}

	result, err := selection.Select(context.Background(), makeProbes(rawProbes), []config.MeasurementCohort{cohort}, defaultOrderer())
	require.NoError(t, err)
	require.Len(t, result, 1)

	assert.Equal(t, 3, len(result[0].Probes),
		"city density_coefficient=3 should allow 3 probes from the same cell")
}

func TestSelect_CityDensityIsPerCohort(t *testing.T) {
	// Same probe pool, two cohorts with different city configs. Verify that
	// each cohort's density coefficient is applied independently.
	const (
		baseLat = 39.04
		baseLon = -77.49
	)
	var rawProbes []snapshot.Probe
	for i := 0; i < 5; i++ {
		rawProbes = append(rawProbes, snapshot.Probe{
			ID:       uint32(i + 1),
			StatusID: 1,
			Lat:      baseLat + float64(i)*0.001,
			Lon:      baseLon + float64(i)*0.001,
		})
	}
	probes := makeProbes(rawProbes)

	ashburn := config.CityConfig{Name: "Ashburn", Lat: baseLat, Lon: baseLon, RadiusKm: 50}

	// Cohort 1: coefficient=3.0 → cap raised from 1 to 3.
	c1 := minimalCohort("dense", 5, 1)
	high := ashburn
	high.DensityCoefficient = 3.0
	c1.Cfg.Cities = []config.CityConfig{high}

	// Cohort 2: no city config → default cap=1 applies.
	c2 := minimalCohort("sparse", 5, 1)

	result, err := selection.Select(context.Background(), probes, []config.MeasurementCohort{c1, c2}, defaultOrderer())
	require.NoError(t, err)
	require.Len(t, result, 2)

	assert.Equal(t, 3, len(result[0].Probes), "dense cohort: city coefficient=3 gives 3 probes from cluster")
	assert.Equal(t, 1, len(result[1].Probes), "sparse cohort: no city config, only 1 probe left in cluster")
}

func TestSelect_CityScore(t *testing.T) {
	// Two groups of identical generic probes in different locations.
	// Group A is near Ashburn, which has score=20 → pushed to BandA.
	// Group B is far away with no city bonus → score=1 (BandD).
	// With cap=1 per cell and count=5, selection should prefer Group A.
	const (
		ashburnLat = 39.04
		ashburnLon = -77.49
	)

	var rawProbes []snapshot.Probe
	// 5 probes near Ashburn (spread across different H3 cells).
	for i := 0; i < 5; i++ {
		rawProbes = append(rawProbes, snapshot.Probe{
			ID:       uint32(i + 1),
			StatusID: 1,
			Lat:      ashburnLat + float64(i)*0.5,
			Lon:      ashburnLon + float64(i)*0.5,
		})
	}
	// 5 probes far away (Tokyo area).
	for i := 0; i < 5; i++ {
		rawProbes = append(rawProbes, snapshot.Probe{
			ID:       uint32(i + 100),
			StatusID: 1,
			Lat:      35.68 + float64(i)*0.5,
			Lon:      139.69 + float64(i)*0.5,
		})
	}

	city := config.CityConfig{
		Name:     "Ashburn",
		Lat:      ashburnLat,
		Lon:      ashburnLon,
		RadiusKm: 300, // generous radius to cover the spread
		Score:    20,
	}

	cohort := minimalCohort("r1", 5, 1)
	cohort.Cfg.Cities = []config.CityConfig{city}

	result, err := selection.Select(context.Background(), makeProbes(rawProbes), []config.MeasurementCohort{cohort}, defaultOrderer())
	require.NoError(t, err)
	require.Len(t, result, 1)
	require.Len(t, result[0].Probes, 5)

	// All selected probes should be from the Ashburn group (IDs 1–5).
	for _, p := range result[0].Probes {
		assert.Less(t, p.ID, uint32(100), "expected Ashburn probe (ID<100), got ID=%d", p.ID)
	}
}

func TestSelect_HardExclusion(t *testing.T) {
	// Tag exclusion is per-cohort via CohortCfg.ExcludeTags. The full probe pool
	// is passed to Select; excluded probes are filtered inside the selection loop.
	rawProbes := []snapshot.Probe{
		{ID: 1, ASN4: 7018, CountryCode: "US", Tags: []string{"office"}, Lat: 10, Lon: 10, StatusID: 1},
		{ID: 2, ASN4: 7018, CountryCode: "US", Tags: []string{"broken"}, Lat: 11, Lon: 11, StatusID: 1},
		{ID: 3, ASN4: 7018, CountryCode: "US", Tags: []string{"system-flakey-power"}, Lat: 12, Lon: 12, StatusID: 1},
		{ID: 4, ASN4: 7018, CountryCode: "US", Tags: []string{"home"}, Lat: 13, Lon: 13, StatusID: 1},
	}

	probes := selection.NewProbes(len(rawProbes))
	for _, p := range rawProbes {
		probes.Append(p)
	}
	probes.Close()

	cohort := minimalCohort("r1", 10, 5)
	cohort.Cfg.ExcludeTags = []string{"broken", "system-flakey-power"}

	result, err := selection.Select(context.Background(), probes, []config.MeasurementCohort{cohort}, defaultOrderer())
	require.NoError(t, err)
	require.Len(t, result, 1)

	for _, p := range result[0].Probes {
		assert.NotEqual(t, uint32(2), p.ID, "excluded probe 2 (broken) must not appear")
		assert.NotEqual(t, uint32(3), p.ID, "excluded probe 3 (system-flakey-power) must not appear")
	}
	assert.Len(t, result[0].Probes, 2, "only the 2 non-excluded probes should be selected")
}

func TestSelect_ExcludeProbeIDs(t *testing.T) {
	rawProbes := spreadProbes(10)
	probes := makeProbes(rawProbes)

	cohort := minimalCohort("r1", 10, 5)
	cohort.ExcludeProbeIDs = []uint32{1, 2, 3}

	result, err := selection.Select(context.Background(), probes, []config.MeasurementCohort{cohort}, defaultOrderer())
	require.NoError(t, err)
	require.Len(t, result, 1)

	for _, p := range result[0].Probes {
		assert.NotEqual(t, uint32(1), p.ID)
		assert.NotEqual(t, uint32(2), p.ID)
		assert.NotEqual(t, uint32(3), p.ID)
	}
	assert.Len(t, result[0].Probes, 7, "10 probes minus 3 excluded = 7")
}

func TestSelect_IncludeProbeIDs(t *testing.T) {
	rawProbes := spreadProbes(10)
	probes := makeProbes(rawProbes)

	cohort := minimalCohort("r1", 5, 1)
	cohort.IncludeProbeIDs = []uint32{5, 6} // guaranteed to appear in results

	result, err := selection.Select(context.Background(), probes, []config.MeasurementCohort{cohort}, defaultOrderer())
	require.NoError(t, err)
	require.Len(t, result, 1)
	require.Len(t, result[0].Probes, 5)

	includedIDs := make(map[uint32]bool)
	for _, p := range result[0].Probes {
		includedIDs[p.ID] = true
	}
	assert.True(t, includedIDs[5], "probe 5 must be included")
	assert.True(t, includedIDs[6], "probe 6 must be included")
}

func TestSelect_IncludeBypassesH3Cap(t *testing.T) {
	// All probes in the same H3 cell, cap=1. Two probes are forced via
	// IncludeProbeIDs. They should both appear despite the cap=1 limit.
	const (
		baseLat = 39.04
		baseLon = -77.49
	)
	clusterProbes := make([]snapshot.Probe, 5)
	for i := range clusterProbes {
		clusterProbes[i] = snapshot.Probe{
			ID:          uint32(i + 1),
			ASN4:        7018,
			CountryCode: "US",
			StatusID:    1,
			Lat:         baseLat + float64(i)*0.001,
			Lon:         baseLon + float64(i)*0.001,
		}
	}

	cohort := minimalCohort("r1", 5, 1) // cap=1 per cell
	cohort.IncludeProbeIDs = []uint32{1, 2}

	result, err := selection.Select(context.Background(), makeProbes(clusterProbes), []config.MeasurementCohort{cohort}, defaultOrderer())
	require.NoError(t, err)
	require.Len(t, result, 1)

	ids := make(map[uint32]bool)
	for _, p := range result[0].Probes {
		ids[p.ID] = true
	}
	assert.True(t, ids[1], "probe 1 must be included (bypasses H3 cap)")
	assert.True(t, ids[2], "probe 2 must be included (bypasses H3 cap)")
	// Only 1 additional probe can be selected due to the cap; total = 3.
	assert.Len(t, result[0].Probes, 3)
}

func TestSelect_IncludeOverridesExclusions(t *testing.T) {
	// include_probe_ids takes precedence over exclude_probe_ids, exclude_tags,
	// and inter-cohort exclusions. The only thing that blocks a pinned probe
	// is absence from the snapshot.
	rawProbes := []snapshot.Probe{
		{ID: 1, ASN4: 7018, CountryCode: "US", Tags: []string{"starlink"}, Lat: 10, Lon: 10, StatusID: 1},
		{ID: 2, ASN4: 7018, CountryCode: "US", Tags: []string{"office"}, Lat: 11, Lon: 11, StatusID: 1},
		{ID: 3, ASN4: 7018, CountryCode: "US", Tags: []string{"home"}, Lat: 12, Lon: 12, StatusID: 1},
	}
	probes := makeProbes(rawProbes)

	// Cohort 1 selects probe 2, making it inter-cohort excluded for cohort 2.
	// Cohort 2 pins probes 1 (tag-excluded) and 2 (inter-cohort excluded) and
	// also lists probe 1 in exclude_probe_ids. All three exclusions must lose.
	cohort1 := minimalCohort("c1", 1, 5)
	cohort2 := minimalCohort("c2", 2, 5)
	cohort2.IncludeProbeIDs = []uint32{1, 2}
	cohort2.ExcludeProbeIDs = []uint32{1}
	cohort2.Cfg.ExcludeTags = []string{"starlink"}

	result, err := selection.Select(context.Background(), probes, []config.MeasurementCohort{cohort1, cohort2}, defaultOrderer())
	require.NoError(t, err)
	require.Len(t, result, 2)

	ids := make(map[uint32]bool)
	for _, p := range result[1].Probes {
		ids[p.ID] = true
	}
	assert.True(t, ids[1], "probe 1 must be included despite tag exclusion and ExcludeProbeIDs")
	assert.True(t, ids[2], "probe 2 must be included despite inter-cohort exclusion")
}

func TestSelect_SmallSnapshot(t *testing.T) {
	// Request more probes than exist — should return all available, no error.
	probes := makeProbes(spreadProbes(5))
	cohorts := []config.MeasurementCohort{
		minimalCohort("r1", 10, 5), // wants 10, only 5 exist
		minimalCohort("r2", 10, 5), // r1 consumed all 5; r2 gets 0
	}

	result, err := selection.Select(context.Background(), probes, cohorts, defaultOrderer())
	require.NoError(t, err)
	require.Len(t, result, 2)

	assert.Len(t, result[0].Probes, 5, "r1 should get all 5 available probes")
	assert.Empty(t, result[1].Probes, "r2 gets nothing — r1 consumed everyone")
}

func TestSelect_ContextCancel(t *testing.T) {
	probes := makeProbes(spreadProbes(30))
	cohorts := []config.MeasurementCohort{
		minimalCohort("r1", 5, 5),
		minimalCohort("r2", 5, 5),
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already cancelled

	_, err := selection.Select(ctx, probes, cohorts, defaultOrderer())
	assert.ErrorIs(t, err, context.Canceled)
}

func TestSelect_GeoJSON(t *testing.T) {
	rawProbes := []snapshot.Probe{
		{ID: 1, ASN4: 7018, CountryCode: "US", Tags: []string{"office"}, Lat: 39.04, Lon: -77.49, StatusID: 1},
		{ID: 2, ASN4: 7922, CountryCode: "US", Tags: []string{"home"}, Lat: 40.71, Lon: -74.00, StatusID: 1},
	}
	probes := makeProbes(rawProbes)
	cohorts := []config.MeasurementCohort{minimalCohort("r1", 10, 5)}

	result, err := selection.Select(context.Background(), probes, cohorts, defaultOrderer())
	require.NoError(t, err)
	require.Len(t, result, 1)

	gj, err := selection.GeoJSON(result)
	require.NoError(t, err)

	var fc map[string]any
	require.NoError(t, json.Unmarshal(gj, &fc))
	assert.Equal(t, "FeatureCollection", fc["type"])
	features := fc["features"].([]any)
	assert.Len(t, features, 2)

	// Verify GeoJSON coordinate order: [longitude, latitude].
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
	assert.InDelta(t, -77.49, coords[0].(float64), 0.001)
	assert.InDelta(t, 39.04, coords[1].(float64), 0.001)

	props := probe1Feature["properties"].(map[string]any)
	assert.Equal(t, "r1", props["cohort"])
}

func TestSelect_ContinentalInterleaving(t *testing.T) {
	// 8 Band-B probes from NA (US) and 4 Band-B probes from EU (DE), all with the
	// same ASN score so they land in the same band. Each probe is in a distinct H3
	// cell (coordinates are ~5° apart, far exceeding the ~60 km cell edge at res 3).
	//
	// Without interleaving: hash order produces roughly 4 NA + 2 EU for a 6-slot
	// cohort (NA has 2× more candidates). With interleaving the algorithm alternates
	// NA and EU within Band B, yielding exactly 3 NA + 3 EU.

	var rawProbes []snapshot.Probe

	// 8 NA probes, each ~5° apart across North America.
	for i := range 8 {
		rawProbes = append(rawProbes, snapshot.Probe{
			ID:          uint32(i + 1),
			ASN4:        7018,
			CountryCode: "US",
			StatusID:    1,
			Lat:         35 + float64(i)*4,
			Lon:         -100 + float64(i)*3,
		})
	}
	// 4 EU probes, each ~5° apart across Europe.
	for i := range 4 {
		rawProbes = append(rawProbes, snapshot.Probe{
			ID:          uint32(100 + i),
			ASN4:        7018,
			CountryCode: "DE",
			StatusID:    1,
			Lat:         48 + float64(i)*5,
			Lon:         8 + float64(i)*5,
		})
	}

	// ASN 7018 weight 10 → score = 11 (base 1 + ASN 10) → Band B (threshold 8-14).
	cohort := minimalCohort("r1", 6, 1)
	cohort.Cfg.ScoringConfig = config.ScoringConfig{
		ASN: map[uint32]int{7018: 10},
	}

	result, err := selection.Select(context.Background(), makeProbes(rawProbes), []config.MeasurementCohort{cohort}, defaultOrderer())
	require.NoError(t, err)
	require.Len(t, result, 1)
	require.Len(t, result[0].Probes, 6, "cohort should fill to count=6")

	naCount, euCount := 0, 0
	for _, p := range result[0].Probes {
		switch p.CountryCode {
		case "US":
			naCount++
		case "DE":
			euCount++
		}
	}
	// Interleaving: Band-B round-robin → NA, EU, NA, EU, NA, EU → 3 each.
	assert.Equal(t, 3, naCount, "interleaving should pick 3 of 8 NA probes")
	assert.Equal(t, 3, euCount, "interleaving should pick 3 of 4 EU probes")
}

func TestSelect_DisableContinentalShuffle(t *testing.T) {
	// 4 NA and 4 EU probes, all Band-B. With interleaving, a 4-slot cohort yields
	// 2 NA + 2 EU. With shuffle disabled, it follows hash order which clusters by
	// whichever zone sorts first — not 2+2.
	var rawProbes []snapshot.Probe
	for i := range 4 {
		rawProbes = append(rawProbes, snapshot.Probe{
			ID: uint32(i + 1), ASN4: 7018, CountryCode: "US", StatusID: 1,
			Lat: 35 + float64(i)*5, Lon: -100 + float64(i)*5,
		})
	}
	for i := range 4 {
		rawProbes = append(rawProbes, snapshot.Probe{
			ID: uint32(100 + i), ASN4: 7018, CountryCode: "DE", StatusID: 1,
			Lat: 48 + float64(i)*5, Lon: 8 + float64(i)*5,
		})
	}
	probes := makeProbes(rawProbes)

	scoring := config.ScoringConfig{ASN: map[uint32]int{7018: 10}}

	shuffled := minimalCohort("shuffled", 4, 1)
	shuffled.Cfg.ScoringConfig = scoring

	noShuffle := minimalCohort("no-shuffle", 4, 1)
	noShuffle.Cfg.ScoringConfig = scoring
	noShuffle.Cfg.DisableContinentalShuffle = true

	rShuffled, err := selection.Select(context.Background(), probes, []config.MeasurementCohort{shuffled}, defaultOrderer())
	require.NoError(t, err)

	rNoShuffle, err := selection.Select(context.Background(), probes, []config.MeasurementCohort{noShuffle}, defaultOrderer())
	require.NoError(t, err)

	naShuffled, euShuffled := 0, 0
	for _, p := range rShuffled[0].Probes {
		if p.CountryCode == "US" {
			naShuffled++
		} else {
			euShuffled++
		}
	}
	// With interleaving: should be 2 NA + 2 EU.
	assert.Equal(t, 2, naShuffled)
	assert.Equal(t, 2, euShuffled)

	naNoShuffle, euNoShuffle := 0, 0
	for _, p := range rNoShuffle[0].Probes {
		if p.CountryCode == "US" {
			naNoShuffle++
		} else {
			euNoShuffle++
		}
	}
	// Without interleaving: hash order → likely all one zone dominates.
	assert.NotEqual(t, 2, naNoShuffle, "without shuffle, distribution should not be balanced 2+2")
}

func TestSelect_OrdererCacheHit(t *testing.T) {
	// The same orderer is reused across two Select calls with the same probe set
	// and cohort cfg. Both calls must return identical probe orderings, confirming
	// the cache key is stable and results are consistent.
	rawProbes := spreadProbes(20)
	probes := makeProbes(rawProbes)
	cohorts := []config.MeasurementCohort{minimalCohort("r1", 10, 2)}

	ord := defaultOrderer()

	r1, err := selection.Select(context.Background(), probes, cohorts, ord)
	require.NoError(t, err)

	r2, err := selection.Select(context.Background(), probes, cohorts, ord)
	require.NoError(t, err)

	require.Equal(t, len(r1[0].Probes), len(r2[0].Probes))
	for i, p := range r1[0].Probes {
		assert.Equal(t, p.ID, r2[0].Probes[i].ID, "probe[%d] ID should match on repeated call", i)
	}
}

// ── benchmark ─────────────────────────────────────────────────────────────────

// BenchmarkSelect verifies the plan's exit criterion: 50k probes, 3 cohorts < 500 ms.
// A fresh orderer is created each iteration to include scoring cost.
func BenchmarkSelect(b *testing.B) {
	rawProbes := spreadProbes(50_000)
	probes := selection.NewProbes(len(rawProbes))
	for _, p := range rawProbes {
		probes.Append(p)
	}
	probes.Close()

	cohorts := []config.MeasurementCohort{
		minimalCohort("high-freq", 30, 1),
		minimalCohort("mid-freq", 60, 2),
		minimalCohort("low-freq", 100, 3),
	}
	// Inject a city so scoring exercises the city bonus path.
	city := config.CityConfig{Name: "Ashburn", Lat: 39.04, Lon: -77.49, RadiusKm: 40, DensityCoefficient: 2.0}
	for i := range cohorts {
		cohorts[i].Cfg.Cities = []config.CityConfig{city}
		cohorts[i].Cfg.ScoringConfig = referenceScoringConfig()
	}
	ctx := context.Background()

	b.ResetTimer()
	for range b.N {
		// New orderer each iteration to include scoring cost in the measurement.
		ord := selection.NewDefaultOrderer()
		_, err := selection.Select(ctx, probes, cohorts, ord)
		if err != nil {
			b.Fatal(err)
		}
	}
}
