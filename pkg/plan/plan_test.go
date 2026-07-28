package plan_test

import (
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/supabase/atlasctl/pkg/config"
	"github.com/supabase/atlasctl/pkg/plan"
	"github.com/supabase/atlasctl/pkg/selection"
	"github.com/supabase/atlasctl/pkg/snapshot"
)

// ── helpers ───────────────────────────────────────────────────────────────────

// findChanges returns all changes in cs that match key and kind.
func findChanges(cs plan.Changeset, key plan.MsmKey, kind plan.ChangeKind) []plan.Change {
	var out []plan.Change
	for _, c := range cs {
		if c.Key == key && c.Kind == kind {
			out = append(out, c)
		}
	}
	return out
}

// findChange returns the single change matching (key, kind), or fails the test.
func findChange(t *testing.T, cs plan.Changeset, key plan.MsmKey, kind plan.ChangeKind) plan.Change {
	t.Helper()
	matches := findChanges(cs, key, kind)
	require.Len(t, matches, 1, "expected exactly one %s change for key %+v", kind, key)
	return matches[0]
}

// sortedU32 returns a sorted copy of ids for deterministic comparison.
func sortedU32(ids []uint32) []uint32 {
	out := make([]uint32, len(ids))
	copy(out, ids)
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// sampleState builds a StateFile with one or more measurement records.
func sampleState(entries ...struct {
	name, cohort string
	rec          plan.MsmRecord
},
) plan.StateFile {
	sf := plan.NewStateFile()
	for _, e := range entries {
		sf.SetRecord(e.name, e.cohort, e.rec)
	}
	return sf
}

// dnsRecord returns a representative MsmRecord for a DNS measurement.
func dnsRecord(msmID uint64, probeIDs ...uint32) plan.MsmRecord {
	return plan.MsmRecord{
		MsmID:    msmID,
		Target:   "canary.supabase.co",
		Type:     "dns",
		Interval: 60,
		ProbeIDs: probeIDs,
	}
}

// ── state file ────────────────────────────────────────────────────────────────

func TestSaveLoad_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.yaml")

	original := plan.NewStateFile()
	original.LastApplied = time.Date(2026, 7, 7, 12, 0, 0, 0, time.UTC)
	original.ProbeSnapshot = "probes/snapshot.json"

	original.SetRecord("dns-canary", "high-freq", plan.MsmRecord{
		MsmID: 11111111, Target: "canary.supabase.co", Type: "dns",
		Interval: 60, ProbeIDs: []uint32{1001, 2002, 3003},
	})
	original.SetRecord("dns-canary", "mid-freq", plan.MsmRecord{
		MsmID: 22222222, Target: "canary.supabase.co", Type: "dns",
		Interval: 300, ProbeIDs: []uint32{4004, 5005},
	})
	original.SetRecord("tls-canary", "high-freq", plan.MsmRecord{
		MsmID: 33333333, Target: "canary.supabase.co", Type: "tls",
		Interval: 60, ProbeIDs: []uint32{1001, 6006},
	})

	require.NoError(t, plan.SaveState(path, original))

	loaded, err := plan.LoadState(path)
	require.NoError(t, err)

	assert.Equal(t, original.LastApplied.UTC(), loaded.LastApplied.UTC())
	assert.Equal(t, original.ProbeSnapshot, loaded.ProbeSnapshot)

	for _, name := range []string{"dns-canary", "tls-canary"} {
		for _, cohort := range []string{"high-freq", "mid-freq"} {
			orig, origOK := original.GetRecord(name, cohort)
			got, gotOK := loaded.GetRecord(name, cohort)
			require.Equal(t, origOK, gotOK, "GetRecord(%s, %s) existence", name, cohort)
			if origOK {
				assert.Equal(t, orig.MsmID, got.MsmID, "MsmID for (%s, %s)", name, cohort)
				assert.Equal(t, orig.Target, got.Target)
				assert.Equal(t, orig.Type, got.Type)
				assert.Equal(t, orig.Interval, got.Interval)
				assert.Equal(t, sortedU32(orig.ProbeIDs), sortedU32(got.ProbeIDs))
			}
		}
	}
}

func TestLoadState_Missing(t *testing.T) {
	_, err := plan.LoadState("/nonexistent/path/state.yaml")
	require.Error(t, err)
	assert.ErrorIs(t, err, plan.ErrStateNotFound)
}

