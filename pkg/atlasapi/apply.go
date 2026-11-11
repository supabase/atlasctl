package atlasapi

import (
	"context"
	"fmt"
	"time"

	goat "github.com/robert-kisteleki/goat"

	"github.com/supabase/atlasctl/pkg/config"
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

// baseOptsFromSpec maps the interval, tags and common options onto goat's
// BaseOptions. Zero values are omitted by goat, so the API default applies.
func baseOptsFromSpec(spec plan.MsmSpec, tags []string) *goat.BaseOptions {
	c := spec.Options.Common
	return &goat.BaseOptions{
		Interval:            uint(spec.Interval),
		Tags:                tags,
		ResolveOnProbe:      c.ResolveOnProbe,
		Spread:              c.Spread,
		SkipDNSCheck:        c.SkipDNSCheck,
		DnsReLookup:         c.TargetUpdateHours,
		AutoTopup:           c.AutoTopup,
		AutoTopupDays:       c.AutoTopupDaysOff,
		AutoTopupSimilarity: c.AutoTopupSimilarity,
	}
}

func pingOptsFromSpec(spec plan.MsmSpec) *goat.PingOptions {
	o := spec.Options.Ping
	return &goat.PingOptions{
		Packets:        o.Packets,
		PacketInterval: o.PacketInterval,
		IncludeProbeID: o.IncludeProbeID,
	}
}

// traceOptsFromSpec leaves Protocol empty when unset: goat then defaults to UDP.
func traceOptsFromSpec(spec plan.MsmSpec) *goat.TraceOptions {
	o := spec.Options.Traceroute
	return &goat.TraceOptions{
		Protocol:        o.Protocol,
		ResponseTimeout: o.ResponseTimeout,
		Packets:         o.Packets,
		ParisId:         o.Paris,
		FirstHop:        o.FirstHop,
		LastHop:         o.MaxHops,
		DestinationEH:   o.DestinationOptSize,
		HopByHopEH:      o.HopByHopOptSize,
		DontFragment:    o.DontFragment,
	}
}

// dnsOptsFromSpec resolves spec.Target (the query argument) through the probe's
// own resolver. Empty Type, Class and Protocol fall back to goat's A, IN and UDP.
func dnsOptsFromSpec(spec plan.MsmSpec) *goat.DnsOptions {
	o := spec.Options.DNS
	return &goat.DnsOptions{
		Protocol:       o.Protocol,
		Class:          o.QueryClass,
		Type:           o.QueryType,
		Argument:       spec.Target,
		UseResolver:    true,
		UseMacros:      o.UseMacros,
		Nsid:           o.SetNSIDBit,
		UdpPayloadSize: o.UDPPayloadSize,
		Retries:        o.Retry,
		IncludeQbuf:    o.IncludeQbuf,
		PrependProbeID: o.PrependProbeID,
		SetRd:          o.SetRDBit,
		SetDo:          o.SetDOBit,
		Timeout:        o.Timeout,
	}
}

func tlsOptsFromSpec(spec plan.MsmSpec) *goat.TlsOptions {
	o := spec.Options.TLS
	return &goat.TlsOptions{Port: o.Port, Sni: o.Hostname}
}

// httpOptsFromSpec converts the HTTP-specific fields on a MsmSpec into a
// *goat.HttpOptions. Returns nil when all fields are zero (goat uses its own
// defaults: method HEAD, path "/", port 80).
func httpOptsFromSpec(spec plan.MsmSpec) *goat.HttpOptions {
	o := spec.Options.HTTP
	if spec.HttpMethod == "" && spec.HttpPath == "" && spec.HttpPort == 0 && spec.HttpVersion == "" &&
		o == (config.HTTPOptions{}) {
		return nil
	}
	return &goat.HttpOptions{
		Method:             spec.HttpMethod,
		Path:               spec.HttpPath,
		Port:               spec.HttpPort,
		Version:            spec.HttpVersion,
		Query:              o.QueryString,
		HeaderBytes:        o.HeaderBytes,
		ExtendedTiming:     o.ExtendedTiming,
		MoreExtendedTiming: o.MoreExtendedTiming,
	}
}

// newGoatSpec builds the goat measurement spec for spec, without probes or
// credentials: the definition, plus the spec-level start, stop and bill-to
// options. Shared by CreateMeasurement and ValidateMsmSpec so that validation
// sees exactly what apply will send.
func newGoatSpec(desc string, spec plan.MsmSpec, tags []string) (*goat.MeasurementSpec, error) {
	ms := goat.NewMeasurementSpec()
	base := baseOptsFromSpec(spec, tags)
	af := uint(spec.AF)

	var defErr error
	switch spec.Type {
	case plan.MsmTypeDNS:
		// The target of a DNS definition is a nameserver; atlasctl always uses
		// the probe's resolver, so it is left empty and spec.Target goes in
		// query_argument.
		defErr = ms.AddDns(desc, "", af, base, dnsOptsFromSpec(spec))
	case plan.MsmTypePing:
		defErr = ms.AddPing(desc, spec.Target, af, base, pingOptsFromSpec(spec))
	case plan.MsmTypeTLS:
		defErr = ms.AddTls(desc, spec.Target, af, base, tlsOptsFromSpec(spec))
	case plan.MsmTypeTraceroute:
		defErr = ms.AddTrace(desc, spec.Target, af, base, traceOptsFromSpec(spec))
	case plan.MsmTypeHTTP:
		defErr = ms.AddHttp(desc, spec.Target, af, base, httpOptsFromSpec(spec))
	default:
		return nil, fmt.Errorf("unsupported measurement type %q", spec.Type)
	}
	if defErr != nil {
		return nil, defErr
	}

	c := spec.Options.Common
	if c.StartTime != "" {
		t, err := time.Parse(time.RFC3339, c.StartTime)
		if err != nil {
			return nil, fmt.Errorf("start_time: %w", err)
		}
		ms.StartTime(t)
	}
	if c.StopTime != "" {
		t, err := time.Parse(time.RFC3339, c.StopTime)
		if err != nil {
			return nil, fmt.Errorf("stop_time: %w", err)
		}
		ms.EndTime(t)
	}
	if c.BillTo != "" {
		ms.BillTo(c.BillTo)
	}
	return ms, nil
}

// specJSON returns the JSON body that CreateMeasurement would POST for spec,
// using a placeholder probe because the API requires at least one.
func specJSON(spec plan.MsmSpec) ([]byte, error) {
	ms, err := newGoatSpec("validate", spec, nil)
	if err != nil {
		return nil, fmt.Errorf("invalid measurement spec: %w", err)
	}
	if err := ms.AddProbesList([]uint{1}); err != nil {
		return nil, err
	}
	return ms.GetApiJson()
}

// ValidateMsmSpec builds the goat measurement spec locally and calls GetApiJson
// to check for structural errors without making any API call. Use this during
// plan to surface spec problems before apply.
func ValidateMsmSpec(spec plan.MsmSpec) error {
	_, err := specJSON(spec)
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

	desc := c.TagCodec.Format(spec.Key.Name, spec.Key.Cohort)
	ms, err := newGoatSpec(desc, spec, []string{c.TagCodec.Namespace()})
	if err != nil {
		return 0, fmt.Errorf("building measurement definition: %w", err)
	}
	ms.ApiKey(c.apiKey)
	ms.Verbose(c.Verbose)

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
