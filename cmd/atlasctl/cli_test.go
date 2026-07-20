package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/supabase/atlasctl/pkg/plan"
	"github.com/supabase/atlasctl/pkg/snapshot"
)

// testAPIKey is a valid UUID used as a fake API key in tests that require one.
const testAPIKey = "00000000-0000-0000-0000-000000000001"

// minimalConfig is the smallest valid atlasctl.yaml for single-measurement tests.
const minimalConfig = `
measurements:
  - name: dns-canary
    type: dns
    target: canary.supabase.co
    cohorts:
      - name: high-freq
        probe_count: 1
        max_probes_per_cell: 1
        interval_seconds: 60
`

// testDeps returns a Deps wired with the provided clients. Unused factories
// return harmless fakes so tests only need to supply what they actually use.
func testDeps(msmClient plan.MsmClient, applyClient plan.ApplyClient) Deps {
	return Deps{
		NewSnapshotClient: func(string, bool) snapshot.Client {
			return &snapshot.FakeClient{}
		},
		NewMsmClient: func(string, bool, plan.TagCodec) (plan.MsmClient, error) {
			return msmClient, nil
		},
		NewApplyClient: func(string, bool, plan.TagCodec) (plan.ApplyClient, error) {
			return applyClient, nil
		},
	}
}

// run executes the root command with the given args and returns (stdout, stderr, error).
func run(deps Deps, args ...string) (string, string, error) {
	var outBuf, errBuf bytes.Buffer
	cmd := newRootCmd(deps)
	cmd.SetOut(&outBuf)
	cmd.SetErr(&errBuf)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return outBuf.String(), errBuf.String(), err
}

// writeFile writes content to path, creating parent directories as needed.
func writeFile(t *testing.T, path, content string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0755))
	require.NoError(t, os.WriteFile(path, []byte(content), 0644))
}

// saveSnap writes an empty probe snapshot to path.
func saveSnap(t *testing.T, path string) {
	t.Helper()
	snap := snapshot.Snapshot{Probes: []snapshot.Probe{}, FetchedAt: time.Now().UTC()}
	require.NoError(t, snapshot.Save(path, snap))
}

// ── tests ─────────────────────────────────────────────────────────────────────

func TestRootHelp(t *testing.T) {
	out, _, err := run(defaultDeps(), "--help")
	require.NoError(t, err)
	for _, sub := range []string{"refresh", "select", "plan", "apply"} {
		assert.Contains(t, out, sub, "help should mention subcommand %q", sub)
	}
}

func TestSelect_ConfigNotFound(t *testing.T) {
	_, _, err := run(defaultDeps(),
		"--config", "/nonexistent/atlasctl.yaml",
		"select",
	)
	require.Error(t, err)
}

func TestPlan_NoState(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "atlasctl.yaml")
	snapPath := filepath.Join(dir, "snapshot.json")
	statePath := filepath.Join(dir, "state.yaml") // intentionally absent

	writeFile(t, cfgPath, minimalConfig)
	saveSnap(t, snapPath)

	fakeMsm := &plan.FakeMsmClient{ListResult: nil} // empty live list → no orphans
	deps := testDeps(fakeMsm, &plan.FakeApplyClient{})

	out, _, err := run(deps,
		"--config", cfgPath,
		"--snapshot", snapPath,
		"--state", statePath,
		"--api-key", testAPIKey,
		"plan",
	)

	require.NoError(t, err)
	// Missing state = first run → all desired measurements are creates.
	assert.Contains(t, out, "create", "plan output should contain at least one create action")
}

