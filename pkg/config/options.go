package config

import (
	"fmt"
	"time"
)

// MsmOptions are the optional RIPE Atlas measurement fields that atlasctl can
// set beyond target, address family and interval. A zero value means "not set":
// the field is omitted from the API request and the API default applies.
//
// MsmOptions is comparable with ==, and is also stored in the state file, so
// that a change to any option is detected as a structural change.
//
// Field doc comments are kept in step with docs/selection-reference.md.
type MsmOptions struct {
	// Common options apply to every measurement type.
	Common CommonOptions `yaml:"common,omitempty"`
	// Ping options are valid only on ping measurements.
	Ping PingOptions `yaml:"ping,omitempty"`
	// Traceroute options are valid only on traceroute measurements.
	Traceroute TraceOptions `yaml:"traceroute,omitempty"`
	// DNS options are valid only on dns measurements.
	DNS DNSOptions `yaml:"dns,omitempty"`
	// TLS options are valid only on tls measurements.
	TLS TLSOptions `yaml:"tls,omitempty"`
	// HTTP options are valid only on http measurements.
	HTTP HTTPOptions `yaml:"http,omitempty"`
}

// CommonOptions apply to every measurement type.
type CommonOptions struct {
	// ResolveOnProbe resolves the target on each probe instead of once centrally.
	ResolveOnProbe bool `yaml:"resolve_on_probe,omitempty"`
	// Spread is the number of seconds over which probe start times are spread
	// within one interval.
	Spread uint `yaml:"spread,omitempty"`
	// SkipDNSCheck skips the platform's DNS check of the target at creation time.
	SkipDNSCheck bool `yaml:"skip_dns_check,omitempty"`
	// TargetUpdateHours re-resolves the target every N hours.
	TargetUpdateHours uint `yaml:"target_update_hours,omitempty"`
	// AutoTopup replaces probes that go offline. Implied by AutoTopupDaysOff
	// and AutoTopupSimilarity.
	AutoTopup bool `yaml:"auto_topup,omitempty"`
	// AutoTopupDaysOff is the number of days a probe is offline before it is replaced.
	AutoTopupDaysOff uint `yaml:"auto_topup_prb_days_off,omitempty"`
	// AutoTopupSimilarity is how similar a replacement probe must be, between 0 and 1.
	AutoTopupSimilarity float64 `yaml:"auto_topup_prb_similarity,omitempty"`
	// StartTime is the measurement start time, RFC 3339.
	StartTime string `yaml:"start_time,omitempty"`
	// StopTime is the measurement stop time, RFC 3339. Must be after StartTime.
	StopTime string `yaml:"stop_time,omitempty"`
	// BillTo charges credits to another RIPE Atlas account instead of your own.
	// That account must have added you to its allowed billing list.
	BillTo string `yaml:"bill_to,omitempty"`
}

// PingOptions are ping-only fields.
type PingOptions struct {
	// Packets is the number of packets per result. Platform default 3.
	Packets uint `yaml:"packets,omitempty"`
	// PacketInterval is the number of milliseconds between the packets of one result.
	PacketInterval uint `yaml:"packet_interval,omitempty"`
	// IncludeProbeID puts the probe ID (as ASCII digits) in the packet payload.
	IncludeProbeID bool `yaml:"include_probe_id,omitempty"`
}