func TestStateFile_SetDeleteRecord(t *testing.T) {
	sf := plan.NewStateFile()
	sf.SetRecord("m", "r", dnsRecord(1))

	rec, ok := sf.GetRecord("m", "r")
	require.True(t, ok)
	assert.Equal(t, uint64(1), rec.MsmID)

	sf.DeleteRecord("m", "r")
	_, ok = sf.GetRecord("m", "r")
	assert.False(t, ok, "record should be gone after DeleteRecord")

	// Deleting the last cohort should remove the outer map entry too.
	assert.Empty(t, sf.Measurements, "outer map should be empty after last cohort removed")
}

// ── diff ──────────────────────────────────────────────────────────────────────

func TestDiff_Create(t *testing.T) {
	key := plan.MsmKey{Name: "dns-canary", Cohort: "high-freq"}
	desired := map[plan.MsmKey]plan.MsmSpec{
		key: {Target: "canary.supabase.co", Type: plan.MsmTypeDNS, Interval: 60, ProbeIDs: []uint32{1, 2, 3}},
	}
	cs := plan.Diff(desired, plan.NewStateFile())

	c := findChange(t, cs, key, plan.ChangeCreate)
	require.NotNil(t, c.Desired)
	assert.Equal(t, "canary.supabase.co", c.Desired.Target)
	assert.Equal(t, []uint32{1, 2, 3}, c.Desired.ProbeIDs)
}

func TestDiff_NoOp(t *testing.T) {
	key := plan.MsmKey{Name: "dns-canary", Cohort: "high-freq"}
	probes := []uint32{1001, 2002, 3003}

	desired := map[plan.MsmKey]plan.MsmSpec{
		key: {Target: "canary.supabase.co", Type: plan.MsmTypeDNS, Interval: 60, ProbeIDs: probes},
	}
	state := sampleState(struct {
		name, cohort string
		rec          plan.MsmRecord
	}{"dns-canary", "high-freq", plan.MsmRecord{
		MsmID: 12345678, Target: "canary.supabase.co", Type: "dns",
		Interval: 60, ProbeIDs: probes,
	}})

	cs := plan.Diff(desired, state)

	c := findChange(t, cs, key, plan.ChangeNoOp)
	assert.Equal(t, uint64(12345678), c.MsmID)
}

func TestDiff_Stop(t *testing.T) {
	key := plan.MsmKey{Name: "dns-canary", Cohort: "high-freq"}
	state := sampleState(struct {
		name, cohort string
		rec          plan.MsmRecord
	}{"dns-canary", "high-freq", dnsRecord(99999999, 1, 2, 3)})

	// Empty desired — everything in state should be stopped.
	cs := plan.Diff(map[plan.MsmKey]plan.MsmSpec{}, state)

	c := findChange(t, cs, key, plan.ChangeStop)
	assert.Equal(t, uint64(99999999), c.MsmID)
}

func TestDiff_ProbeSetChanged(t *testing.T) {
	key := plan.MsmKey{Name: "dns-canary", Cohort: "high-freq"}

	desired := map[plan.MsmKey]plan.MsmSpec{
		// Remove 1, keep 2+3, add 4.
		key: {Target: "canary.supabase.co", Type: plan.MsmTypeDNS, Interval: 60, ProbeIDs: []uint32{2, 3, 4}},
	}
	state := sampleState(struct {
		name, cohort string
		rec          plan.MsmRecord
	}{"dns-canary", "high-freq", plan.MsmRecord{
		MsmID: 12345678, Target: "canary.supabase.co", Type: "dns",
		Interval: 60, ProbeIDs: []uint32{1, 2, 3},
	}})

	cs := plan.Diff(desired, state)

	add := findChange(t, cs, key, plan.ChangeAddProbes)
	assert.Equal(t, []uint32{4}, sortedU32(add.ProbeIDs))
	assert.Equal(t, uint64(12345678), add.MsmID)

	rem := findChange(t, cs, key, plan.ChangeRemoveProbes)
	assert.Equal(t, []uint32{1}, sortedU32(rem.ProbeIDs))
	assert.Equal(t, uint64(12345678), rem.MsmID)

	// Must not produce Stop or Create for a probe-only change.
	assert.Empty(t, findChanges(cs, key, plan.ChangeStop))
	assert.Empty(t, findChanges(cs, key, plan.ChangeCreate))
}

