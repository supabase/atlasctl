package atlasapi

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	goat "github.com/robert-kisteleki/goat"

	"github.com/supabase/atlasctl/pkg/plan"
)

// MsmClient implements plan.MsmClient using the RIPE Atlas API.
type MsmClient struct {
	apiKey   *uuid.UUID
	Verbose  bool
	TagCodec plan.TagCodec
}

// NewMsmClient constructs an MsmClient from a raw API key string.
func NewMsmClient(apiKey string, verbose bool, codec plan.TagCodec) (*MsmClient, error) {
	k, err := uuid.Parse(apiKey)
	if err != nil {
		return nil, fmt.Errorf("invalid API key (expected UUID): %w", err)
	}
	return &MsmClient{apiKey: &k, Verbose: verbose, TagCodec: codec}, nil
}

// GetMeasurement retrieves a single measurement by ID.
// Returns plan.ErrMsmNotFound when the API returns nil (404).
func (c *MsmClient) GetMeasurement(ctx context.Context, id uint64) (plan.MsmInfo, error) {
	if err := ctx.Err(); err != nil {
		return plan.MsmInfo{}, err
	}
	msm, err := goat.GetMeasurement(c.Verbose, uint(id), c.apiKey)
	if err != nil {
		return plan.MsmInfo{}, fmt.Errorf("GetMeasurement(%d): %w", id, err)
	}
	if msm == nil {
		return plan.MsmInfo{}, plan.ErrMsmNotFound
	}
	return toMsmInfo(*msm), nil
}

// ListMyMeasurements returns all ongoing measurements owned by the API key
// whose description contains the atlasctl tag prefix.
//
// API quirk: limit=0 exits after the first result. Always set to ^uint(0).
func (c *MsmClient) ListMyMeasurements(ctx context.Context) ([]plan.MsmInfo, error) {
	filter := goat.NewMeasurementFilter()
	filter.FilterTags([]string{c.TagCodec.Prefix()})
	filter.FilterMy()
	filter.FilterStatus(goat.MeasurementStatusOngoing)
	filter.ApiKey(c.apiKey)
	filter.Verbose(c.Verbose)
	filter.Limit(^uint(0))

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
