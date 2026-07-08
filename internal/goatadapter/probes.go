// Package goatadapter adapts the goat library to the interfaces defined in pkg/.
// It is the only place in the codebase that imports github.com/robert-kisteleki/goat,
// keeping pkg/ free of that dependency and fully testable with fakes.
package goatadapter

import (
	"context"
	"fmt"

	goat "github.com/robert-kisteleki/goat"

	"github.com/supabase/atlascli/pkg/snapshot"
)

// ProbeClient implements snapshot.Client using the goat library.
type ProbeClient struct {
	// Verbose enables goat's HTTP-level debug logging.
	Verbose bool
}

// FetchProbes fetches all connected IPv4-capable probes from the RIPE Atlas API.
//
// Context cancellation is checked between probe results. Because the underlying
// goat HTTP calls do not accept a context, an in-flight page request will still
// complete after cancellation — only the iteration stops early.
//
// goat quirk: ProbeFilter.limit=0 (the zero default) causes the loop to exit
// after the very first probe because the guard is `total >= limit` and 1>=0 is
// true. We therefore always set limit to ^uint(0) (MaxUint) to mean "unlimited".
func (c *ProbeClient) FetchProbes(ctx context.Context) ([]snapshot.Probe, error) {
	filter := goat.NewProbeFilter()
	filter.FilterStatus(goat.ProbeStatusConnected)
	filter.Verbose(c.Verbose)
	filter.Limit(^uint(0)) // see godoc above

	// Buffer the channel so the goroutine is less likely to block if we exit early.
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

// toProbe converts a goat.Probe to a snapshot.Probe.
// Returns nil for probes that lack an IPv4 ASN or valid coordinates,
// since both are required for scoring and selection.
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