func TestDiff_ProbeSetAddsOnly(t *testing.T) {
	key := plan.MsmKey{Name: "ping-edge", Cohort: "low-freq"}
	desired := map[plan.MsmKey]plan.MsmSpec{
		key: {Target: "1.2.3.4", Type: plan.MsmTypePing, Interval: 900, ProbeIDs: []uint32{1, 2, 3, 4}},
	}
	state := sampleState(struct {
		name, cohort string
		rec          plan.MsmRecord
	}{"ping-edge", "low-freq", plan.MsmRecord{
		MsmID: 55555, Target: "1.2.3.4", Type: "ping", Interval: 900, ProbeIDs: []uint32{1, 2},
	}})

	cs := plan.Diff(desired, state)

	add := findChange(t, cs, key, plan.ChangeAddProbes)
	assert.ElementsMatch(t, []uint32{3, 4}, add.ProbeIDs)
	assert.Empty(t, findChanges(cs, key, plan.ChangeRemoveProbes), "no removals expected")
}

func TestDiff_StructuralChange(t *testing.T) {
	key := plan.MsmKey{Name: "dns-canary", Cohort: "high-freq"}

	// Interval changed: 60 → 120.
	desired := map[plan.MsmKey]plan.MsmSpec{
		key: {Target: "canary.supabase.co", Type: plan.MsmTypeDNS, Interval: 120, ProbeIDs: []uint32{1, 2}},
	}
	state := sampleState(struct {
		name, cohort string
		rec          plan.MsmRecord
	}{"dns-canary", "high-freq", plan.MsmRecord{
		MsmID: 12345678, Target: "canary.supabase.co", Type: "dns",
		Interval: 60, ProbeIDs: []uint32{1, 2},
	}})

	cs := plan.Diff(desired, state)

	stop := findChange(t, cs, key, plan.ChangeStop)
	assert.Equal(t, uint64(12345678), stop.MsmID, "should stop the old measurement")

	create := findChange(t, cs, key, plan.ChangeCreate)
	require.NotNil(t, create.Desired)
	assert.Equal(t, 120, create.Desired.Interval, "new measurement should use the new interval")

	// Must not produce probe-set changes for a structural change.
	assert.Empty(t, findChanges(cs, key, plan.ChangeAddProbes))
	assert.Empty(t, findChanges(cs, key, plan.ChangeRemoveProbes))
}

func TestDiff_StructuralChange_TargetChanged(t *testing.T) {
	key := plan.MsmKey{Name: "dns-canary", Cohort: "high-freq"}
	desired := map[plan.MsmKey]plan.MsmSpec{
		key: {Target: "new.supabase.co", Type: plan.MsmTypeDNS, Interval: 60, ProbeIDs: []uint32{1}},
	}
	state := sampleState(struct {
		name, cohort string
		rec          plan.MsmRecord
	}{"dns-canary", "high-freq", dnsRecord(1000, 1)})

	cs := plan.Diff(desired, state)

	findChange(t, cs, key, plan.ChangeStop)
	c := findChange(t, cs, key, plan.ChangeCreate)
	assert.Equal(t, "new.supabase.co", c.Desired.Target)
}

func TestDiff_NamespaceChange(t *testing.T) {
	// Namespace stored in state differs from desired — must trigger stop+create.
	key := plan.MsmKey{Name: "dns-canary", Cohort: "high-freq"}
	desired := map[plan.MsmKey]plan.MsmSpec{
		key: {
			Namespace: "new-ns",
			Target:    "canary.supabase.co",
			Type:      plan.MsmTypeDNS,
			Interval:  60,
			ProbeIDs:  []uint32{1, 2},
		},
	}
	state := sampleState(struct {
		name, cohort string
		rec          plan.MsmRecord
	}{"dns-canary", "high-freq", plan.MsmRecord{
		MsmID:     12345678,
		Namespace: "old-ns",
		Target:    "canary.supabase.co",
		Type:      "dns",
		Interval:  60,
		ProbeIDs:  []uint32{1, 2},
	}})

	cs := plan.Diff(desired, state)

	stop := findChange(t, cs, key, plan.ChangeStop)
	assert.Equal(t, uint64(12345678), stop.MsmID)

	create := findChange(t, cs, key, plan.ChangeCreate)
	require.NotNil(t, create.Desired)
	assert.Equal(t, "new-ns", create.Desired.Namespace)

	assert.Empty(t, findChanges(cs, key, plan.ChangeAddProbes))
	assert.Empty(t, findChanges(cs, key, plan.ChangeRemoveProbes))
}

