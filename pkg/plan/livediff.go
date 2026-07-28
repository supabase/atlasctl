package plan

import (
	"context"
	"errors"
	"fmt"
)

// ErrMsmNotFound is returned by MsmClient.GetMeasurement when no measurement
// with the given ID exists on the RIPE Atlas API.
var ErrMsmNotFound = errors.New("measurement not found")

// msmStatusOngoing is the RIPE Atlas status ID for a running measurement.
// Matches goat.MeasurementStatusOngoing (2).
const msmStatusOngoing = 2

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
//  1. Builds an effective desired map by filling any empty Namespace field from
//     codec.Namespace(). Callers such as the Pulumi provider manage namespace at
//     the provider level and may not set Namespace on individual MsmSpecs; filling
//     it in here means the static diff and any created measurements always carry
//     the correct namespace without requiring callers to thread it through.
//  2. Computes the base Changeset via Diff(effectiveDesired, state).
//  3. Calls client.ListMyMeasurements to discover all ongoing tagged measurements.
//  4. For each live measurement already running under the codec namespace:
//     - If it is absent from state → orphan warning.
//     - If the static diff scheduled a stop+create only because the stored
//       namespace was empty (normalised to default, not a real structural
//       change) → cancel the stop+create and emit a noop instead.
//  5. For each state entry absent from the live list, calls GetMeasurement(id):
//     - Truly gone → ghost warning.
//     - Alive under a different namespace → promote to stop+create using the
//       effective (codec-namespace-filled) spec so the new measurement is
//       tagged correctly.
//     - Static structural change already scheduled a stop → suppress the
//       ghost warning; no duplicate stop is emitted.
//
// codec must match the TagCodec used to build the MsmClient so that orphan
// detection parses descriptions with the configured namespace, not the
// hardcoded default.
func LiveDiff(
	ctx context.Context,
	desired map[MsmKey]MsmSpec,
	state StateFile,
	client MsmClient,
	codec TagCodec,
) (Changeset, []DriftWarning, error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}

	// Build an effective desired map: for any spec with an empty Namespace,
	// fill it in from the codec. This lets callers omit the field (e.g. the
	// Pulumi provider, where namespace is a provider-level concern) and still
	// have namespace changes detected by the static diff and propagated to
	// newly created measurements.
	effective := make(map[MsmKey]MsmSpec, len(desired))
	for k, v := range desired {
		if v.Namespace == "" {
			v.Namespace = codec.Namespace()
		}
		effective[k] = v
	}

	// Static diff — pure, no API calls.
	cs := Diff(effective, state)

	// Build a set of keys already scheduled for a stop by the static diff.
	// Keys with only a ChangeStop (measurement removed from desired) are
	// distinguished from keys with Stop+Create (structural change) by also
	// checking whether the key appears in effective.
	alreadyStopping := make(map[MsmKey]bool, len(cs))
	for _, ch := range cs {
		if ch.Kind == ChangeStop {
			alreadyStopping[ch.Key] = true
		}
	}

	// Fetch all ongoing measurements with our namespace tag from the API.
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
	// Uses codec.Parse so descriptions are matched against the configured namespace,
	// not the hardcoded DefaultNamespace.
	for _, info := range live {
		name, round, ok := codec.Parse(info.Description)
		if !ok {
			// Has our namespace prefix (required by ListMyMeasurements filter) but
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
		rec, inState := state.GetRecord(name, round)
		if !inState {
			warnings = append(warnings, DriftWarning{
				Kind:  DriftOrphan,
				Key:   key,
				MsmID: info.ID,
				Message: fmt.Sprintf(
					"live measurement %d (%s/%s) is tagged as ours but absent from state file",
					info.ID, name, round,
				),
			})
			continue
		}

		// The measurement is in state and running under the codec namespace.
		// If the static diff scheduled a stop+create for this key, it may be a
		// false positive caused by namespace normalisation: the stored namespace
		// was empty (treated as DefaultNamespace by isStructuralChange) but the
		// measurement was actually already running under the codec namespace.
		// Cancel the stop+create if there are no other structural differences.
		if alreadyStopping[key] && info.ID == rec.MsmID {
			if spec, inEffective := effective[key]; inEffective {
				if !nonNamespaceStructuralChange(rec, spec) {
					cs = cancelStopCreate(cs, key, rec.MsmID)
					delete(alreadyStopping, key)
				}
			}
		}
	}

	// Ghost check: state records whose MsmID is absent from the live set.
	//
	// A state entry can be absent from liveByID for two reasons:
	//   (a) The measurement was stopped externally → real ghost.
	//   (b) The namespace changed → ListMyMeasurements filters by the new
	//       namespace and misses measurements still tagged with the old one.
	//
	// We distinguish (a) from (b) by calling GetMeasurement(id). If the
	// measurement is found and ongoing but our codec cannot parse its
	// description, the namespace has changed — promote to stop+create.
	for msmName, cohorts := range state.Measurements {
		for cohortName, rec := range cohorts {
			key := MsmKey{Name: msmName, Cohort: cohortName}
			if _, alive := liveByID[rec.MsmID]; alive {
				continue // in the live set, no drift
			}

			// A static structural change (e.g. namespace stored in state changed)
			// already scheduled a stop. Suppress the redundant ghost warning.
			if alreadyStopping[key] {
				continue
			}

			// Fetch by ID to tell ghosts from namespace mismatches.
			info, getErr := client.GetMeasurement(ctx, rec.MsmID)
			if getErr != nil {
				if errors.Is(getErr, ErrMsmNotFound) {
					warnings = append(warnings, DriftWarning{
						Kind:  DriftGhost,
						Key:   key,
						MsmID: rec.MsmID,
						Message: fmt.Sprintf(
							"state references measurement %d (%s/%s) which is not in live ongoing set",
							rec.MsmID, msmName, cohortName,
						),
					})
					continue
				}
				return nil, nil, fmt.Errorf("checking measurement %d for namespace drift: %w", rec.MsmID, getErr)
			}
			if info.StatusID != msmStatusOngoing {
				warnings = append(warnings, DriftWarning{
					Kind:  DriftGhost,
					Key:   key,
					MsmID: rec.MsmID,
					Message: fmt.Sprintf(
						"state references measurement %d (%s/%s) which is not in live ongoing set",
						rec.MsmID, msmName, cohortName,
					),
				})
				continue
			}

			// Measurement is alive and ongoing. Check whether our codec can parse
			// the description. If yes, it somehow didn't appear in liveByID —
			// flag as a ghost and let the operator investigate. If no, it was
			// created under a different namespace.
			if _, _, ok := codec.Parse(info.Description); ok {
				warnings = append(warnings, DriftWarning{
					Kind:  DriftGhost,
					Key:   key,
					MsmID: rec.MsmID,
					Message: fmt.Sprintf(
						"state references measurement %d (%s/%s) which is not in live ongoing set",
						rec.MsmID, msmName, cohortName,
					),
				})
				continue
			}

			// Namespace mismatch: the measurement is alive but was created under a
			// different namespace. Use the effective spec (namespace filled from codec)
			// so the replacement measurement is tagged with the correct namespace.
			spec := effective[key]
			cs = replaceWithStopCreate(cs, key, rec.MsmID, spec)
		}
	}

	return cs, warnings, nil
}

