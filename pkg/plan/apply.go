package plan

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/rs/zerolog"
)

// MsmSpec carries the information needed to create one RIPE Atlas measurement.
type MsmSpec struct {
	Key       MsmKey
	Namespace string // effective namespace (after TagCodec defaulting) at desired-state build time
	Target    string
	Type      MsmType
	AF        int // address family: 4 or 6
	Interval  int
	ProbeIDs  []uint32
}

// ApplyClient extends MsmClient with the mutating operations needed by Apply.
type ApplyClient interface {
	MsmClient
	// CreateMeasurement creates a new RIPE Atlas measurement from spec.
	// Returns the assigned measurement ID.
	CreateMeasurement(ctx context.Context, spec MsmSpec) (uint64, error)
	// StopMeasurement stops the ongoing measurement with the given ID.
	StopMeasurement(ctx context.Context, id uint64) error
	// AddParticipants adds probeIDs to the ongoing measurement.
	AddParticipants(ctx context.Context, id uint64, probeIDs []uint32) error
	// RemoveParticipants removes probeIDs from the ongoing measurement.
	RemoveParticipants(ctx context.Context, id uint64, probeIDs []uint32) error
}

// ApplyOptions controls the behaviour of Apply.
type ApplyOptions struct {
	// DryRun logs what would happen without making any API calls.
	DryRun bool
}

// Apply executes cs against the live RIPE Atlas API, returning the updated
// state. It:
//   - Checks ctx before every API call; on cancellation returns immediately.
//   - Logs each operation at Info level (Debug for NoOp) via log.
//   - In dry-run mode logs "would ..." but makes no API calls.
//   - Continues past per-operation API errors, collecting them.
//   - Returns (updatedState, joinedErrors). updatedState reflects only the
//     operations that completed successfully.
func Apply(
	ctx context.Context,
	cs Changeset,
	state StateFile,
	client ApplyClient,
	opts ApplyOptions,
	log zerolog.Logger,
) (StateFile, error) {
	out := cloneState(state)
	var errs []error

	for _, ch := range cs {
		if err := ctx.Err(); err != nil {
			return out, err
		}

		switch ch.Kind {
		case ChangeNoOp:
			log.Debug().
				Str("name", ch.Key.Name).
				Str("cohort", ch.Key.Cohort).
				Msg("no change required")

		case ChangeCreate:
			if opts.DryRun {
				log.Info().
					Str("name", ch.Key.Name).
					Str("cohort", ch.Key.Cohort).
					Str("type", string(ch.Desired.Type)).
					Str("target", ch.Desired.Target).
					Msg("[dry-run] would create measurement")
				continue
			}
			spec := *ch.Desired
			spec.Key = ch.Key // ch.Key is authoritative
			id, err := client.CreateMeasurement(ctx, spec)
			if err != nil {
				log.Error().Err(err).
					Str("name", ch.Key.Name).
					Str("cohort", ch.Key.Cohort).
					Msg("create measurement failed")
				errs = append(errs, fmt.Errorf("create %s/%s: %w", ch.Key.Name, ch.Key.Cohort, err))
				continue
			}
			log.Info().
				Uint64("id", id).
				Str("name", ch.Key.Name).
				Str("cohort", ch.Key.Cohort).
				Msg("created measurement")
			out.SetRecord(ch.Key.Name, ch.Key.Cohort, MsmRecord{
				MsmID:     id,
				Namespace: ch.Desired.Namespace,
				Target:    ch.Desired.Target,
				Type:      string(ch.Desired.Type),
				AF:        ch.Desired.AF,
				Interval:  ch.Desired.Interval,
				ProbeIDs:  ch.Desired.ProbeIDs,
			})

		case ChangeStop:
			if opts.DryRun {
				log.Info().
					Uint64("id", ch.MsmID).
					Str("name", ch.Key.Name).
					Str("cohort", ch.Key.Cohort).
					Msg("[dry-run] would stop measurement")
				continue
			}
			if err := client.StopMeasurement(ctx, ch.MsmID); err != nil {
				log.Error().Err(err).
					Uint64("id", ch.MsmID).
					Str("name", ch.Key.Name).
					Str("cohort", ch.Key.Cohort).
					Msg("stop measurement failed")
				errs = append(errs, fmt.Errorf("stop %s/%s (id=%d): %w",
					ch.Key.Name, ch.Key.Cohort, ch.MsmID, err))
				continue
			}
			log.Info().
				Uint64("id", ch.MsmID).
				Str("name", ch.Key.Name).
				Str("cohort", ch.Key.Cohort).
				Msg("stopped measurement")
			out.DeleteRecord(ch.Key.Name, ch.Key.Cohort)

		case ChangeAddProbes:
			if opts.DryRun {
				log.Info().
					Uint64("id", ch.MsmID).
					Str("name", ch.Key.Name).
					Str("cohort", ch.Key.Cohort).
					Interface("probe_ids", ch.ProbeIDs).
					Msg("[dry-run] would add probes")
				continue
			}
			if err := client.AddParticipants(ctx, ch.MsmID, ch.ProbeIDs); err != nil {
				log.Error().Err(err).
					Uint64("id", ch.MsmID).
					Str("name", ch.Key.Name).
					Str("cohort", ch.Key.Cohort).
					Msg("add probes failed")
				errs = append(errs, fmt.Errorf("add probes %s/%s (id=%d): %w",
					ch.Key.Name, ch.Key.Cohort, ch.MsmID, err))
				continue
			}
			log.Info().
				Uint64("id", ch.MsmID).
				Str("name", ch.Key.Name).
				Str("cohort", ch.Key.Cohort).
				Msg("added probes")
			if rec, ok := out.GetRecord(ch.Key.Name, ch.Key.Cohort); ok {
				rec.ProbeIDs = mergeProbeIDs(rec.ProbeIDs, ch.ProbeIDs)
				out.SetRecord(ch.Key.Name, ch.Key.Cohort, rec)
			}

		case ChangeRemoveProbes:
			if opts.DryRun {
				log.Info().
					Uint64("id", ch.MsmID).
					Str("name", ch.Key.Name).
					Str("cohort", ch.Key.Cohort).
					Interface("probe_ids", ch.ProbeIDs).
					Msg("[dry-run] would remove probes")
				continue
			}
			if err := client.RemoveParticipants(ctx, ch.MsmID, ch.ProbeIDs); err != nil {
				log.Error().Err(err).
					Uint64("id", ch.MsmID).
					Str("name", ch.Key.Name).
					Str("cohort", ch.Key.Cohort).
					Msg("remove probes failed")
				errs = append(errs, fmt.Errorf("remove probes %s/%s (id=%d): %w",
					ch.Key.Name, ch.Key.Cohort, ch.MsmID, err))
				continue
			}
			log.Info().
				Uint64("id", ch.MsmID).
				Str("name", ch.Key.Name).
				Str("cohort", ch.Key.Cohort).
				Msg("removed probes")
			if rec, ok := out.GetRecord(ch.Key.Name, ch.Key.Cohort); ok {
				rec.ProbeIDs = subtractProbeIDs(rec.ProbeIDs, ch.ProbeIDs)
				out.SetRecord(ch.Key.Name, ch.Key.Cohort, rec)
			}
		}
	}

	out.LastApplied = time.Now().UTC()

	if len(errs) > 0 {
		return out, errors.Join(errs...)
	}
	return out, nil
}

