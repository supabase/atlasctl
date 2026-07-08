package plan_test

import (
	"context"
	"errors"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/supabase/atlascli/pkg/plan"
)

// nopLog discards all log output in tests.
var nopLog = zerolog.Nop()

// ── helpers ───────────────────────────────────────────────────────────────────

// createChangeset builds a Changeset with a single ChangeCreate.
func createChangeset(name, round, target string, typ plan.MsmType, interval int, probeIDs []uint32) plan.Changeset {
	key := plan.MsmKey{Name: name, Round: round}
	desired := plan.DesiredMsm{Target: target, Type: typ, Interval: interval, ProbeIDs: probeIDs}
	return plan.Changeset{
		{Kind: plan.ChangeCreate, Key: key, Desired: &desired},
	}
}

// stopChangeset builds a Changeset with a single ChangeStop.
func stopChangeset(name, round string, msmID uint64) plan.Changeset {
	return plan.Changeset{
		{Kind: plan.ChangeStop, Key: plan.MsmKey{Name: name, Round: round}, MsmID: msmID},
	}
}

// ── tests ─────────────────────────────────────────────────────────────────────

func TestApply_Create(t *testing.T) {
	client := &plan.FakeApplyClient{}
	cs := createChangeset("dns-canary", "high-freq", "canary.supabase.co", plan.MsmTypeDNS, 60, []uint32{1, 2, 3})

	out, err := plan.Apply(context.Background(), cs, plan.NewStateFile(), client, plan.ApplyOptions{}, nopLog)

	require.NoError(t, err)
	require.Len(t, client.CreatedSpecs, 1)
	assert.Equal(t, "dns-canary", client.CreatedSpecs[0].Key.Name)
	assert.Equal(t, "high-freq", client.CreatedSpecs[0].Key.Round)

	rec, ok := out.GetRecord("dns-canary", "high-freq")
	require.True(t, ok, "record should exist in returned state")
	assert.Equal(t, uint64(10_000_001), rec.MsmID)
	assert.Equal(t, "canary.supabase.co", rec.Target)
	assert.Equal(t, []uint32{1, 2, 3}, rec.ProbeIDs)
}

func TestApply_Stop(t *testing.T) {
	state := plan.NewStateFile()
	state.SetRecord("dns-canary", "high-freq", plan.MsmRecord{
		MsmID: 99999, Target: "canary.supabase.co", Type: "dns", Interval: 60, ProbeIDs: []uint32{1},
	})
	client := &plan.FakeApplyClient{}
	cs := stopChangeset("dns-canary", "high-freq", 99999)

	out, err := plan.Apply(context.Background(), cs, state, client, plan.ApplyOptions{}, nopLog)

	require.NoError(t, err)
	require.Len(t, client.StoppedIDs, 1)
	assert.Equal(t, uint64(99999), client.StoppedIDs[0])

	_, ok := out.GetRecord("dns-canary", "high-freq")
	assert.False(t, ok, "record should be removed from returned state")
}

func TestApply_ProbeChange(t *testing.T) {
	const msmID = uint64(55555)
	state := plan.NewStateFile()
	state.SetRecord("dns-canary", "high-freq", plan.MsmRecord{
		MsmID: msmID, Target: "canary.supabase.co", Type: "dns", Interval: 60,
		ProbeIDs: []uint32{1, 2, 3, 4},
	})
	client := &plan.FakeApplyClient{}

	key := plan.MsmKey{Name: "dns-canary", Round: "high-freq"}
	cs := plan.Changeset{
		{Kind: plan.ChangeAddProbes, Key: key, MsmID: msmID, ProbeIDs: []uint32{5, 6}},
		{Kind: plan.ChangeRemoveProbes, Key: key, MsmID: msmID, ProbeIDs: []uint32{3, 4}},
	}

	out, err := plan.Apply(context.Background(), cs, state, client, plan.ApplyOptions{}, nopLog)

	require.NoError(t, err)

	require.Len(t, client.AddedCalls, 1)
	assert.Equal(t, msmID, client.AddedCalls[0].MsmID)
	assert.Equal(t, []uint32{5, 6}, client.AddedCalls[0].ProbeIDs)

	require.Len(t, client.RemovedCalls, 1)
	assert.Equal(t, msmID, client.RemovedCalls[0].MsmID)
	assert.Equal(t, []uint32{3, 4}, client.RemovedCalls[0].ProbeIDs)

	rec, ok := out.GetRecord("dns-canary", "high-freq")
	require.True(t, ok)
	// After add {5,6} and remove {3,4}: expect {1,2,5,6}.
	assert.ElementsMatch(t, []uint32{1, 2, 5, 6}, rec.ProbeIDs)
}