// TraceOptions are traceroute-only fields.
type TraceOptions struct {
	// Protocol is ICMP, UDP or TCP. atlasctl defaults to UDP; the platform
	// default is ICMP.
	Protocol string `yaml:"protocol,omitempty"`
	// ResponseTimeout is the number of milliseconds to wait for a reply to each
	// packet. Platform default 4000.
	ResponseTimeout uint `yaml:"response_timeout,omitempty"`
	// Packets is the number of packets sent per hop. Platform default 3.
	Packets uint `yaml:"packets,omitempty"`
	// Paris is the number of distinct flow identifiers to cycle through, so that
	// load balanced paths are explored consistently (Paris traceroute). Platform
	// default 16.
	Paris uint `yaml:"paris,omitempty"`
	// FirstHop is the TTL of the first hop probed. Platform default 1.
	FirstHop uint `yaml:"first_hop,omitempty"`
	// MaxHops is the TTL of the last hop probed. Platform default 32.
	MaxHops uint `yaml:"max_hops,omitempty"`
	// DestinationOptSize is the size in bytes of an IPv6 destination options
	// extension header added to each packet. Platform default 0 (none).
	DestinationOptSize uint `yaml:"destination_option_size,omitempty"`
	// HopByHopOptSize is the size in bytes of an IPv6 hop by hop options
	// extension header added to each packet. Platform default 0 (none).
	HopByHopOptSize uint `yaml:"hop_by_hop_option_size,omitempty"`
	// DontFragment sets the don't fragment bit on outgoing packets. Platform
	// default false.
	DontFragment bool `yaml:"dont_fragment,omitempty"`
}

// DNSOptions are DNS-only fields. The query argument is the measurement target
// and the probe's own resolver is always used.
type DNSOptions struct {
	// QueryType is the record type: A (default), AAAA, NS, MX, TXT, and so on.
	// Takes priority over the flat query_type key; the two must agree if both
	// are set.
	QueryType string `yaml:"query_type,omitempty"`
	// QueryClass is IN (default) or CHAOS.
	QueryClass string `yaml:"query_class,omitempty"`
	// Protocol is UDP (default) or TCP.
	Protocol string `yaml:"protocol,omitempty"`
	// UseMacros expands $p (probe ID), $r (random string) and $t (timestamp) in
	// the target, for example to build unique names that bypass resolver caches.
	UseMacros bool `yaml:"use_macros,omitempty"`
	// SetNSIDBit asks the server to identify itself with the EDNS NSID option
	// (RFC 5001), commonly used to see which anycast instance answered.
	SetNSIDBit bool `yaml:"set_nsid_bit,omitempty"`
	// IncludeQbuf includes the query packet sent by the probe in each result.
	IncludeQbuf bool `yaml:"include_qbuf,omitempty"`
	// PrependProbeID prefixes the queried name with the probe ID, giving a
	// unique name per probe.
	PrependProbeID bool `yaml:"prepend_probe_id,omitempty"`
	// SetRDBit sets the recursion desired bit.
	SetRDBit bool `yaml:"set_rd_bit,omitempty"`
	// SetDOBit sets the DNSSEC OK bit.
	SetDOBit bool `yaml:"set_do_bit,omitempty"`
	// UDPPayloadSize is the maximum UDP response size in bytes advertised
	// through EDNS0. Platform default 512.
	UDPPayloadSize uint `yaml:"udp_payload_size,omitempty"`
	// Retry is the number of times a query is retried after a failure. Platform
	// default 0.
	Retry uint `yaml:"retry,omitempty"`
	// Timeout is the number of milliseconds to wait for a response before the
	// query counts as failed. Platform default 5000.
	Timeout uint `yaml:"timeout,omitempty"`
}

// TLSOptions are TLS (sslcert) only fields.
type TLSOptions struct {
	// Port is the TCP port. Platform default 443.
	Port uint `yaml:"port,omitempty"`
	// Hostname is the server name sent as SNI. Useful when the target is an IP
	// address.
	Hostname string `yaml:"hostname,omitempty"`
}

// HTTPOptions are HTTP fields in addition to the flat http_* keys on Measurement.
type HTTPOptions struct {
	// QueryString is appended to the request path (without the leading "?").
	QueryString string `yaml:"query_string,omitempty"`
	// HeaderBytes is the maximum number of response header bytes to record in
	// the result.
	HeaderBytes uint `yaml:"header_bytes,omitempty"`
	// ExtendedTiming reports additional timing detail in each result (for
	// example time to resolve and time to first byte).
	ExtendedTiming bool `yaml:"extended_timing,omitempty"`
	// MoreExtendedTiming reports even more timing detail than ExtendedTiming.
	MoreExtendedTiming bool `yaml:"more_extended_timing,omitempty"`
}