func TestDiff_NamespaceMissing_NoStaticChange(t *testing.T) {
	// State has no stored namespace (old state file). Empty namespace is treated
	// as DefaultNamespace ("atlasctl"), so when the desired namespace is also
	// "atlasctl" there is no structural change.
	key := plan.MsmKey{Name: "dns-canary", Cohort: "high-freq"}
	desired := map[plan.MsmKey]plan.MsmSpec{
		key: {
			Namespace: "atlasctl",
			Target:    "canary.supabase.co",
			Type:      plan.MsmTypeDNS,
			Interval:  60,
			ProbeIDs:  []uint32{1, 2},
		},
	}
	state := sampleState(struct {
		name, cohort string
		rec          plan.MsmRecord
	}{"dns-canary", "high-freq", plan.MsmRecord{
		MsmID:  12345678,
		Target: "canary.supabase.co",
		Type:   "dns",
		// Namespace intentionally empty — old state.
		Interval: 60,
		ProbeIDs: []uint32{1, 2},
	}})

	cs := plan.Diff(desired, state)

	// No structural change — empty stored namespace == default == desired.
	findChange(t, cs, key, plan.ChangeNoOp)
	assert.Empty(t, findChanges(cs, key, plan.ChangeStop))
	assert.Empty(t, findChanges(cs, key, plan.ChangeCreate))
}

func TestDiff_NamespaceMissing_WithChange(t *testing.T) {
	// State has no stored namespace (old state file) but the user has changed
	// their configured namespace to a custom value. Empty stored namespace is
	// treated as DefaultNamespace; the mismatch must trigger stop+create so that
	// pkg callers who use Diff directly (not LiveDiff) see the change.
	key := plan.MsmKey{Name: "dns-canary", Cohort: "high-freq"}
	desired := map[plan.MsmKey]plan.MsmSpec{
		key: {
			Namespace: "my-ns",
			Target:    "canary.supabase.co",
			Type:      plan.MsmTypeDNS,
			Interval:  60,
			ProbeIDs:  []uint32{1, 2},
		},
	}
	state := sampleState(struct {
		name, cohort string
		rec          plan.MsmRecord
	}{"dns-canary", "high-freq", plan.MsmRecord{
		MsmID:  12345678,
		Target: "canary.supabase.co",
		Type:   "dns",
		// Namespace intentionally empty — old state, predates namespace tracking.
		Interval: 60,
		ProbeIDs: []uint32{1, 2},
	}})

	cs := plan.Diff(desired, state)

	stop := findChange(t, cs, key, plan.ChangeStop)
	assert.Equal(t, uint64(12345678), stop.MsmID)

	create := findChange(t, cs, key, plan.ChangeCreate)
	require.NotNil(t, create.Desired)
	assert.Equal(t, "my-ns", create.Desired.Namespace)

	assert.Empty(t, findChanges(cs, key, plan.ChangeNoOp))
}

func TestDiff_MultipleEntries(t *testing.T) {
	k1 := plan.MsmKey{Name: "dns-canary", Cohort: "high-freq"}
	k2 := plan.MsmKey{Name: "tls-canary", Cohort: "high-freq"}
	k3 := plan.MsmKey{Name: "ping-edge", Cohort: "low-freq"}

	desired := map[plan.MsmKey]plan.MsmSpec{
		k1: {Target: "canary.supabase.co", Type: plan.MsmTypeDNS, Interval: 60, ProbeIDs: []uint32{1}},
		k2: {Target: "canary.supabase.co", Type: plan.MsmTypeTLS, Interval: 60, ProbeIDs: []uint32{1}},
		// k3 is absent from desired → should produce Stop.
	}
	state := sampleState(
		struct {
			name, cohort string
			rec          plan.MsmRecord
		}{"tls-canary", "high-freq", plan.MsmRecord{
			MsmID: 2, Target: "canary.supabase.co",
			Type: "tls", Interval: 60, ProbeIDs: []uint32{1},
		}},
		struct {
			name, cohort string
			rec          plan.MsmRecord
		}{"ping-edge", "low-freq", plan.MsmRecord{MsmID: 3, Target: "1.2.3.4", Type: "ping", Interval: 900, ProbeIDs: []uint32{1}}},
	)

	cs := plan.Diff(desired, state)

	findChange(t, cs, k1, plan.ChangeCreate)
	findChange(t, cs, k2, plan.ChangeNoOp)
	findChange(t, cs, k3, plan.ChangeStop)
}

// ── desired state ──────────────────────────────────────────────────────────────

