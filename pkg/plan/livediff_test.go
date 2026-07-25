package plan_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/supabase/atlasctl/pkg/plan"
)

// ── helpers ───────────────────────────────────────────────────────────────────

// defaultCodec is a convenience for tests that don't care about namespace.
func defaultCodec() plan.TagCodec { return plan.NewTagCodec("") }

// taggedInfo returns an MsmInfo whose description embeds the atlasctl tag for
// (name, round) using the default namespace, simulating a measurement created
// by atlasctl with no custom namespace.
func taggedInfo(id uint64, name, round string) plan.MsmInfo {
	return taggedInfoNS(id, "", name, round)
}

// taggedInfoNS returns an MsmInfo whose description embeds the tag for
// (name, round) under the given namespace (empty = default "atlasctl").
func taggedInfoNS(id uint64, ns, name, round string) plan.MsmInfo {
	codec := plan.NewTagCodec(ns)
	return plan.MsmInfo{
		ID:          id,
		Description: "Supabase telemetry " + codec.Format(name, round),
		StatusID:    2, // Ongoing
		Type:        "dns",
		Target:      "canary.supabase.co",
		Interval:    60,
	}
}

// consistentSetup returns a desired map, state file, and FakeMsmClient that all
// agree with each other — the baseline "nothing to do" scenario.
func consistentSetup() (map[plan.MsmKey]plan.MsmSpec, plan.StateFile, *plan.FakeMsmClient) {
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

	state := plan.NewStateFile()
	state.SetRecord("dns-canary", "high-freq", plan.MsmRecord{
		MsmID:     12345678,
		Namespace: "atlasctl",
		Target:    "canary.supabase.co",
		Type:      "dns",
		Interval:  60,
		ProbeIDs:  []uint32{1, 2},
	})

	live := taggedInfo(12345678, "dns-canary", "high-freq")
	client := &plan.FakeMsmClient{
		Measurements: map[uint64]plan.MsmInfo{12345678: live},
		ListResult:   []plan.MsmInfo{live},
	}

	return desired, state, client
}

// ── existing tests (codec arg added) ─────────────────────────────────────────

func TestLiveDiff_Consistent(t *testing.T) {
	desired, state, client := consistentSetup()

	cs, warnings, err := plan.LiveDiff(context.Background(), desired, state, client, defaultCodec())

	require.NoError(t, err)
	assert.Empty(t, warnings, "no drift expected when state and API agree")
	// Static diff: desired == state → one NoOp.
	require.Len(t, cs, 1)
	assert.Equal(t, plan.ChangeNoOp, cs[0].Kind)
}

func TestLiveDiff_Orphan(t *testing.T) {
	// State is empty, but the API has a measurement with our tag.
	orphanKey := plan.MsmKey{Name: "old-measurement", Cohort: "mid-freq"}
	live := taggedInfo(99999, "old-measurement", "mid-freq")

	client := &plan.FakeMsmClient{
		ListResult: []plan.MsmInfo{live},
	}

	cs, warnings, err := plan.LiveDiff(
		context.Background(),
		map[plan.MsmKey]plan.MsmSpec{},
		plan.NewStateFile(),
		client,
		defaultCodec(),
	)

	require.NoError(t, err)
	require.Len(t, cs, 0, "empty desired + empty state → no changes")

	require.Len(t, warnings, 1)
	w := warnings[0]
	assert.Equal(t, plan.DriftOrphan, w.Kind)
	assert.Equal(t, orphanKey, w.Key)
	assert.Equal(t, uint64(99999), w.MsmID)
}

func TestLiveDiff_OrphanMalformedTag(t *testing.T) {
	// Live measurement has our prefix but a malformed tag body.
	malformed := plan.MsmInfo{
		ID:          77777,
		Description: "[atlasctl:no-colon-here]",
		StatusID:    2,
	}
	client := &plan.FakeMsmClient{ListResult: []plan.MsmInfo{malformed}}

	_, warnings, err := plan.LiveDiff(
		context.Background(),
		map[plan.MsmKey]plan.MsmSpec{},
		plan.NewStateFile(),
		client,
		defaultCodec(),
	)

	require.NoError(t, err)
	require.Len(t, warnings, 1)
	assert.Equal(t, plan.DriftOrphan, warnings[0].Kind)
	assert.Equal(t, uint64(77777), warnings[0].MsmID)
	// Key is zero value — we couldn't parse it.
	assert.Equal(t, plan.MsmKey{}, warnings[0].Key)
}