var (
	validTraceProtocols = []string{"ICMP", "UDP", "TCP"}
	validDNSProtocols   = []string{"UDP", "TCP"}
	validDNSClasses     = []string{"IN", "CHAOS"}
	validDNSTypes       = []string{"A", "AAAA", "ANY", "CNAME", "DNSKEY", "DS", "MX", "NS", "NSEC", "PTR", "RRSIG", "SOA", "TXT", "SRV", "NAPTR", "TLSA"}
)

func oneOf(v string, allowed []string) bool {
	for _, a := range allowed {
		if v == a {
			return true
		}
	}
	return false
}

// normalizeOptions folds the legacy flat query_type key into dns.query_type
// (DNS measurements only; other types ignored it before and still do) and
// rewrites start_time / stop_time as UTC RFC 3339 so that equivalent values
// compare equal. Unparseable times are left for validate to report.
func (m *Measurement) normalizeOptions() {
	if m.Type == TypeDNS && m.DNS.QueryType == "" {
		m.DNS.QueryType = m.QueryType
	}
	c := &m.Common
	for _, ts := range []*string{&c.StartTime, &c.StopTime} {
		if t, err := time.Parse(time.RFC3339, *ts); err == nil {
			*ts = t.UTC().Format(time.RFC3339)
		}
	}
}

// validateOptions checks the per-type option blocks of m.
func (m Measurement) validateOptions() []error {
	var errs []error
	bad := func(format string, args ...any) {
		errs = append(errs, fmt.Errorf("measurement %q: "+format, append([]any{m.Name}, args...)...))
	}

	// A block for a different type is almost certainly a mistake: reject it
	// rather than silently ignore it.
	wrongType := func(block string, set bool, want MeasurementType) {
		if set && m.Type != want {
			bad("%s options are only valid for type %q, got %q", block, want, m.Type)
		}
	}
	wrongType("ping", m.Ping != PingOptions{}, TypePing)
	wrongType("traceroute", m.Traceroute != TraceOptions{}, TypeTraceroute)
	wrongType("dns", m.DNS != DNSOptions{}, TypeDNS)
	wrongType("tls", m.TLS != TLSOptions{}, TypeTLS)
	wrongType("http", m.HTTP != HTTPOptions{}, TypeHTTP)

	if m.Type == TypeDNS && m.QueryType != "" && m.DNS.QueryType != m.QueryType {
		bad("query_type %q conflicts with dns.query_type %q", m.QueryType, m.DNS.QueryType)
	}
	if p := m.Traceroute.Protocol; p != "" && !oneOf(p, validTraceProtocols) {
		bad("traceroute.protocol %q must be ICMP, UDP, or TCP", p)
	}
	if p := m.DNS.Protocol; p != "" && !oneOf(p, validDNSProtocols) {
		bad("dns.protocol %q must be UDP or TCP", p)
	}
	if c := m.DNS.QueryClass; c != "" && !oneOf(c, validDNSClasses) {
		bad("dns.query_class %q must be IN or CHAOS", c)
	}
	if t := m.DNS.QueryType; t != "" && !oneOf(t, validDNSTypes) {
		bad("dns.query_type %q is not a supported DNS record type", t)
	}

	c := m.Common
	if s := c.AutoTopupSimilarity; s < 0 || s > 1 {
		bad("common.auto_topup_prb_similarity must be between 0 and 1, got %v", s)
	}
	var start, stop time.Time
	for _, f := range []struct {
		key string
		val string
		out *time.Time
	}{{"start_time", c.StartTime, &start}, {"stop_time", c.StopTime, &stop}} {
		if f.val == "" {
			continue
		}
		t, err := time.Parse(time.RFC3339, f.val)
		if err != nil {
			bad("common.%s %q must be RFC 3339 (e.g. 2026-12-01T00:00:00Z)", f.key, f.val)
			continue
		}
		*f.out = t
	}
	if !start.IsZero() && !stop.IsZero() && !stop.After(start) {
		bad("common.stop_time must be after common.start_time")
	}
	return errs
}