// nonNamespaceStructuralChange reports whether rec and want differ on any
// immutable attribute other than namespace (target, type, af, interval).
// Used to distinguish a namespace-only false positive from a real structural change.
func nonNamespaceStructuralChange(rec MsmRecord, want MsmSpec) bool {
	return rec.Target != want.Target ||
		MsmType(rec.Type) != want.Type ||
		rec.AF != want.AF ||
		rec.Interval != want.Interval
}

// cancelStopCreate removes the ChangeStop and ChangeCreate for key from cs and
// replaces them with a ChangeNoOp. Used when the static diff scheduled a
// stop+create only because of namespace normalisation but the measurement is
// already running under the correct (codec) namespace.
func cancelStopCreate(cs Changeset, key MsmKey, msmID uint64) Changeset {
	out := cs[:0:0]
	for _, ch := range cs {
		if ch.Key != key {
			out = append(out, ch)
		}
	}
	out = append(out, Change{Kind: ChangeNoOp, Key: key, MsmID: msmID})
	return out
}

// replaceWithStopCreate removes all existing changes for key from cs and
// appends a ChangeStop (to retire the old measurement) followed by a
// ChangeCreate (to recreate it under the current namespace).
func replaceWithStopCreate(cs Changeset, key MsmKey, msmID uint64, spec MsmSpec) Changeset {
	out := cs[:0:0]
	for _, ch := range cs {
		if ch.Key != key {
			out = append(out, ch)
		}
	}
	out = append(out, Change{Kind: ChangeStop, Key: key, MsmID: msmID})
	d := spec
	out = append(out, Change{Kind: ChangeCreate, Key: key, Desired: &d})
	return out
}