func TestLiveDiff_Ghost(t *testing.T) {
	// State references an MsmID that is absent from the live API list.
	// GetMeasurement returns ErrMsmNotFound → real ghost.
	key := plan.MsmKey{Name: "dns-canary", Cohort: "high-freq"}

	desired := map[plan.MsmKey]plan.MsmSpec{
		key: {Target: "canary.supabase.co", Type: plan.MsmTypeDNS, Interval: 60, ProbeIDs: []uint32{1}},
	}
	state := plan.NewStateFile()
	state.SetRecord("dns-canary", "high-freq", plan.MsmRecord{
		MsmID: 12345678, Target: "canary.supabase.co", Type: "dns",
		Interval: 60, ProbeIDs: []uint32{1},
	})

	// List returns nothing and GetMeasurement finds no such ID → true ghost.
	client := &plan.FakeMsmClient{ListResult: nil}

	cs, warnings, err := plan.LiveDiff(context.Background(), desired, state, client, defaultCodec())

	require.NoError(t, err)
	require.Len(t, warnings, 1)
	w := warnings[0]
	assert.Equal(t, plan.DriftGhost, w.Kind)
	assert.Equal(t, key, w.Key)
	assert.Equal(t, uint64(12345678), w.MsmID)

	// The changeset still reflects desired-vs-state (probe sets match → NoOp).
	require.Len(t, cs, 1)
	assert.Equal(t, plan.ChangeNoOp, cs[0].Kind)
}

func TestLiveDiff_OrphanAndGhostTogether(t *testing.T) {
	// State references ID 111 (ghost: not in live list, not found by GetMeasurement).
	// Live has ID 222 with our tag (orphan: not in state).
	state := plan.NewStateFile()
	state.SetRecord("dns-canary", "high-freq", plan.MsmRecord{
		MsmID: 111, Target: "canary.supabase.co", Type: "dns",
		Interval: 60, ProbeIDs: []uint32{1},
	})

	orphan := taggedInfo(222, "tls-canary", "mid-freq")
	client := &plan.FakeMsmClient{ListResult: []plan.MsmInfo{orphan}}

	desired := map[plan.MsmKey]plan.MsmSpec{
		{Name: "dns-canary", Cohort: "high-freq"}: {
			Target: "canary.supabase.co", Type: plan.MsmTypeDNS, Interval: 60, ProbeIDs: []uint32{1},
		},
	}

	_, warnings, err := plan.LiveDiff(context.Background(), desired, state, client, defaultCodec())

	require.NoError(t, err)
	require.Len(t, warnings, 2)

	kinds := map[plan.DriftKind]bool{}
	for _, w := range warnings {
		kinds[w.Kind] = true
	}
	assert.True(t, kinds[plan.DriftOrphan], "should have an orphan warning")
	assert.True(t, kinds[plan.DriftGhost], "should have a ghost warning")
}

func TestLiveDiff_ListError(t *testing.T) {
	sentinel := errors.New("API timeout")
	client := &plan.FakeMsmClient{ListErr: sentinel}

	_, _, err := plan.LiveDiff(
		context.Background(),
		map[plan.MsmKey]plan.MsmSpec{},
		plan.NewStateFile(),
		client,
		defaultCodec(),
	)

	require.Error(t, err)
	assert.ErrorContains(t, err, "listing live measurements")
}

func TestLiveDiff_ContextCancel(t *testing.T) {
	_, state, client := consistentSetup()
	desired := map[plan.MsmKey]plan.MsmSpec{}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already cancelled before the call

	_, _, err := plan.LiveDiff(ctx, desired, state, client, defaultCodec())

	assert.ErrorIs(t, err, context.Canceled)
}

