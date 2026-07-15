package plan

import (
	"github.com/supabase/atlasctl/pkg/config"
	"github.com/supabase/atlasctl/pkg/selection"
)

// MsmKey uniquely identifies a measurement within atlasctl as the pair
// (measurement name, cohort name).
type MsmKey struct {
	Name   string
	Cohort string
}

// ChangeKind classifies a single change operation.
type ChangeKind string

const (
	ChangeCreate       ChangeKind = "create"
	ChangeStop         ChangeKind = "stop"
	ChangeAddProbes    ChangeKind = "add_probes"
	ChangeRemoveProbes ChangeKind = "remove_probes"
	ChangeNoOp         ChangeKind = "noop"
)

// Change is one operation in a Changeset.
type Change struct {
	Kind ChangeKind
	Key  MsmKey

	// Desired is populated for ChangeCreate.
	Desired *MsmSpec
	// MsmID is populated for ChangeStop, ChangeAddProbes, ChangeRemoveProbes,
	// and ChangeNoOp (the live measurement ID from state).
	MsmID uint64
	// ProbeIDs is populated for ChangeAddProbes and ChangeRemoveProbes.
	ProbeIDs []uint32
}

// Changeset is the ordered list of operations needed to reconcile state with
// the desired configuration. Order within the slice is stable for a given input
// only when the inputs have a single entry — in general the map-driven diff
// produces non-deterministic ordering, so callers must not depend on it.
type Changeset []Change

// DesiredState builds the desired measurement map from the config and the
// selected probe cohorts. Keys are (measurement name, cohort name); values
// describe the fully-resolved target state including probe IDs.
func DesiredState(cfg config.Config, cohorts []selection.SelectedCohort) map[MsmKey]MsmSpec {
	// Index selected cohorts by name for O(1) lookup.
	cohortProbes := make(map[string][]uint32, len(cohorts))
	for _, r := range cohorts {
		ids := make([]uint32, len(r.Probes))
		for i, p := range r.Probes {
			ids[i] = p.ID
		}
		cohortProbes[r.Cohort.Name] = ids
	}

	//TODO: need this anymore?
	/*
		cohortInterval := make(map[string]int, len(cfg.Cohorts))
		for _, r := range cfg.Cohorts {
			cohortInterval[r.Name] = r.IntervalSeconds
		}
	*/

	desired := make(map[MsmKey]MsmSpec)
	for _, msm := range cfg.Measurements {
		for _, cohortName := range msm.Cohorts {
			key := MsmKey{Name: msm.Name, Cohort: cohortName}

			desired[key] = MsmSpec{
				Key:    key,
				Target: msm.Target,
				Type:   MsmType(msm.Type),

				AF:       msm.AF,
				Interval: msm.IntervalSeconds,
				ProbeIDs: cohortProbes[cohortName],
			}
		}
	}
	return desired
}

// Diff computes the Changeset needed to bring state into agreement with desired.
// It is a pure function: no API calls, no side effects.
//
// Structural attributes (Target, Type, Interval) are immutable — a change in
// any of them produces a Stop of the old measurement and a Create of a new one.
// Probe set changes are handled by AddProbes / RemoveProbes without recreating.
func Diff(desired map[MsmKey]MsmSpec, state StateFile) Changeset {
	var changes Changeset

	// Pass 1: reconcile each desired entry against state.
	for key, want := range desired {
		rec, exists := state.GetRecord(key.Name, key.Cohort)
		if !exists {
			d := want
			changes = append(changes, Change{Kind: ChangeCreate, Key: key, Desired: &d})
			continue
		}

		if isStructuralChange(rec, want) {
			// Stop the old measurement; create a new one with the full desired state.
			changes = append(
				changes,
				Change{Kind: ChangeStop, Key: key, MsmID: rec.MsmID},
			)
			d := want
			changes = append(changes, Change{Kind: ChangeCreate, Key: key, Desired: &d})
			continue
		}

		toAdd, toRemove := probeDelta(rec.ProbeIDs, want.ProbeIDs)
		if len(toAdd) > 0 {
			changes = append(changes, Change{
				Kind: ChangeAddProbes, Key: key, MsmID: rec.MsmID, ProbeIDs: toAdd,
			})
		}
		if len(toRemove) > 0 {
			changes = append(changes, Change{
				Kind: ChangeRemoveProbes, Key: key, MsmID: rec.MsmID, ProbeIDs: toRemove,
			})
		}
		if len(toAdd) == 0 && len(toRemove) == 0 {
			changes = append(changes, Change{Kind: ChangeNoOp, Key: key, MsmID: rec.MsmID})
		}
	}

	// Pass 2: stop anything in state that is no longer desired.
	for msmName, cohorts := range state.Measurements {
		for cohortName, rec := range cohorts {
			key := MsmKey{Name: msmName, Cohort: cohortName}
			if _, ok := desired[key]; !ok {
				changes = append(changes, Change{Kind: ChangeStop, Key: key, MsmID: rec.MsmID})
			}
		}
	}

	return changes
}

// isStructuralChange reports whether any immutable attribute has changed between
// the recorded state and the desired state.
func isStructuralChange(rec MsmRecord, want MsmSpec) bool {
	return rec.Target != want.Target ||
		MsmType(rec.Type) != want.Type ||
		rec.AF != want.AF ||
		rec.Interval != want.Interval
}

// probeDelta returns the probe IDs to add and remove when moving from current
// to desired. Order within the returned slices is not guaranteed.
func probeDelta(current, desired []uint32) (toAdd, toRemove []uint32) {
	cur := make(map[uint32]struct{}, len(current))
	for _, id := range current {
		cur[id] = struct{}{}
	}
	des := make(map[uint32]struct{}, len(desired))
	for _, id := range desired {
		des[id] = struct{}{}
	}
	for id := range des {
		if _, ok := cur[id]; !ok {
			toAdd = append(toAdd, id)
		}
	}
	for id := range cur {
		if _, ok := des[id]; !ok {
			toRemove = append(toRemove, id)
		}
	}
	return toAdd, toRemove
}