func TestApply_DryRun(t *testing.T) {
	state := plan.NewStateFile()
	state.SetRecord("dns-canary", "high-freq", plan.MsmRecord{
		MsmID: 99999, Target: "canary.supabase.co", Type: "dns", Interval: 60, ProbeIDs: []uint32{1},
	})
	client := &plan.FakeApplyClient{}

	key := plan.MsmKey{Name: "dns-canary", Round: "high-freq"}
	cs := plan.Changeset{
		{Kind: plan.ChangeCreate, Key: plan.MsmKey{Name: "new-msm", Round: "low"}, Desired: &plan.DesiredMsm{
			Target: "t.example.com", Type: plan.MsmTypePing, Interval: 120, ProbeIDs: []uint32{10},
		}},
		{Kind: plan.ChangeStop, Key: key, MsmID: 99999},
	}

	out, err := plan.Apply(context.Background(), cs, state, client, plan.ApplyOptions{DryRun: true}, nopLog)

	require.NoError(t, err)
	// No API calls made.
	assert.Empty(t, client.CreatedSpecs)
	assert.Empty(t, client.StoppedIDs)
	// State is unchanged (cloned from input).
	_, exists := out.GetRecord("new-msm", "low")
	assert.False(t, exists, "dry-run must not write new records")
	_, exists = out.GetRecord("dns-canary", "high-freq")
	assert.True(t, exists, "dry-run must not remove existing records")
}

func TestApply_ContextCancel(t *testing.T) {
	// Two creates; context is cancelled inside the first CreateMeasurement call
	// so the second change should never be attempted.
	ctx, cancel := context.WithCancel(context.Background())

	client := &cancelAfterFirstCreate{cancel: cancel}

	cs := plan.Changeset{
		{Kind: plan.ChangeCreate, Key: plan.MsmKey{Name: "msm-a", Round: "r1"}, Desired: &plan.DesiredMsm{
			Target: "a.example.com", Type: plan.MsmTypeDNS, Interval: 60, ProbeIDs: []uint32{1},
		}},
		{Kind: plan.ChangeCreate, Key: plan.MsmKey{Name: "msm-b", Round: "r1"}, Desired: &plan.DesiredMsm{
			Target: "b.example.com", Type: plan.MsmTypeDNS, Interval: 60, ProbeIDs: []uint32{2},
		}},
	}

	out, err := plan.Apply(ctx, cs, plan.NewStateFile(), client, plan.ApplyOptions{}, nopLog)

	assert.ErrorIs(t, err, context.Canceled)
	// Only msm-a was created before cancellation.
	_, aExists := out.GetRecord("msm-a", "r1")
	assert.True(t, aExists, "first operation completed before cancel")
	_, bExists := out.GetRecord("msm-b", "r1")
	assert.False(t, bExists, "second operation skipped due to cancel")
}

func TestApply_APIError(t *testing.T) {
	// Two creates; the first fails, the second succeeds.
	// Apply must continue past the failure.
	sentinel := errors.New("quota exceeded")
	errThenOk := &errorOnFirstCreate{err: sentinel}

	cs := plan.Changeset{
		{Kind: plan.ChangeCreate, Key: plan.MsmKey{Name: "msm-fail", Round: "r1"}, Desired: &plan.DesiredMsm{
			Target: "fail.example.com", Type: plan.MsmTypeDNS, Interval: 60, ProbeIDs: []uint32{1},
		}},
		{Kind: plan.ChangeCreate, Key: plan.MsmKey{Name: "msm-ok", Round: "r1"}, Desired: &plan.DesiredMsm{
			Target: "ok.example.com", Type: plan.MsmTypeDNS, Interval: 60, ProbeIDs: []uint32{2},
		}},
	}

	out, err := plan.Apply(context.Background(), cs, plan.NewStateFile(), errThenOk, plan.ApplyOptions{}, nopLog)

	require.Error(t, err)
	assert.ErrorContains(t, err, "quota exceeded")

	// Failed record is absent.
	_, failExists := out.GetRecord("msm-fail", "r1")
	assert.False(t, failExists)

	// Successful record is present.
	_, okExists := out.GetRecord("msm-ok", "r1")
	assert.True(t, okExists, "second create should have succeeded despite first failure")
}

// ── test-local client types ───────────────────────────────────────────────────

// cancelAfterFirstCreate cancels its context inside the first successful
// CreateMeasurement call so that Apply sees ctx.Err() before the second change.
type cancelAfterFirstCreate struct {
	plan.FakeApplyClient
	cancel context.CancelFunc
	calls  int
}

func (c *cancelAfterFirstCreate) CreateMeasurement(ctx context.Context, spec plan.MsmSpec) (uint64, error) {
	c.calls++
	id, err := c.FakeApplyClient.CreateMeasurement(ctx, spec)
	if c.calls == 1 {
		c.cancel()
	}
	return id, err
}

// errorOnFirstCreate fails the first call to CreateMeasurement and succeeds thereafter.
type errorOnFirstCreate struct {
	plan.FakeApplyClient
	err   error
	calls int
}

func (c *errorOnFirstCreate) CreateMeasurement(ctx context.Context, spec plan.MsmSpec) (uint64, error) {
	c.calls++
	if c.calls == 1 {
		return 0, c.err
	}
	return c.FakeApplyClient.CreateMeasurement(ctx, spec)
}