func TestDesiredState(t *testing.T) {
	cfg := config.Config{
		Measurements: []config.Measurement{
			{
				Name:   "dns-canary",
				Type:   config.TypeDNS,
				Target: "canary.supabase.co",
				AF:     4,
				Cohorts: []config.MeasurementCohort{
					{Name: "high-freq", ProbeCount: 2, MaxProbesPerCell: 1, IntervalSeconds: 60},
					{Name: "low-freq", ProbeCount: 3, MaxProbesPerCell: 2, IntervalSeconds: 900},
				},
			},
			{
				Name:   "ping-edge",
				Type:   config.TypePing,
				Target: "1.2.3.4",
				AF:     4,
				Cohorts: []config.MeasurementCohort{
					{Name: "low-freq", ProbeCount: 3, MaxProbesPerCell: 2, IntervalSeconds: 900},
				},
			},
		},
	}

	// Each measurement gets independent selection results.
	allSelected := map[string][]selection.SelectedCohort{
		"dns-canary": {
			{
				Cohort: config.MeasurementCohort{Name: "high-freq", IntervalSeconds: 60},
				Probes: []snapshot.Probe{{ID: 10}, {ID: 20}},
			},
			{
				Cohort: config.MeasurementCohort{Name: "low-freq", IntervalSeconds: 900},
				Probes: []snapshot.Probe{{ID: 30}, {ID: 40}, {ID: 50}},
			},
		},
		"ping-edge": {
			{
				Cohort: config.MeasurementCohort{Name: "low-freq", IntervalSeconds: 900},
				Probes: []snapshot.Probe{{ID: 30}, {ID: 40}, {ID: 50}},
			},
		},
	}

	desired := plan.DesiredState(cfg, allSelected)

	require.Len(t, desired, 3) // dns-canary/high-freq, dns-canary/low-freq, ping-edge/low-freq

	d := desired[plan.MsmKey{Name: "dns-canary", Cohort: "high-freq"}]
	assert.Equal(t, "atlasctl", d.Namespace)
	assert.Equal(t, "canary.supabase.co", d.Target)
	assert.Equal(t, plan.MsmTypeDNS, d.Type)
	assert.Equal(t, 60, d.Interval)
	assert.ElementsMatch(t, []uint32{10, 20}, d.ProbeIDs)
	// 2 probes × 10 credits × 3600/60  = 1200/hour; × 86400/60 = 28800/day
	assert.Equal(t, int64(1200), d.HourlyCredits)
	assert.Equal(t, int64(28800), d.DailyCredits)

	d = desired[plan.MsmKey{Name: "dns-canary", Cohort: "low-freq"}]
	assert.Equal(t, "atlasctl", d.Namespace)
	assert.Equal(t, 900, d.Interval)
	assert.ElementsMatch(t, []uint32{30, 40, 50}, d.ProbeIDs)
	// 3 probes × 10 credits × 3600/900 = 120/hour; × 86400/900 = 2880/day
	assert.Equal(t, int64(120), d.HourlyCredits)
	assert.Equal(t, int64(2880), d.DailyCredits)

	d = desired[plan.MsmKey{Name: "ping-edge", Cohort: "low-freq"}]
	assert.Equal(t, "atlasctl", d.Namespace)
	assert.Equal(t, "1.2.3.4", d.Target)
	assert.Equal(t, plan.MsmTypePing, d.Type)
	assert.Equal(t, 900, d.Interval)
	assert.ElementsMatch(t, []uint32{30, 40, 50}, d.ProbeIDs)
	// 3 probes × 3 credits × 3600/900 = 36/hour; × 86400/900 = 864/day
	assert.Equal(t, int64(36), d.HourlyCredits)
	assert.Equal(t, int64(864), d.DailyCredits)
}

func TestDesiredState_CustomNamespace(t *testing.T) {
	// When cfg.Namespace is set, DesiredState must embed it in every spec.
	cfg := config.Config{
		Namespace: "my-ns",
		Measurements: []config.Measurement{
			{
				Name:   "dns-canary",
				Type:   config.TypeDNS,
				Target: "canary.supabase.co",
				AF:     4,
				Cohorts: []config.MeasurementCohort{
					{Name: "high-freq", ProbeCount: 2, MaxProbesPerCell: 1, IntervalSeconds: 60},
				},
			},
		},
	}
	allSelected := map[string][]selection.SelectedCohort{
		"dns-canary": {
			{
				Cohort: config.MeasurementCohort{Name: "high-freq", IntervalSeconds: 60},
				Probes: []snapshot.Probe{{ID: 1}, {ID: 2}},
			},
		},
	}

	desired := plan.DesiredState(cfg, allSelected)

	d := desired[plan.MsmKey{Name: "dns-canary", Cohort: "high-freq"}]
	assert.Equal(t, "my-ns", d.Namespace)
}