func TestLiveDiff_ContextCancelDuringList(t *testing.T) {
	// Simulate a client that respects context cancellation by returning the
	// ctx error when the context is already done.
	ctx, cancel := context.WithCancel(context.Background())

	blockingClient := &blockOnListClient{cancel: cancel}

	_, _, err := plan.LiveDiff(ctx, map[plan.MsmKey]plan.MsmSpec{}, plan.NewStateFile(), blockingClient, defaultCodec())

	assert.ErrorIs(t, err, context.Canceled)
}

// blockOnListClient cancels its own context when ListMyMeasurements is called,
// then returns the context error — simulating a cancellation mid-list.
type blockOnListClient struct {
	cancel context.CancelFunc
}

func (b *blockOnListClient) GetMeasurement(_ context.Context, _ uint64) (plan.MsmInfo, error) {
	return plan.MsmInfo{}, nil
}

func (b *blockOnListClient) ListMyMeasurements(ctx context.Context) ([]plan.MsmInfo, error) {
	b.cancel()
	return nil, ctx.Err()
}

// ── namespace-change tests ────────────────────────────────────────────────────

// TestLiveDiff_NamespaceMismatch_OldState covers the migration case: state was
// written before namespace tracking, so rec.Namespace is empty. isStructuralChange
// skips the check; LiveDiff calls GetMeasurement and detects the mismatch.
func TestLiveDiff_NamespaceMismatch_OldState(t *testing.T) {
	key := plan.MsmKey{Name: "dns-canary", Cohort: "high-freq"}

	// Desired uses the new namespace.
	newCodec := plan.NewTagCodec("new-ns")
	desired := map[plan.MsmKey]plan.MsmSpec{
		key: {
			Namespace: newCodec.Namespace(),
			Target:    "canary.supabase.co",
			Type:      plan.MsmTypeDNS,
			Interval:  60,
			ProbeIDs:  []uint32{1, 2},
		},
	}

	// State has no namespace stored (old state file).
	state := plan.NewStateFile()
	state.SetRecord("dns-canary", "high-freq", plan.MsmRecord{
		MsmID:  12345678,
		Target: "canary.supabase.co",
		Type:   "dns",
		// Namespace intentionally empty — migration scenario.
		Interval: 60,
		ProbeIDs: []uint32{1, 2},
	})

	// ListMyMeasurements (filtered by "new-ns") returns nothing — old measurement
	// is tagged "atlasctl", not "new-ns".
	// GetMeasurement returns the live measurement with the old-namespace description.
	oldNsInfo := taggedInfoNS(12345678, "atlasctl", "dns-canary", "high-freq")
	client := &plan.FakeMsmClient{
		Measurements: map[uint64]plan.MsmInfo{12345678: oldNsInfo},
		ListResult:   nil,
	}

	cs, warnings, err := plan.LiveDiff(context.Background(), desired, state, client, newCodec)

	require.NoError(t, err)
	assert.Empty(t, warnings, "namespace mismatch must not produce a ghost warning")

	// Changeset must contain a Stop (old measurement) and a Create (new namespace).
	stops := findChanges(cs, key, plan.ChangeStop)
	creates := findChanges(cs, key, plan.ChangeCreate)
	require.Len(t, stops, 1, "expected one Stop for the old-namespace measurement")
	require.Len(t, creates, 1, "expected one Create for the new-namespace measurement")
	assert.Equal(t, uint64(12345678), stops[0].MsmID)
	assert.Equal(t, "new-ns", creates[0].Desired.Namespace)
}

