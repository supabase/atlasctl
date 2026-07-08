package plan_test

import (
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/supabase/atlascli/pkg/config"
	"github.com/supabase/atlascli/pkg/plan"
	"github.com/supabase/atlascli/pkg/selection"
	"github.com/supabase/atlascli/pkg/snapshot"
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
	name, round string
	rec         plan.MsmRecord
}) plan.StateFile {
	sf := plan.NewStateFile()
	for _, e := range entries {
		sf.SetRecord(e.name, e.round, e.rec)
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
		for _, round := range []string{"high-freq", "mid-freq"} {
			orig, origOK := original.GetRecord(name, round)
			got, gotOK := loaded.GetRecord(name, round)
			require.Equal(t, origOK, gotOK, "GetRecord(%s, %s) existence", name, round)
			if origOK {
				assert.Equal(t, orig.MsmID, got.MsmID, "MsmID for (%s, %s)", name, round)
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

	// Deleting the last round should remove the outer map entry too.
	assert.Empty(t, sf.Measurements, "outer map should be empty after last round removed")
}

// ── diff ──────────────────────────────────────────────────────────────────────

func TestDiff_Create(t *testing.T) {
	key := plan.MsmKey{Name: "dns-canary", Round: "high-freq"}
	desired := map[plan.MsmKey]plan.DesiredMsm{
		key: {Target: "canary.supabase.co", Type: "dns", Interval: 60, ProbeIDs: []uint32{1, 2, 3}},
	}
	cs := plan.Diff(desired, plan.NewStateFile())

	c := findChange(t, cs, key, plan.ChangeCreate)
	require.NotNil(t, c.Desired)
	assert.Equal(t, "canary.supabase.co", c.Desired.Target)
	assert.Equal(t, []uint32{1, 2, 3}, c.Desired.ProbeIDs)
}

func TestDiff_NoOp(t *testing.T) {
	key := plan.MsmKey{Name: "dns-canary", Round: "high-freq"}
	probes := []uint32{1001, 2002, 3003}

	desired := map[plan.MsmKey]plan.DesiredMsm{
		key: {Target: "canary.supabase.co", Type: "dns", Interval: 60, ProbeIDs: probes},
	}
	state := sampleState(struct {
		name, round string
		rec         plan.MsmRecord
	}{"dns-canary", "high-freq", plan.MsmRecord{
		MsmID: 12345678, Target: "canary.supabase.co", Type: "dns",
		Interval: 60, ProbeIDs: probes,
	}})

	cs := plan.Diff(desired, state)

	c := findChange(t, cs, key, plan.ChangeNoOp)
	assert.Equal(t, uint64(12345678), c.MsmID)
}

func TestDiff_Stop(t *testing.T) {
	key := plan.MsmKey{Name: "dns-canary", Round: "high-freq"}
	state := sampleState(struct {
		name, round string
		rec         plan.MsmRecord
	}{"dns-canary", "high-freq", dnsRecord(99999999, 1, 2, 3)})

	// Empty desired — everything in state should be stopped.
	cs := plan.Diff(map[plan.MsmKey]plan.DesiredMsm{}, state)

	c := findChange(t, cs, key, plan.ChangeStop)
	assert.Equal(t, uint64(99999999), c.MsmID)
}

func TestDiff_ProbeSetChanged(t *testing.T) {
	key := plan.MsmKey{Name: "dns-canary", Round: "high-freq"}

	desired := map[plan.MsmKey]plan.DesiredMsm{
		// Remove 1, keep 2+3, add 4.
		key: {Target: "canary.supabase.co", Type: "dns", Interval: 60, ProbeIDs: []uint32{2, 3, 4}},
	}
	state := sampleState(struct {
		name, round string
		rec         plan.MsmRecord
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
	key := plan.MsmKey{Name: "ping-edge", Round: "low-freq"}
	desired := map[plan.MsmKey]plan.DesiredMsm{
		key: {Target: "1.2.3.4", Type: "ping", Interval: 900, ProbeIDs: []uint32{1, 2, 3, 4}},
	}
	state := sampleState(struct {
		name, round string
		rec         plan.MsmRecord
	}{"ping-edge", "low-freq", plan.MsmRecord{
		MsmID: 55555, Target: "1.2.3.4", Type: "ping", Interval: 900, ProbeIDs: []uint32{1, 2},
	}})

	cs := plan.Diff(desired, state)

	add := findChange(t, cs, key, plan.ChangeAddProbes)
	assert.ElementsMatch(t, []uint32{3, 4}, add.ProbeIDs)
	assert.Empty(t, findChanges(cs, key, plan.ChangeRemoveProbes), "no removals expected")
}

func TestDiff_StructuralChange(t *testing.T) {
	key := plan.MsmKey{Name: "dns-canary", Round: "high-freq"}

	// Interval changed: 60 → 120.
	desired := map[plan.MsmKey]plan.DesiredMsm{
		key: {Target: "canary.supabase.co", Type: "dns", Interval: 120, ProbeIDs: []uint32{1, 2}},
	}
	state := sampleState(struct {
		name, round string
		rec         plan.MsmRecord
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
	key := plan.MsmKey{Name: "dns-canary", Round: "high-freq"}
	desired := map[plan.MsmKey]plan.DesiredMsm{
		key: {Target: "new.supabase.co", Type: "dns", Interval: 60, ProbeIDs: []uint32{1}},
	}
	state := sampleState(struct {
		name, round string
		rec         plan.MsmRecord
	}{"dns-canary", "high-freq", dnsRecord(1000, 1)})

	cs := plan.Diff(desired, state)

	findChange(t, cs, key, plan.ChangeStop)
	c := findChange(t, cs, key, plan.ChangeCreate)
	assert.Equal(t, "new.supabase.co", c.Desired.Target)
}

func TestDiff_MultipleEntries(t *testing.T) {
	k1 := plan.MsmKey{Name: "dns-canary", Round: "high-freq"}
	k2 := plan.MsmKey{Name: "tls-canary", Round: "high-freq"}
	k3 := plan.MsmKey{Name: "ping-edge", Round: "low-freq"}

	desired := map[plan.MsmKey]plan.DesiredMsm{
		k1: {Target: "canary.supabase.co", Type: "dns", Interval: 60, ProbeIDs: []uint32{1}},
		k2: {Target: "canary.supabase.co", Type: "tls", Interval: 60, ProbeIDs: []uint32{1}},
		// k3 is absent from desired → should produce Stop.
	}
	state := sampleState(
		struct {
			name, round string
			rec         plan.MsmRecord
		}{"tls-canary", "high-freq", plan.MsmRecord{MsmID: 2, Target: "canary.supabase.co", Type: "tls", Interval: 60, ProbeIDs: []uint32{1}}},
		struct {
			name, round string
			rec         plan.MsmRecord
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
		Rounds: []config.Round{
			{Name: "high-freq", Count: 2, IntervalSeconds: 60, MaxProbesPerCell: 1},
			{Name: "low-freq", Count: 3, IntervalSeconds: 900, MaxProbesPerCell: 2},
		},
		Measurements: []config.Measurement{
			{Name: "dns-canary", Type: "dns", Target: "canary.supabase.co", Rounds: []string{"high-freq", "low-freq"}},
			{Name: "ping-edge", Type: "ping", Target: "1.2.3.4", Rounds: []string{"low-freq"}},
		},
	}

	rounds := []selection.SelectedRound{
		{
			Round:  config.Round{Name: "high-freq"},
			Probes: []snapshot.Probe{{ID: 10}, {ID: 20}},
		},
		{
			Round:  config.Round{Name: "low-freq"},
			Probes: []snapshot.Probe{{ID: 30}, {ID: 40}, {ID: 50}},
		},
	}

	desired := plan.DesiredState(cfg, rounds)

	require.Len(t, desired, 3) // dns-canary/high-freq, dns-canary/low-freq, ping-edge/low-freq

	d := desired[plan.MsmKey{Name: "dns-canary", Round: "high-freq"}]
	assert.Equal(t, "canary.supabase.co", d.Target)
	assert.Equal(t, "dns", d.Type)
	assert.Equal(t, 60, d.Interval)
	assert.ElementsMatch(t, []uint32{10, 20}, d.ProbeIDs)

	d = desired[plan.MsmKey{Name: "dns-canary", Round: "low-freq"}]
	assert.Equal(t, 900, d.Interval)
	assert.ElementsMatch(t, []uint32{30, 40, 50}, d.ProbeIDs)

	d = desired[plan.MsmKey{Name: "ping-edge", Round: "low-freq"}]
	assert.Equal(t, "1.2.3.4", d.Target)
	assert.Equal(t, "ping", d.Type)
	assert.ElementsMatch(t, []uint32{30, 40, 50}, d.ProbeIDs)
}

// ── tag codec ─────────────────────────────────────────────────────────────────

func TestTagCodec(t *testing.T) {
	roundTrip := []struct {
		name  string
		round string
	}{
		{"dns-canary", "high-freq"},
		{"tls-canary", "mid-freq"},
		{"ping-edge", "low-freq"},
		{"m", "r"},
	}

	for _, tt := range roundTrip {
		tag := plan.FormatTag(tt.name, tt.round)
		gotName, gotRound, ok := plan.ParseTag(tag)
		require.True(t, ok, "ParseTag(%q) should succeed", tag)
		assert.Equal(t, tt.name, gotName, "name mismatch for (%s, %s)", tt.name, tt.round)
		assert.Equal(t, tt.round, gotRound, "round mismatch for (%s, %s)", tt.name, tt.round)
	}
}

func TestTagCodec_TagEmbeddedInDescription(t *testing.T) {
	tag := plan.FormatTag("dns-canary", "high-freq")
	desc := "Supabase external telemetry " + tag + " — do not delete"

	name, round, ok := plan.ParseTag(desc)
	require.True(t, ok)
	assert.Equal(t, "dns-canary", name)
	assert.Equal(t, "high-freq", round)
}

func TestTagCodec_Malformed(t *testing.T) {
	cases := []struct {
		desc  string
		input string
	}{
		{"no tag at all", "Supabase external telemetry"},
		{"prefix only, no closing bracket", "[atlasctl:dns-canary:high-freq"},
		{"empty name", "[atlasctl::high-freq]"},
		{"empty round", "[atlasctl:dns-canary:]"},
		{"no colon separator", "[atlasctl:dnscanaryhighfreq]"},
	}

	for _, tt := range cases {
		t.Run(tt.desc, func(t *testing.T) {
			_, _, ok := plan.ParseTag(tt.input)
			assert.False(t, ok, "ParseTag(%q) should return ok=false", tt.input)
		})
	}
}