// ── credit estimation ─────────────────────────────────────────────────────────

func TestEstimateCredits(t *testing.T) {
	// dns-canary/high-freq: 30 probes, 60s interval, DNS (10 credits/result)
	//   results/day = 30 × (86400/60) = 30 × 1440 = 43200
	//   credits/day = 43200 × 10 = 432000
	//
	// ping-edge/low-freq: 50 probes, 900s interval, Ping (3 credits/result)
	//   results/day = 50 × (86400/900) = 50 × 96 = 4800
	//   credits/day = 4800 × 3 = 14400
	//
	// total daily = 446400, weekly = 3124800
	desired := map[plan.MsmKey]plan.MsmSpec{
		{Name: "dns-canary", Cohort: "high-freq"}: {
			Target: "canary.supabase.co", Type: plan.MsmTypeDNS,
			Interval: 60, ProbeIDs: makeProbeIDs(30),
		},
		{Name: "ping-edge", Cohort: "low-freq"}: {
			Target: "1.2.3.4", Type: plan.MsmTypePing,
			Interval: 900, ProbeIDs: makeProbeIDs(50),
		},
	}

	est := plan.EstimateCredits(desired)

	assert.Equal(t, int64(446400), est.Daily)
	assert.Equal(t, int64(3124800), est.Weekly)
	assert.Len(t, est.Lines, 2)

	// Lines sorted descending by PerDay; dns-canary (432000) > ping-edge (14400).
	assert.Equal(t, "dns-canary", est.Lines[0].Key.Name)
	assert.Equal(t, int64(432000), est.Lines[0].PerDay)
	assert.Equal(t, "ping-edge", est.Lines[1].Key.Name)
	assert.Equal(t, int64(14400), est.Lines[1].PerDay)
}

func TestEstimateCredits_Empty(t *testing.T) {
	est := plan.EstimateCredits(map[plan.MsmKey]plan.MsmSpec{})
	assert.Equal(t, int64(0), est.Daily)
	assert.Equal(t, int64(0), est.Weekly)
	assert.Empty(t, est.Lines)
}

// makeProbeIDs returns a slice of n distinct probe IDs starting at 1.
func makeProbeIDs(n int) []uint32 {
	ids := make([]uint32, n)
	for i := range ids {
		ids[i] = uint32(i + 1)
	}
	return ids
}

// ── tag codec ─────────────────────────────────────────────────────────────────

func TestTagCodec(t *testing.T) {
	roundTrip := []struct {
		name   string
		cohort string
	}{
		{"dns-canary", "high-freq"},
		{"tls-canary", "mid-freq"},
		{"ping-edge", "low-freq"},
		{"m", "r"},
	}

	for _, tt := range roundTrip {
		tag := plan.FormatTag(tt.name, tt.cohort)
		gotName, gotCohort, ok := plan.ParseTag(tag)
		require.True(t, ok, "ParseTag(%q) should succeed", tag)
		assert.Equal(t, tt.name, gotName, "name mismatch for (%s, %s)", tt.name, tt.cohort)
		assert.Equal(t, tt.cohort, gotCohort, "cohort mismatch for (%s, %s)", tt.name, tt.cohort)
	}
}

func TestTagCodec_TagEmbeddedInDescription(t *testing.T) {
	tag := plan.FormatTag("dns-canary", "high-freq")
	desc := "Supabase external telemetry " + tag + " — do not delete"

	name, cohort, ok := plan.ParseTag(desc)
	require.True(t, ok)
	assert.Equal(t, "dns-canary", name)
	assert.Equal(t, "high-freq", cohort)
}

func TestTagCodec_Malformed(t *testing.T) {
	cases := []struct {
		desc  string
		input string
	}{
		{"no tag at all", "Supabase external telemetry"},
		{"prefix only, no closing bracket", "[atlasctl:dns-canary:high-freq"},
		{"empty name", "[atlasctl::high-freq]"},
		{"empty cohort", "[atlasctl:dns-canary:]"},
		{"no colon separator", "[atlasctl:dnscanaryhighfreq]"},
	}

	for _, tt := range cases {
		t.Run(tt.desc, func(t *testing.T) {
			_, _, ok := plan.ParseTag(tt.input)
			assert.False(t, ok, "ParseTag(%q) should return ok=false", tt.input)
		})
	}
}
