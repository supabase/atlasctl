package config_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/supabase/atlasctl/pkg/config"
)

// optsYAML renders a one-measurement config with the given type and extra
// measurement-level YAML (already indented by 4 spaces).
func optsYAML(typ, extra string) string {
	return `
measurements:
  - name: m
    type: ` + typ + `
    target: example.com
` + extra + `
    cohorts:
      - name: c
        probe_count: 10
        max_probes_per_cell: 1
        interval_seconds: 60
`
}

func TestLoad_Options(t *testing.T) {
	t.Run("all blocks parse", func(t *testing.T) {
		tests := []struct {
			typ, extra string
			check      func(t *testing.T, o config.MsmOptions)
		}{
			{"ping", `    common:
      resolve_on_probe: true
      spread: 30
      skip_dns_check: true
      target_update_hours: 6
      auto_topup: true
      auto_topup_prb_days_off: 3
      auto_topup_prb_similarity: 0.8
      bill_to: other@example.com
    ping:
      packets: 5
      packet_interval: 500
      include_probe_id: true`, func(t *testing.T, o config.MsmOptions) {
				assert.Equal(t, config.CommonOptions{
					ResolveOnProbe: true, Spread: 30, SkipDNSCheck: true, TargetUpdateHours: 6,
					AutoTopup: true, AutoTopupDaysOff: 3, AutoTopupSimilarity: 0.8,
					BillTo: "other@example.com",
				}, o.Common)
				assert.Equal(t, config.PingOptions{Packets: 5, PacketInterval: 500, IncludeProbeID: true}, o.Ping)
			}},
			{"traceroute", `    traceroute:
      protocol: TCP
      response_timeout: 2000
      packets: 2
      paris: 8
      first_hop: 2
      max_hops: 20
      destination_option_size: 8
      hop_by_hop_option_size: 16
      dont_fragment: true`, func(t *testing.T, o config.MsmOptions) {
				assert.Equal(t, config.TraceOptions{
					Protocol: "TCP", ResponseTimeout: 2000, Packets: 2, Paris: 8, FirstHop: 2, MaxHops: 20,
					DestinationOptSize: 8, HopByHopOptSize: 16, DontFragment: true,
				}, o.Traceroute)
			}},
			{"dns", `    dns:
      query_type: AAAA
      query_class: CHAOS
      protocol: TCP
      use_macros: true
      set_nsid_bit: true
      include_qbuf: true
      prepend_probe_id: true
      set_rd_bit: true
      set_do_bit: true
      udp_payload_size: 1232
      retry: 2
      timeout: 3000`, func(t *testing.T, o config.MsmOptions) {
				assert.Equal(t, config.DNSOptions{
					QueryType: "AAAA", QueryClass: "CHAOS", Protocol: "TCP", UseMacros: true, SetNSIDBit: true,
					IncludeQbuf: true, PrependProbeID: true, SetRDBit: true, SetDOBit: true,
					UDPPayloadSize: 1232, Retry: 2, Timeout: 3000,
				}, o.DNS)
			}},
			{"tls", `    tls:
      port: 8443
      hostname: api.example.com`, func(t *testing.T, o config.MsmOptions) {
				assert.Equal(t, config.TLSOptions{Port: 8443, Hostname: "api.example.com"}, o.TLS)
			}},
			{"http", `    http:
      query_string: a=b
      header_bytes: 64
      extended_timing: true
      more_extended_timing: true`, func(t *testing.T, o config.MsmOptions) {
				assert.Equal(t, config.HTTPOptions{
					QueryString: "a=b", HeaderBytes: 64, ExtendedTiming: true, MoreExtendedTiming: true,
				}, o.HTTP)
			}},
		}
		for _, tc := range tests {
			t.Run(tc.typ, func(t *testing.T) {
				cfg, err := config.Load(writeTemp(t, optsYAML(tc.typ, tc.extra)))
				require.NoError(t, err)
				tc.check(t, cfg.Measurements[0].MsmOptions)
			})
		}
	})

	t.Run("no options is the zero value", func(t *testing.T) {
		cfg, err := config.Load(writeTemp(t, optsYAML("dns", "")))
		require.NoError(t, err)
		assert.Equal(t, config.MsmOptions{}, cfg.Measurements[0].MsmOptions)
	})

	t.Run("legacy query_type folds into dns.query_type", func(t *testing.T) {
		cfg, err := config.Load(writeTemp(t, optsYAML("dns", "    query_type: NS")))
		require.NoError(t, err)
		assert.Equal(t, "NS", cfg.Measurements[0].DNS.QueryType)
	})

	t.Run("legacy query_type on a non-dns measurement is still ignored", func(t *testing.T) {
		cfg, err := config.Load(writeTemp(t, optsYAML("ping", "    query_type: NS")))
		require.NoError(t, err)
		assert.Equal(t, config.MsmOptions{}, cfg.Measurements[0].MsmOptions)
	})

	t.Run("times are normalised to UTC", func(t *testing.T) {
		cfg, err := config.Load(writeTemp(t, optsYAML("ping", `    common:
      start_time: "2030-01-01T02:00:00+02:00"
      stop_time: "2030-02-01T00:00:00Z"`)))
		require.NoError(t, err)
		assert.Equal(t, "2030-01-01T00:00:00Z", cfg.Measurements[0].Common.StartTime)
		assert.Equal(t, "2030-02-01T00:00:00Z", cfg.Measurements[0].Common.StopTime)
	})
}

func TestLoad_OptionsInvalid(t *testing.T) {
	tests := []struct {
		name, typ, extra, errContains string
	}{
		{"ping block on dns", "dns", "    ping:\n      packets: 3", `ping options are only valid for type "ping"`},
		{"dns block on ping", "ping", "    dns:\n      retry: 1", `dns options are only valid for type "dns"`},
		{"traceroute block on tls", "tls", "    traceroute:\n      max_hops: 5", `traceroute options are only valid`},
		{"tls block on http", "http", "    tls:\n      port: 1", `tls options are only valid`},
		{"http block on dns", "dns", "    http:\n      header_bytes: 1", `http options are only valid`},
		{"bad traceroute protocol", "traceroute", "    traceroute:\n      protocol: SCTP", "traceroute.protocol"},
		{"bad dns protocol", "dns", "    dns:\n      protocol: QUIC", "dns.protocol"},
		{"bad dns class", "dns", "    dns:\n      query_class: HS", "dns.query_class"},
		{"bad dns type", "dns", "    dns:\n      query_type: BOGUS", "dns.query_type"},
		{"legacy and new query_type conflict", "dns", "    query_type: A\n    dns:\n      query_type: AAAA", "conflicts with dns.query_type"},
		{"similarity out of range", "ping", "    common:\n      auto_topup_prb_similarity: 1.5", "auto_topup_prb_similarity"},
		{"bad start_time", "ping", "    common:\n      start_time: tomorrow", "start_time"},
		{"stop before start", "ping", "    common:\n      start_time: \"2030-02-01T00:00:00Z\"\n      stop_time: \"2030-01-01T00:00:00Z\"", "stop_time must be after"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := config.Load(writeTemp(t, optsYAML(tc.typ, tc.extra)))
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.errContains)
		})
	}
}