// TestLiveDiff_NamespaceMismatch_Stored covers the post-migration case: namespace
// is stored in state and has changed. isStructuralChange already schedules a
// stop+create; LiveDiff must suppress the ghost warning and not emit a duplicate stop.
func TestLiveDiff_NamespaceMismatch_Stored(t *testing.T) {
	key := plan.MsmKey{Name: "dns-canary", Cohort: "high-freq"}

	newCodec := plan.NewTagCodec("new-ns")
	desired := map[plan.MsmKey]plan.MsmSpec{
		key: {
			Namespace: newCodec.Namespace(),
			Target:    "canary.supabase.co",
			Type:      plan.MsmTypeDNS,
			Interval:  60,
			ProbeIDs:  []uint32{1, 2},
		},
	}

	// State has the OLD namespace stored — isStructuralChange detects the change.
	state := plan.NewStateFile()
	state.SetRecord("dns-canary", "high-freq", plan.MsmRecord{
		MsmID:     12345678,
		Namespace: "old-ns",
		Target:    "canary.supabase.co",
		Type:      "dns",
		Interval:  60,
		ProbeIDs:  []uint32{1, 2},
	})

	// ListMyMeasurements (filtered by "new-ns") returns nothing.
	// Measurements map is not populated — GetMeasurement must NOT be called
	// because alreadyStopping suppresses it.
	client := &plan.FakeMsmClient{ListResult: nil}

	cs, warnings, err := plan.LiveDiff(context.Background(), desired, state, client, newCodec)

	require.NoError(t, err)
	assert.Empty(t, warnings, "ghost warning must be suppressed when stop is already scheduled")

	// Static diff already produced exactly one Stop and one Create — no duplicates.
	stops := findChanges(cs, key, plan.ChangeStop)
	creates := findChanges(cs, key, plan.ChangeCreate)
	assert.Len(t, stops, 1, "exactly one Stop — no duplicate from ghost path")
	assert.Len(t, creates, 1, "exactly one Create")
}

// TestLiveDiff_TrueGhost_NotNamespaceMismatch confirms that a measurement which
// is genuinely gone (GetMeasurement returns ErrMsmNotFound) is reported as a
// ghost and not misclassified as a namespace mismatch.
func TestLiveDiff_TrueGhost_NotNamespaceMismatch(t *testing.T) {
	key := plan.MsmKey{Name: "dns-canary", Cohort: "high-freq"}

	desired := map[plan.MsmKey]plan.MsmSpec{
		key: {
			Namespace: "atlasctl",
			Target:    "canary.supabase.co",
			Type:      plan.MsmTypeDNS,
			Interval:  60,
			ProbeIDs:  []uint32{1},
		},
	}
	state := plan.NewStateFile()
	state.SetRecord("dns-canary", "high-freq", plan.MsmRecord{
		MsmID:     12345678,
		Namespace: "atlasctl",
		Target:    "canary.supabase.co",
		Type:      "dns",
		Interval:  60,
		ProbeIDs:  []uint32{1},
	})

	// ListResult empty and GetMeasurement returns ErrMsmNotFound.
	client := &plan.FakeMsmClient{ListResult: nil}

	cs, warnings, err := plan.LiveDiff(context.Background(), desired, state, client, defaultCodec())

	require.NoError(t, err)
	require.Len(t, warnings, 1)
	assert.Equal(t, plan.DriftGhost, warnings[0].Kind)
	assert.Equal(t, key, warnings[0].Key)

	// Changeset is NoOp — ghost does not inject stop+create.
	require.Len(t, cs, 1)
	assert.Equal(t, plan.ChangeNoOp, cs[0].Kind)
}

// TestLiveDiff_GetMeasurementError confirms that unexpected GetMeasurement
// errors (not ErrMsmNotFound) are propagated as a fatal error from LiveDiff.
func TestLiveDiff_GetMeasurementError(t *testing.T) {
	key := plan.MsmKey{Name: "dns-canary", Cohort: "high-freq"}

	desired := map[plan.MsmKey]plan.MsmSpec{
		key: {Target: "canary.supabase.co", Type: plan.MsmTypeDNS, Interval: 60, ProbeIDs: []uint32{1}},
	}
	state := plan.NewStateFile()
	state.SetRecord("dns-canary", "high-freq", plan.MsmRecord{
		MsmID: 12345678, Target: "canary.supabase.co", Type: "dns",
		Interval: 60, ProbeIDs: []uint32{1},
	})

	apiErr := errors.New("internal server error")
	client := &plan.FakeMsmClient{
		ListResult: nil,
		GetErr:     apiErr,
	}

	_, _, err := plan.LiveDiff(context.Background(), desired, state, client, defaultCodec())

	require.Error(t, err)
	assert.ErrorContains(t, err, "checking measurement 12345678 for namespace drift")
}