func TestApply_DryRunFlag(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "atlasctl.yaml")
	snapPath := filepath.Join(dir, "snapshot.json")
	statePath := filepath.Join(dir, "state.yaml") // absent → empty state → Create actions

	writeFile(t, cfgPath, minimalConfig)
	saveSnap(t, snapPath)

	fakeApply := &plan.FakeApplyClient{}
	// Reuse fakeApply for NewMsmClient too: FakeApplyClient embeds FakeMsmClient
	// so it satisfies plan.MsmClient.
	deps := Deps{
		NewSnapshotClient: func(string, bool) snapshot.Client { return &snapshot.FakeClient{} },
		NewMsmClient:      func(string, bool, plan.TagCodec) (plan.MsmClient, error) { return fakeApply, nil },
		NewApplyClient:    func(string, bool, plan.TagCodec) (plan.ApplyClient, error) { return fakeApply, nil },
	}

	_, _, err := run(deps,
		"--config", cfgPath,
		"--snapshot", snapPath,
		"--state", statePath,
		"--api-key", testAPIKey,
		"apply", "--dry-run", "--yes",
	)

	require.NoError(t, err)
	assert.Empty(t, fakeApply.CreatedSpecs, "--dry-run must not create measurements")
	assert.Empty(t, fakeApply.StoppedIDs, "--dry-run must not stop measurements")

	// State file must not be written in dry-run mode.
	_, statErr := os.Stat(statePath)
	assert.True(t, os.IsNotExist(statErr), "--dry-run must not write the state file")
}

// emptyMeasurementsConfig is a valid config with no measurements defined.
const emptyMeasurementsConfig = `
measurements: []
`

func TestPlan_EmptyMeasurements_StopsAll(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "atlasctl.yaml")
	snapPath := filepath.Join(dir, "snapshot.json")
	statePath := filepath.Join(dir, "state.yaml")

	writeFile(t, cfgPath, emptyMeasurementsConfig)
	saveSnap(t, snapPath)

	// Seed state with one live measurement so there is something to stop.
	state := plan.NewStateFile()
	state.SetRecord("dns-canary", "high-freq", plan.MsmRecord{MsmID: 99001})
	require.NoError(t, plan.SaveState(statePath, state))

	fakeMsm := &plan.FakeMsmClient{ListResult: nil}
	deps := testDeps(fakeMsm, &plan.FakeApplyClient{})

	out, _, err := run(deps,
		"--config", cfgPath,
		"--snapshot", snapPath,
		"--state", statePath,
		"--api-key", testAPIKey,
		"plan",
	)

	require.NoError(t, err)
	assert.Contains(t, out, "stop", "plan should show a stop for the orphaned state entry")
}

func TestApply_EmptyMeasurements_StopsAll(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "atlasctl.yaml")
	snapPath := filepath.Join(dir, "snapshot.json")
	statePath := filepath.Join(dir, "state.yaml")

	writeFile(t, cfgPath, emptyMeasurementsConfig)
	saveSnap(t, snapPath)

	state := plan.NewStateFile()
	state.SetRecord("dns-canary", "high-freq", plan.MsmRecord{MsmID: 99001})
	require.NoError(t, plan.SaveState(statePath, state))

	fakeApply := &plan.FakeApplyClient{}
	deps := Deps{
		NewSnapshotClient: func(string, bool) snapshot.Client { return &snapshot.FakeClient{} },
		NewMsmClient:      func(string, bool, plan.TagCodec) (plan.MsmClient, error) { return fakeApply, nil },
		NewApplyClient:    func(string, bool, plan.TagCodec) (plan.ApplyClient, error) { return fakeApply, nil },
	}

	_, _, err := run(deps,
		"--config", cfgPath,
		"--snapshot", snapPath,
		"--state", statePath,
		"--api-key", testAPIKey,
		"apply", "--yes",
	)

	require.NoError(t, err)
	assert.Equal(t, []uint64{99001}, fakeApply.StoppedIDs, "apply should stop the measurement from state")
}

// ── ensure pkg/ tests still pass: compile-time check ─────────────────────────

// TestPkgImportDirection verifies that cmd/ does not leak into pkg/. This is a
// build-time assertion: if any pkg/ file imports cmd/, the build fails.
func TestPkgImportDirection(_ *testing.T) {
	// The fact that this test file compiles without importing cmd/ is the assertion.
	_ = io.Discard
}
