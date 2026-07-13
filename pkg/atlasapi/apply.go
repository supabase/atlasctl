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
	ms.ApiKey(c.MsmClient.apiKey)
	ms.Verbose(c.MsmClient.Verbose)

	desc := c.MsmClient.TagCodec.Format(spec.Key.Name, spec.Key.Cohort)
	baseOpts := &goat.BaseOptions{Interval: uint(spec.Interval)}
	af := uint(spec.AF)

	var defErr error
	switch spec.Type {
	case "dns":
		defErr = ms.AddDns(desc, spec.Target, af, baseOpts, &goat.DnsOptions{Type: "A"})
	case "ping":
		defErr = ms.AddPing(desc, spec.Target, af, baseOpts, nil)
	case "tls":
		defErr = ms.AddTls(desc, spec.Target, af, baseOpts, nil)
	case "traceroute":
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
	ms.ApiKey(c.MsmClient.apiKey)
	ms.Verbose(c.MsmClient.Verbose)
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
	ms.ApiKey(c.MsmClient.apiKey)
	ms.Verbose(c.MsmClient.Verbose)
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
	ms.ApiKey(c.MsmClient.apiKey)
	ms.Verbose(c.MsmClient.Verbose)
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
