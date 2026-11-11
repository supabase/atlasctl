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
type MsmOptions struct {
	Common     CommonOptions `yaml:"common,omitempty"`
	Ping       PingOptions   `yaml:"ping,omitempty"`
	Traceroute TraceOptions  `yaml:"traceroute,omitempty"`
	DNS        DNSOptions    `yaml:"dns,omitempty"`
	TLS        TLSOptions    `yaml:"tls,omitempty"`
	HTTP       HTTPOptions   `yaml:"http,omitempty"`
}

// CommonOptions apply to every measurement type.
type CommonOptions struct {
	ResolveOnProbe      bool    `yaml:"resolve_on_probe,omitempty"`
	Spread              uint    `yaml:"spread,omitempty"` // seconds over which probe start times are spread
	SkipDNSCheck        bool    `yaml:"skip_dns_check,omitempty"`
	TargetUpdateHours   uint    `yaml:"target_update_hours,omitempty"` // re-resolve the target every N hours
	AutoTopup           bool    `yaml:"auto_topup,omitempty"`
	AutoTopupDaysOff    uint    `yaml:"auto_topup_prb_days_off,omitempty"`
	AutoTopupSimilarity float64 `yaml:"auto_topup_prb_similarity,omitempty"` // 0..1
	StartTime           string  `yaml:"start_time,omitempty"`                // RFC 3339
	StopTime            string  `yaml:"stop_time,omitempty"`                 // RFC 3339
	BillTo              string  `yaml:"bill_to,omitempty"`
}

// PingOptions are ping-only fields.
type PingOptions struct {
	Packets        uint `yaml:"packets,omitempty"`         // API default 3
	PacketInterval uint `yaml:"packet_interval,omitempty"` // ms between packets
	IncludeProbeID bool `yaml:"include_probe_id,omitempty"`
}

// TraceOptions are traceroute-only fields.
type TraceOptions struct {
	Protocol           string `yaml:"protocol,omitempty"` // ICMP, UDP (atlasctl default), TCP
	ResponseTimeout    uint   `yaml:"response_timeout,omitempty"`
	Packets            uint   `yaml:"packets,omitempty"`
	Paris              uint   `yaml:"paris,omitempty"`
	FirstHop           uint   `yaml:"first_hop,omitempty"`
	MaxHops            uint   `yaml:"max_hops,omitempty"`
	DestinationOptSize uint   `yaml:"destination_option_size,omitempty"`
	HopByHopOptSize    uint   `yaml:"hop_by_hop_option_size,omitempty"`
	DontFragment       bool   `yaml:"dont_fragment,omitempty"`
}

// DNSOptions are DNS-only fields. The query argument is the measurement target
// and the probe's own resolver is always used.
type DNSOptions struct {
	QueryType      string `yaml:"query_type,omitempty"`  // A (default), AAAA, NS, MX, ...
	QueryClass     string `yaml:"query_class,omitempty"` // IN (default), CHAOS
	Protocol       string `yaml:"protocol,omitempty"`    // UDP (default), TCP
	UseMacros      bool   `yaml:"use_macros,omitempty"`
	SetNSIDBit     bool   `yaml:"set_nsid_bit,omitempty"`
	IncludeQbuf    bool   `yaml:"include_qbuf,omitempty"`
	PrependProbeID bool   `yaml:"prepend_probe_id,omitempty"`
	SetRDBit       bool   `yaml:"set_rd_bit,omitempty"`
	SetDOBit       bool   `yaml:"set_do_bit,omitempty"`
	UDPPayloadSize uint   `yaml:"udp_payload_size,omitempty"`
	Retry          uint   `yaml:"retry,omitempty"`
	Timeout        uint   `yaml:"timeout,omitempty"` // ms
}

// TLSOptions are TLS (sslcert) only fields.
type TLSOptions struct {
	Port     uint   `yaml:"port,omitempty"`     // API default 443
	Hostname string `yaml:"hostname,omitempty"` // SNI
}

// HTTPOptions are HTTP fields in addition to the flat http_* keys on Measurement.
type HTTPOptions struct {
	QueryString        string `yaml:"query_string,omitempty"`
	HeaderBytes        uint   `yaml:"header_bytes,omitempty"`
	ExtendedTiming     bool   `yaml:"extended_timing,omitempty"`
	MoreExtendedTiming bool   `yaml:"more_extended_timing,omitempty"`
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
