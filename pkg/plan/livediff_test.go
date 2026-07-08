package plan_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/supabase/atlascli/pkg/plan"
)

// ── helpers ───────────────────────────────────────────────────────────────────

// taggedInfo returns an MsmInfo whose description embeds the atlasctl tag for
// (name, round), simulating a measurement created by atlasctl.
func taggedInfo(id uint64, name, round string) plan.MsmInfo {
	return plan.MsmInfo{
		ID:          id,
		Description: "Supabase telemetry " + plan.FormatTag(name, round),
		StatusID:    2, // Ongoing
		Type:        "dns",
		Target:      "canary.supabase.co",
		Interval:    60,
	}
}

// consistentSetup returns a desired map, state file, and FakeMsmClient that all
// agree with each other — the baseline "nothing to do" scenario.
func consistentSetup() (map[plan.MsmKey]plan.DesiredMsm, plan.StateFile, *plan.FakeMsmClient) {
	key := plan.MsmKey{Name: "dns-canary", Round: "high-freq"}

	desired := map[plan.MsmKey]plan.DesiredMsm{
		key: {Target: "canary.supabase.co", Type: plan.MsmTypeDNS, Interval: 60, ProbeIDs: []uint32{1, 2}},
	}

	state := plan.NewStateFile()
	state.SetRecord("dns-canary", "high-freq", plan.MsmRecord{
		MsmID: 12345678, Target: "canary.supabase.co", Type: "dns",
		Interval: 60, ProbeIDs: []uint32{1, 2},
	})

	live := taggedInfo(12345678, "dns-canary", "high-freq")
	client := &plan.FakeMsmClient{
		Measurements: map[uint64]plan.MsmInfo{12345678: live},
		ListResult:   []plan.MsmInfo{live},
	}

	return desired, state, client
}

// ── tests ─────────────────────────────────────────────────────────────────────

func TestLiveDiff_Consistent(t *testing.T) {
	desired, state, client := consistentSetup()

	cs, warnings, err := plan.LiveDiff(context.Background(), desired, state, client)

	require.NoError(t, err)
	assert.Empty(t, warnings, "no drift expected when state and API agree")
	// Static diff: desired == state → one NoOp.
	require.Len(t, cs, 1)
	assert.Equal(t, plan.ChangeNoOp, cs[0].Kind)
}

func TestLiveDiff_Orphan(t *testing.T) {
	// State is empty, but the API has a measurement with our tag.
	orphanKey := plan.MsmKey{Name: "old-measurement", Round: "mid-freq"}
	live := taggedInfo(99999, "old-measurement", "mid-freq")

	client := &plan.FakeMsmClient{
		ListResult: []plan.MsmInfo{live},
	}

	cs, warnings, err := plan.LiveDiff(
		context.Background(),
		map[plan.MsmKey]plan.DesiredMsm{},
		plan.NewStateFile(),
		client,
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
		map[plan.MsmKey]plan.DesiredMsm{},
		plan.NewStateFile(),
		client,
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
	key := plan.MsmKey{Name: "dns-canary", Round: "high-freq"}

	desired := map[plan.MsmKey]plan.DesiredMsm{
		key: {Target: "canary.supabase.co", Type: plan.MsmTypeDNS, Interval: 60, ProbeIDs: []uint32{1}},
	}
	state := plan.NewStateFile()
	state.SetRecord("dns-canary", "high-freq", plan.MsmRecord{
		MsmID: 12345678, Target: "canary.supabase.co", Type: "dns",
		Interval: 60, ProbeIDs: []uint32{1},
	})

	// API returns an empty list — measurement is gone.
	client := &plan.FakeMsmClient{ListResult: nil}

	cs, warnings, err := plan.LiveDiff(context.Background(), desired, state, client)

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
	// State references ID 111 (ghost: not in live list).
	// Live has ID 222 with our tag (orphan: not in state).
	state := plan.NewStateFile()
	state.SetRecord("dns-canary", "high-freq", plan.MsmRecord{
		MsmID: 111, Target: "canary.supabase.co", Type: "dns",
		Interval: 60, ProbeIDs: []uint32{1},
	})

	orphan := taggedInfo(222, "tls-canary", "mid-freq")
	client := &plan.FakeMsmClient{ListResult: []plan.MsmInfo{orphan}}

	desired := map[plan.MsmKey]plan.DesiredMsm{
		{Name: "dns-canary", Round: "high-freq"}: {
			Target: "canary.supabase.co", Type: plan.MsmTypeDNS, Interval: 60, ProbeIDs: []uint32{1},
		},
	}

	_, warnings, err := plan.LiveDiff(context.Background(), desired, state, client)

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
		map[plan.MsmKey]plan.DesiredMsm{},
		plan.NewStateFile(),
		client,
	)

	require.Error(t, err)
	assert.ErrorContains(t, err, "listing live measurements")
}

func TestLiveDiff_ContextCancel(t *testing.T) {
	_, state, client := consistentSetup()
	desired := map[plan.MsmKey]plan.DesiredMsm{}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already cancelled before the call

	_, _, err := plan.LiveDiff(ctx, desired, state, client)

	assert.ErrorIs(t, err, context.Canceled)
}

func TestLiveDiff_ContextCancelDuringList(t *testing.T) {
	// Simulate a client that respects context cancellation by returning the
	// ctx error when the context is already done.
	ctx, cancel := context.WithCancel(context.Background())

	blockingClient := &blockOnListClient{cancel: cancel}

	_, _, err := plan.LiveDiff(ctx, map[plan.MsmKey]plan.DesiredMsm{}, plan.NewStateFile(), blockingClient)

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
