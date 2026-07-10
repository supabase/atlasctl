package goatadapter

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	goat "github.com/robert-kisteleki/goat"

	"github.com/supabase/atlasctl/pkg/plan"
)

// MsmClient implements plan.MsmClient using the goat library.
type MsmClient struct {
	APIKey  *uuid.UUID
	Verbose bool
}

// GetMeasurement retrieves a single measurement by ID.
// Returns plan.ErrMsmNotFound when the API returns nil (404).
func (c *MsmClient) GetMeasurement(ctx context.Context, id uint64) (plan.MsmInfo, error) {
	if err := ctx.Err(); err != nil {
		return plan.MsmInfo{}, err
	}
	msm, err := goat.GetMeasurement(c.Verbose, uint(id), c.APIKey)
	if err != nil {
		return plan.MsmInfo{}, fmt.Errorf("GetMeasurement(%d): %w", id, err)
	}
	if msm == nil {
		return plan.MsmInfo{}, plan.ErrMsmNotFound
	}
	return toMsmInfo(*msm), nil
}

// ListMyMeasurements returns all ongoing measurements owned by the API key whose
// description contains the atlasctl tag prefix.
//
// goat quirk: limit=0 exits after the first result (see probe adapter for
// explanation). We always set limit to ^uint(0) (MaxUint).
func (c *MsmClient) ListMyMeasurements(ctx context.Context) ([]plan.MsmInfo, error) {
	filter := goat.NewMeasurementFilter()
	filter.FilterMy()
	filter.FilterStatus(goat.MeasurementStatusOngoing)
	filter.FilterDescriptionHas("[atlasctl:")
	filter.ApiKey(c.APIKey)
	filter.Verbose(c.Verbose)
	filter.Limit(^uint(0)) // see godoc on ProbeClient.FetchProbes

	ch := make(chan goat.AsyncMeasurementResult, 64)
	go filter.GetMeasurements(ch)

	var result []plan.MsmInfo
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case r, ok := <-ch:
			if !ok {
				return result, nil
			}
			if r.Error != nil {
				return nil, fmt.Errorf("listing measurements: %w", r.Error)
			}
			result = append(result, toMsmInfo(r.Measurement))
		}
	}
}

func toMsmInfo(m goat.Measurement) plan.MsmInfo {
	info := plan.MsmInfo{
		ID:       uint64(m.ID),
		StatusID: m.Status.ID,
		Type:     m.Type,
		Target:   m.Target,
	}
	if m.Description != nil {
		info.Description = *m.Description
	}
	if m.Interval != nil {
		info.Interval = int(*m.Interval)
	}
	probeIDs := make([]uint32, len(m.Probes))
	for i, p := range m.Probes {
		probeIDs[i] = uint32(p.ID)
	}
	info.ProbeIDs = probeIDs
	return info
}
