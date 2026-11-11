package plan_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/supabase/atlasctl/pkg/config"
	"github.com/supabase/atlasctl/pkg/plan"
	"github.com/supabase/atlasctl/pkg/selection"
	"github.com/supabase/atlasctl/pkg/snapshot"
)

func pingOpts(packets uint) config.MsmOptions {
	return config.MsmOptions{Ping: config.PingOptions{Packets: packets}}
}

func pingState(opts config.MsmOptions) plan.StateFile {
	sf := plan.NewStateFile()
	sf.SetRecord("p", "c", plan.MsmRecord{
		MsmID: 7, Target: "1.2.3.4", Type: "ping", AF: 4, Interval: 60,
		ProbeIDs: []uint32{1}, Options: opts,
	})
	return sf
}

func pingSpec(opts config.MsmOptions) map[plan.MsmKey]plan.MsmSpec {
	key := plan.MsmKey{Name: "p", Cohort: "c"}
	return map[plan.MsmKey]plan.MsmSpec{key: {
		Key: key, Target: "1.2.3.4", Type: plan.MsmTypePing, AF: 4, Interval: 60,
		ProbeIDs: []uint32{1}, Options: opts,
	}}
}

func TestDiff_Options(t *testing.T) {
	key := plan.MsmKey{Name: "p", Cohort: "c"}

	t.Run("identical options are a no-op", func(t *testing.T) {
		cs := plan.Diff(pingSpec(pingOpts(5)), pingState(pingOpts(5)))
		findChange(t, cs, key, plan.ChangeNoOp)
	})

	t.Run("changed option is stop and create", func(t *testing.T) {
		cs := plan.Diff(pingSpec(pingOpts(5)), pingState(pingOpts(3)))
		findChange(t, cs, key, plan.ChangeStop)
		create := findChange(t, cs, key, plan.ChangeCreate)
		assert.Equal(t, uint(5), create.Desired.Options.Ping.Packets)
	})

	t.Run("option added to a record written before options existed", func(t *testing.T) {
		cs := plan.Diff(pingSpec(pingOpts(5)), pingState(config.MsmOptions{}))
		findChange(t, cs, key, plan.ChangeStop)
		findChange(t, cs, key, plan.ChangeCreate)
	})

	t.Run("option removed", func(t *testing.T) {
		cs := plan.Diff(pingSpec(config.MsmOptions{}), pingState(pingOpts(5)))
		findChange(t, cs, key, plan.ChangeStop)
		findChange(t, cs, key, plan.ChangeCreate)
	})
}

func TestState_OptionsRoundTripAndLegacy(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.yaml")

	opts := config.MsmOptions{
		Common: config.CommonOptions{Spread: 10, StartTime: "2030-01-01T00:00:00Z"},
		DNS:    config.DNSOptions{QueryType: "AAAA", SetRDBit: true},
	}
	sf := plan.NewStateFile()
	sf.SetRecord("m", "c", plan.MsmRecord{MsmID: 1, Target: "x", Type: "dns", Options: opts})
	require.NoError(t, plan.SaveState(path, sf))

	loaded, err := plan.LoadState(path)
	require.NoError(t, err)
	rec, ok := loaded.GetRecord("m", "c")
	require.True(t, ok)
	assert.Equal(t, opts, rec.Options)

	// A state file written before options were tracked loads with zero options.
	legacy := filepath.Join(dir, "legacy.yaml")
	require.NoError(t, os.WriteFile(legacy, []byte(`
measurements:
  m:
    c:
      msm_id: 2
      target: x
      type: dns
      af: 4
      interval: 60
      probe_ids: [1]
`), 0o600))
	loaded, err = plan.LoadState(legacy)
	require.NoError(t, err)
	rec, ok = loaded.GetRecord("m", "c")
	require.True(t, ok)
	assert.Equal(t, config.MsmOptions{}, rec.Options)

	// And an empty Options is not written at all.
	sf = plan.NewStateFile()
	sf.SetRecord("m", "c", plan.MsmRecord{MsmID: 1, Target: "x", Type: "dns"})
	require.NoError(t, plan.SaveState(path, sf))
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.NotContains(t, string(data), "options")
}

func TestDesiredState_Options(t *testing.T) {
	opts := config.MsmOptions{DNS: config.DNSOptions{QueryType: "AAAA"}}
	cfg := config.Config{Measurements: []config.Measurement{{
		Name: "d", Type: config.TypeDNS, Target: "example.com", AF: 4, MsmOptions: opts,
		Cohorts: []config.MeasurementCohort{{Name: "c", IntervalSeconds: 60}},
	}}}
	selected := map[string][]selection.SelectedCohort{"d": {{
		Cohort: config.MeasurementCohort{Name: "c", IntervalSeconds: 60},
		Probes: []snapshot.Probe{{ID: 1}},
	}}}

	got := plan.DesiredState(cfg, selected)
	assert.Equal(t, opts, got[plan.MsmKey{Name: "d", Cohort: "c"}].Options)
}
