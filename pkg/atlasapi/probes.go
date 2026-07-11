// Package atlasapi implements the snapshot.Client and plan.ApplyClient
// interfaces against the live RIPE Atlas API. It is the only package in
// atlasctl that imports the underlying Atlas library.
package atlasapi

import (
	"context"
	"fmt"

	goat "github.com/robert-kisteleki/goat"

	"github.com/supabase/atlasctl/pkg/snapshot"
)

// ProbeClient implements snapshot.Client using the RIPE Atlas API.
type ProbeClient struct {
	// Verbose enables HTTP-level debug logging.
	Verbose bool
}

// FetchProbes fetches all connected probes from the RIPE Atlas API.
//
// Context cancellation is checked between probe results. Because the underlying
// HTTP calls do not accept a context, an in-flight page request will still
// complete after cancellation — only the iteration stops early.
//
// API quirk: limit=0 (the zero default) causes the loop to exit after the very
// first probe because the guard is `total >= limit` and 1>=0 is true. We
// therefore always set limit to ^uint(0) (MaxUint) to mean "unlimited".
func (c *ProbeClient) FetchProbes(ctx context.Context) ([]snapshot.Probe, error) {
	filter := goat.NewProbeFilter()
	filter.FilterStatus(goat.ProbeStatusConnected)
	filter.Verbose(c.Verbose)
	filter.Limit(^uint(0))

	ch := make(chan goat.AsyncProbeResult, 256)
	go filter.GetProbes(ch)

	var probes []snapshot.Probe
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case result, ok := <-ch:
			if !ok {
				return probes, nil
			}
			if result.Error != nil {
				return nil, fmt.Errorf("fetching probes: %w", result.Error)
			}
			if p := toProbe(result.Probe); p != nil {
				probes = append(probes, *p)
			}
		}
	}
}

// toProbe converts an API probe to a snapshot.Probe.
// Returns nil for probes that lack an IPv4 ASN or valid coordinates.
func toProbe(g goat.Probe) *snapshot.Probe {
	if g.ASN4 == nil {
		return nil
	}
	// GeoJSON coordinate order is [longitude, latitude].
	if len(g.Location.Coordinates) < 2 {
		return nil
	}

	tags := make([]string, len(g.Tags))
	for i, t := range g.Tags {
		tags[i] = t.Slug
	}

	return &snapshot.Probe{
		ID:          uint32(g.ID),
		ASN4:        uint32(*g.ASN4),
		CountryCode: g.CountryCode,
		Tags:        tags,
		Lat:         float64(g.Location.Coordinates[1]),
		Lon:         float64(g.Location.Coordinates[0]),
		StatusID:    g.Status.ID,
	}
}
