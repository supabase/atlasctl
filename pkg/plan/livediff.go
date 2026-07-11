package plan

import (
	"context"
	"errors"
	"fmt"
)

// ErrMsmNotFound is returned by MsmClient.GetMeasurement when no measurement
// with the given ID exists on the RIPE Atlas API.
var ErrMsmNotFound = errors.New("measurement not found")

// MsmInfo is a stripped-down view of a RIPE Atlas measurement containing only
// the fields needed for drift detection and apply operations.
type MsmInfo struct {
	ID          uint64
	Description string
	StatusID    uint // e.g. goat.MeasurementStatusOngoing = 2
	Type        string
	Target      string
	Interval    int
	ProbeIDs    []uint32
}

// MsmClient is the interface through which pkg/plan accesses the RIPE Atlas
// measurements API. It is implemented by pkg/atlasapi and by FakeMsmClient for tests.
type MsmClient interface {
	// GetMeasurement returns live info for one measurement by ID.
	// Returns ErrMsmNotFound if no measurement with that ID exists.
	GetMeasurement(ctx context.Context, id uint64) (MsmInfo, error)

	// ListMyMeasurements returns all ongoing measurements owned by the
	// authenticated API key whose description contains the atlasctl tag prefix.
	// This is the primary input for orphan and ghost detection in LiveDiff.
	ListMyMeasurements(ctx context.Context) ([]MsmInfo, error)
}

// DriftKind classifies a drift condition detected between state and the live API.
type DriftKind string

const (
	// DriftOrphan: the API has an atlasctl-tagged ongoing measurement whose
	// (name, round) key does not appear in the local state file.
	// Cause: manual creation, state file lost, or apply that wrote to API
	// but crashed before saving state.
	DriftOrphan DriftKind = "orphan"

	// DriftGhost: the state file references an MsmID that is no longer
	// present (or no longer ongoing) in the live API.
	// Cause: measurement stopped externally, credits exhausted, or deleted.
	DriftGhost DriftKind = "ghost"
)

// DriftWarning describes a single discrepancy between the state file and the
// live RIPE Atlas API. Warnings do not block the changeset — they are reported
// alongside it for the operator to investigate.
type DriftWarning struct {
	Kind    DriftKind
	Key     MsmKey // the (name, round) pair when known; zero value otherwise
	MsmID   uint64
	Message string
}

// LiveDiff extends the static Diff with a live API check for drift.
//
// It:
//  1. Computes the base Changeset via Diff(desired, state).
//  2. Calls client.ListMyMeasurements to discover all ongoing tagged measurements.
//  3. Flags orphans — tagged live measurements whose key is absent from state.
//  4. Flags ghosts — state records whose MsmID is not in the live ongoing set.
//
// Context cancellation is checked before any API call and propagated through
// the client calls, which themselves should honour cancellation.
func LiveDiff(
	ctx context.Context,
	desired map[MsmKey]DesiredMsm,
	state StateFile,
	client MsmClient,
) (Changeset, []DriftWarning, error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}

	// Static diff — pure, no API calls.
	cs := Diff(desired, state)

	// Fetch all ongoing measurements with our tag from the API.
	live, err := client.ListMyMeasurements(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("listing live measurements: %w", err)
	}

	// Build a set of live MsmIDs for fast ghost lookup.
	liveByID := make(map[uint64]MsmInfo, len(live))
	for _, info := range live {
		liveByID[info.ID] = info
	}

	var warnings []DriftWarning

	// Orphan check: live measurements with our tag not accounted for in state.
	for _, info := range live {
		name, round, ok := ParseTag(info.Description)
		if !ok {
			// Has our prefix (required by ListMyMeasurements filter) but
			// malformed tag — report as orphan with unknown key.
			warnings = append(warnings, DriftWarning{
				Kind:  DriftOrphan,
				MsmID: info.ID,
				Message: fmt.Sprintf(
					"live measurement %d has unrecognised atlasctl tag in description %q",
					info.ID, info.Description,
				),
			})
			continue
		}
		key := MsmKey{Name: name, Cohort: round}
		if _, inState := state.GetRecord(name, round); !inState {
			warnings = append(warnings, DriftWarning{
				Kind:  DriftOrphan,
				Key:   key,
				MsmID: info.ID,
				Message: fmt.Sprintf(
					"live measurement %d (%s/%s) is tagged as ours but absent from state file",
					info.ID, name, round,
				),
			})
		}
	}

	// Ghost check: state records whose MsmID is absent from the live set.
	for msmName, cohorts := range state.Measurements {
		for cohortName, rec := range cohorts {
			if _, alive := liveByID[rec.MsmID]; !alive {
				key := MsmKey{Name: msmName, Cohort: cohortName}
				warnings = append(warnings, DriftWarning{
					Kind:  DriftGhost,
					Key:   key,
					MsmID: rec.MsmID,
					Message: fmt.Sprintf(
						"state references measurement %d (%s/%s) which is not in live ongoing set",
						rec.MsmID, msmName, cohortName,
					),
				})
			}
		}
	}

	return cs, warnings, nil
}
