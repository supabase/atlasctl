package plan

import (
	"github.com/supabase/atlascli/pkg/config"
	"github.com/supabase/atlascli/pkg/selection"
)

// MsmKey uniquely identifies a measurement within atlasctl as the pair
// (measurement name, round name).
type MsmKey struct {
	Name  string
	Round string
}

// DesiredMsm describes the fully-resolved desired state for one (name, round) pair.
type DesiredMsm struct {
	Target   string
	Type     MsmType
	Interval int
	ProbeIDs []uint32
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
	Desired *DesiredMsm
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
// selected probe rounds. Keys are (measurement name, round name); values
// describe the fully-resolved target state including probe IDs.
func DesiredState(cfg config.Config, rounds []selection.SelectedRound) map[MsmKey]DesiredMsm {
	// Index selected rounds by name for O(1) lookup.
	roundProbes := make(map[string][]uint32, len(rounds))
	for _, r := range rounds {
		ids := make([]uint32, len(r.Probes))
		for i, p := range r.Probes {
			ids[i] = p.ID
		}
		roundProbes[r.Round.Name] = ids
	}

	roundInterval := make(map[string]int, len(cfg.Rounds))
	for _, r := range cfg.Rounds {
		roundInterval[r.Name] = r.IntervalSeconds
	}

	desired := make(map[MsmKey]DesiredMsm)
	for _, msm := range cfg.Measurements {
		for _, roundName := range msm.Rounds {
			desired[MsmKey{Name: msm.Name, Round: roundName}] = DesiredMsm{
				Target:   msm.Target,
				Type:     MsmType(msm.Type),
				Interval: roundInterval[roundName],
				ProbeIDs: roundProbes[roundName],
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
func Diff(desired map[MsmKey]DesiredMsm, state StateFile) Changeset {
	var changes Changeset

	// Pass 1: reconcile each desired entry against state.
	for key, want := range desired {
		rec, exists := state.GetRecord(key.Name, key.Round)
		if !exists {
			d := want
			changes = append(changes, Change{Kind: ChangeCreate, Key: key, Desired: &d})
			continue
		}

		if isStructuralChange(rec, want) {
			// Stop the old measurement; create a new one with the full desired state.
			changes = append(changes,
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
	for msmName, rounds := range state.Measurements {
		for roundName, rec := range rounds {
			key := MsmKey{Name: msmName, Round: roundName}
			if _, ok := desired[key]; !ok {
				changes = append(changes, Change{Kind: ChangeStop, Key: key, MsmID: rec.MsmID})
			}
		}
	}

	return changes
}

// isStructuralChange reports whether any immutable attribute has changed between
// the recorded state and the desired state.
func isStructuralChange(rec MsmRecord, want DesiredMsm) bool {
	return rec.Target != want.Target ||
		MsmType(rec.Type) != want.Type ||
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
