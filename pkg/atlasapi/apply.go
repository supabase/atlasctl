package atlasapi

import (
	"context"
	"fmt"

	goat "github.com/robert-kisteleki/goat"

	"github.com/supabase/atlasctl/pkg/plan"
)

// ApplyClient implements plan.ApplyClient using the RIPE Atlas API.
// It embeds MsmClient to satisfy plan.MsmClient as well.
type ApplyClient struct {
	MsmClient
}

// NewApplyClient returns an ApplyClient ready for use.
func NewApplyClient(apiKey string, verbose bool, codec plan.TagCodec) (*ApplyClient, error) {
	msm, err := NewMsmClient(apiKey, verbose, codec)
	if err != nil {
		return nil, err
	}
	return &ApplyClient{MsmClient: *msm}, nil
}

// ValidateMsmSpec builds the goat measurement spec locally and calls GetApiJson
// to check for structural errors without making any API call. Use this during
// plan to surface spec problems before apply.
func ValidateMsmSpec(spec plan.MsmSpec) error {
	ms := goat.NewMeasurementSpec()
	baseOpts := &goat.BaseOptions{Interval: uint(spec.Interval)}
	af := uint(spec.AF)

	var defErr error
	switch spec.Type {
	case plan.MsmTypeDNS:
		defErr = ms.AddDns("validate", "", af, baseOpts, &goat.DnsOptions{
			Type: "A", Argument: spec.Target, UseResolver: true,
		})
	case plan.MsmTypePing:
		defErr = ms.AddPing("validate", spec.Target, af, baseOpts, nil)
	case plan.MsmTypeTLS:
		defErr = ms.AddTls("validate", spec.Target, af, baseOpts, nil)
	case plan.MsmTypeTraceroute:
		defErr = ms.AddTrace("validate", spec.Target, af, baseOpts, nil)
	default:
		return fmt.Errorf("unsupported measurement type %q", spec.Type)
	}
	if defErr != nil {
		return fmt.Errorf("invalid measurement spec: %w", defErr)
	}

	// GetApiJson requires at least one probe entry; use a placeholder.
	if err := ms.AddProbesList([]uint{1}); err != nil {
		return err
	}
	_, err := ms.GetApiJson()
	return err
}

// CreateMeasurement creates a new RIPE Atlas measurement from spec.
// The description embeds the atlasctl tag so that LiveDiff can reconcile
// state against the live API.
//
// API quirk: Schedule() does not accept a context. We check ctx.Err()
// immediately before calling it.
func (c *ApplyClient) CreateMeasurement(ctx context.Context, spec plan.MsmSpec) (uint64, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}

	ms := goat.NewMeasurementSpec()
	ms.ApiKey(c.apiKey)
	ms.Verbose(c.Verbose)

	desc := c.TagCodec.Format(spec.Key.Name, spec.Key.Cohort)
	baseOpts := &goat.BaseOptions{
		Interval: uint(spec.Interval),
		Tags:     []string{c.TagCodec.Prefix()},
	}
	af := uint(spec.AF)

	var defErr error
	switch spec.Type {
	case plan.MsmTypeDNS:
		// spec.Target is the domain to resolve (query_argument). DNS measurements
		// use the probe's default resolver; no explicit nameserver target is set.
		defErr = ms.AddDns(desc, "", af, baseOpts, &goat.DnsOptions{
			Type:     "A",
			Argument: spec.Target, UseResolver: true,
		})
	case plan.MsmTypePing:
		defErr = ms.AddPing(desc, spec.Target, af, baseOpts, nil)
	case plan.MsmTypeTLS:
		defErr = ms.AddTls(desc, spec.Target, af, baseOpts, nil)
	case plan.MsmTypeTraceroute:
		defErr = ms.AddTrace(desc, spec.Target, af, baseOpts, nil)
	default:
		return 0, fmt.Errorf("unsupported measurement type %q", spec.Type)
	}
	if defErr != nil {
		return 0, fmt.Errorf("building measurement definition: %w", defErr)
	}

	probeList := make([]uint, len(spec.ProbeIDs))
	for i, id := range spec.ProbeIDs {
		probeList[i] = uint(id)
	}
	if err := ms.AddProbesList(probeList); err != nil {
		return 0, fmt.Errorf("AddProbesList: %w", err)
	}

	ids, err := ms.Schedule()
	if err != nil {
		return 0, fmt.Errorf("Schedule: %w", err)
	}
	if len(ids) == 0 {
		return 0, fmt.Errorf("Schedule returned no measurement IDs")
	}
	return uint64(ids[0]), nil
}

// StopMeasurement stops the ongoing measurement with the given ID.
//
// API quirk: Stop() does not accept a context. We check ctx.Err() immediately
// before calling it.
func (c *ApplyClient) StopMeasurement(ctx context.Context, id uint64) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	ms := goat.NewMeasurementSpec()
	ms.ApiKey(c.apiKey)
	ms.Verbose(c.Verbose)
	if err := ms.Stop(uint(id)); err != nil {
		return fmt.Errorf("Stop(%d): %w", id, err)
	}
	return nil
}

// AddParticipants adds probeIDs to the ongoing measurement via a participation
// request.
//
// API quirk: ParticipationRequest() does not accept a context.
func (c *ApplyClient) AddParticipants(ctx context.Context, id uint64, probeIDs []uint32) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	ms := goat.NewMeasurementSpec()
	ms.ApiKey(c.apiKey)
	ms.Verbose(c.Verbose)
	list := make([]uint, len(probeIDs))
	for i, pid := range probeIDs {
		list[i] = uint(pid)
	}
	if err := ms.AddProbesList(list); err != nil {
		return fmt.Errorf("AddProbesList: %w", err)
	}
	if _, err := ms.ParticipationRequest(uint(id), true); err != nil {
		return fmt.Errorf("ParticipationRequest(add, %d): %w", id, err)
	}
	return nil
}

// RemoveParticipants removes probeIDs from the ongoing measurement via a
// participation request.
//
// API quirk: ParticipationRequest() does not accept a context.
func (c *ApplyClient) RemoveParticipants(ctx context.Context, id uint64, probeIDs []uint32) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	ms := goat.NewMeasurementSpec()
	ms.ApiKey(c.apiKey)
	ms.Verbose(c.Verbose)
	list := make([]uint, len(probeIDs))
	for i, pid := range probeIDs {
		list[i] = uint(pid)
	}
	if err := ms.AddProbesList(list); err != nil {
		return fmt.Errorf("AddProbesList: %w", err)
	}
	if _, err := ms.ParticipationRequest(uint(id), false); err != nil {
		return fmt.Errorf("ParticipationRequest(remove, %d): %w", id, err)
	}
	return nil
}