// cloneState returns a deep copy of sf so Apply can mutate it safely.
func cloneState(sf StateFile) StateFile {
	out := NewStateFile()
	out.LastApplied = sf.LastApplied
	out.ProbeSnapshot = sf.ProbeSnapshot
	out.ProbeSnapshotFetched = sf.ProbeSnapshotFetched
	for name, cohorts := range sf.Measurements {
		for cohort, rec := range cohorts {
			cloned := MsmRecord{
				MsmID:     rec.MsmID,
				Namespace: rec.Namespace,
				Target:    rec.Target,
				Type:      rec.Type,
				AF:        rec.AF,
				Interval:  rec.Interval,
				ProbeIDs:  append([]uint32(nil), rec.ProbeIDs...),
			}
			out.SetRecord(name, cohort, cloned)
		}
	}
	return out
}

// mergeProbeIDs appends add to base, deduplicating.
func mergeProbeIDs(base, add []uint32) []uint32 {
	seen := make(map[uint32]struct{}, len(base))
	for _, id := range base {
		seen[id] = struct{}{}
	}
	result := append([]uint32(nil), base...)
	for _, id := range add {
		if _, ok := seen[id]; !ok {
			result = append(result, id)
			seen[id] = struct{}{}
		}
	}
	return result
}

// subtractProbeIDs returns base with every id in remove excluded.
func subtractProbeIDs(base, remove []uint32) []uint32 {
	rm := make(map[uint32]struct{}, len(remove))
	for _, id := range remove {
		rm[id] = struct{}{}
	}
	result := base[:0:0]
	for _, id := range base {
		if _, ok := rm[id]; !ok {
			result = append(result, id)
		}
	}
	return result
}
