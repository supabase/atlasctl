package atlasapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"

	"github.com/google/uuid"
	goat "github.com/robert-kisteleki/goat"

	"github.com/supabase/atlasctl/pkg/plan"
)

// MsmClient implements plan.MsmClient using the RIPE Atlas API.
type MsmClient struct {
	apiKey   *uuid.UUID
	Verbose  bool
	TagCodec plan.TagCodec

	// baseURL overrides apiBaseURL; set only by tests.
	baseURL string
	// httpClient overrides http.DefaultClient; set only by tests.
	httpClient *http.Client
}

// apiBaseURL is the RIPE Atlas REST API root.
const apiBaseURL = "https://atlas.ripe.net/api/v2/"

// NewMsmClient constructs an MsmClient from a raw API key string.
func NewMsmClient(apiKey string, verbose bool, codec plan.TagCodec) (*MsmClient, error) {
	k, err := uuid.Parse(apiKey)
	if err != nil {
		return nil, fmt.Errorf("invalid API key (expected UUID): %w", err)
	}
	return &MsmClient{apiKey: &k, Verbose: verbose, TagCodec: codec}, nil
}

// GetMeasurement retrieves a single measurement by ID, including its current
// participating probes.
// Returns plan.ErrMsmNotFound when the API returns 404.
//
// API quirk: GET /measurements/{id}/ omits the probe list unless
// optional_fields=probes is requested. goat.GetMeasurement does not send it,
// so every probe list came back empty and drift detection saw all probes
// removed. The request is made here instead, and decoded into goat.Measurement.
func (c *MsmClient) GetMeasurement(ctx context.Context, id uint64) (plan.MsmInfo, error) {
	base := c.baseURL
	if base == "" {
		base = apiBaseURL
	}
	q := url.Values{}
	q.Set("optional_fields", "probes")
	q.Set("format[datetime]", "iso-8601")
	u := fmt.Sprintf("%smeasurements/%d/?%s", base, id, q.Encode())

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return plan.MsmInfo{}, fmt.Errorf("GetMeasurement(%d): %w", id, err)
	}
	req.Header.Set("User-Agent", "atlasctl")
	if c.apiKey != nil {
		req.Header.Set("Authorization", "Key "+c.apiKey.String())
	}
	if c.Verbose {
		fmt.Printf("# API call: GET %s\n", u)
	}

	client := c.httpClient
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return plan.MsmInfo{}, fmt.Errorf("GetMeasurement(%d): %w", id, err)
	}
	defer resp.Body.Close()

	switch {
	case resp.StatusCode == http.StatusNotFound:
		return plan.MsmInfo{}, plan.ErrMsmNotFound
	case resp.StatusCode != http.StatusOK:
		return plan.MsmInfo{}, fmt.Errorf("GetMeasurement(%d): %w", id,
			errors.New("unexpected HTTP status "+resp.Status))
	}

	var msm goat.Measurement
	if err := json.NewDecoder(resp.Body).Decode(&msm); err != nil {
		return plan.MsmInfo{}, fmt.Errorf("GetMeasurement(%d): decoding response: %w", id, err)
	}
	return toMsmInfo(msm), nil
}

// ListMyMeasurements returns all ongoing measurements owned by the API key
// whose description contains the atlasctl tag prefix.
//
// API quirk: limit=0 exits after the first result. Always set to ^uint(0).
func (c *MsmClient) ListMyMeasurements(ctx context.Context) ([]plan.MsmInfo, error) {
	filter := goat.NewMeasurementFilter()
	filter.FilterTags([]string{c.TagCodec.Namespace()})
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
