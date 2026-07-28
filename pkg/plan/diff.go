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

// DesiredState builds the desired measurement map from per-measurement
// selection results. selected maps measurement name to its ordered cohort
// selection output.
//
// The namespace is derived from cfg.Namespace: if empty, DefaultNamespace is
// used. The effective namespace is embedded in every MsmSpec so isStructuralChange
// can detect namespace changes. Deriving it here (rather than accepting it as a
// parameter) ensures callers cannot accidentally pass a stale or empty value.
func DesiredState(cfg config.Config, selected map[string][]selection.SelectedCohort) map[MsmKey]MsmSpec {
	ns := cfg.Namespace
	if ns == "" {
		ns = DefaultNamespace
	}

	desired := make(map[MsmKey]MsmSpec)
	for _, msm := range cfg.Measurements {
		cohorts, ok := selected[msm.Name]
		if !ok {
			continue
		}
		for _, sc := range cohorts {
			key := MsmKey{Name: msm.Name, Cohort: sc.Cohort.Name}
			ids := make([]uint32, len(sc.Probes))
			for i, p := range sc.Probes {
				ids[i] = p.ID
			}
			msmType := MsmType(msm.Type)
			desired[key] = MsmSpec{
				Key:           key,
				Namespace:     ns,
				Target:        msm.Target,
				Type:          msmType,
				AF:            msm.AF,
				Interval:      sc.Cohort.IntervalSeconds,
				ProbeIDs:      ids,
				HourlyCredits: specHourlyCredits(len(ids), msmType, sc.Cohort.IntervalSeconds),
				DailyCredits:  specDailyCredits(len(ids), msmType, sc.Cohort.IntervalSeconds),
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
//
// Both an empty rec.Namespace and an empty want.Namespace are treated as
// DefaultNamespace. An empty stored namespace means the record predates namespace
// tracking; an empty desired namespace means the caller did not set one (the
// codec always defaults to DefaultNamespace). Treating both as DefaultNamespace
// ensures that pkg callers who never set namespace see no spurious structural
// changes, while a change from the default to a custom value is correctly
// detected without requiring a live API call.
func isStructuralChange(rec MsmRecord, want MsmSpec) bool {
	recNS := rec.Namespace
	if recNS == "" {
		recNS = DefaultNamespace
	}
	wantNS := want.Namespace
	if wantNS == "" {
		wantNS = DefaultNamespace
	}
	if recNS != wantNS {
		return true
	}
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
