package atlasapi

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/supabase/atlasctl/pkg/config"
	"github.com/supabase/atlasctl/pkg/plan"
)

// requestBody returns the decoded request JSON for spec, plus its first definition.
func requestBody(t *testing.T, spec plan.MsmSpec) (body, def map[string]any) {
	t.Helper()
	raw, err := specJSON(spec)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(raw, &body))
	defs, ok := body["definitions"].([]any)
	require.True(t, ok)
	require.Len(t, defs, 1)
	return body, defs[0].(map[string]any)
}

func baseSpec(typ plan.MsmType, target string) plan.MsmSpec {
	return plan.MsmSpec{
		Key: plan.MsmKey{Name: "m", Cohort: "c"}, Target: target, Type: typ, AF: 4, Interval: 60,
	}
}

func TestSpecJSON_Defaults(t *testing.T) {
	t.Run("dns keeps the probe resolver, type A, and puts the target in query_argument", func(t *testing.T) {
		_, def := requestBody(t, baseSpec(plan.MsmTypeDNS, "example.com"))
		assert.Equal(t, "dns", def["type"])
		assert.Equal(t, "A", def["query_type"])
		assert.Equal(t, "IN", def["query_class"])
		assert.Equal(t, "UDP", def["protocol"])
		assert.Equal(t, "example.com", def["query_argument"])
		assert.Equal(t, true, def["use_probe_resolver"])
		assert.NotContains(t, def, "target")
	})

	t.Run("traceroute keeps goat's UDP default", func(t *testing.T) {
		_, def := requestBody(t, baseSpec(plan.MsmTypeTraceroute, "1.2.3.4"))
		assert.Equal(t, "UDP", def["protocol"])
	})

	t.Run("tls defaults to 443 with no hostname", func(t *testing.T) {
		_, def := requestBody(t, baseSpec(plan.MsmTypeTLS, "example.com"))
		assert.Equal(t, "sslcert", def["type"])
		assert.EqualValues(t, 443, def["port"])
		assert.NotContains(t, def, "hostname")
	})

	t.Run("unset options are absent from the request", func(t *testing.T) {
		for _, typ := range []plan.MsmType{
			plan.MsmTypeDNS, plan.MsmTypePing, plan.MsmTypeTLS, plan.MsmTypeTraceroute, plan.MsmTypeHTTP,
		} {
			body, def := requestBody(t, baseSpec(typ, "example.com"))
			for _, k := range []string{
				"resolve_on_probe", "spread", "target_update_hours", "auto_topup", "packets",
				"packet_interval", "include_probe_id", "response_timeout", "retry", "timeout",
				"query_string", "header_bytes", "extended_timing",
			} {
				assert.NotContains(t, def, k, "%s: %s", typ, k)
			}
			for _, k := range []string{"start_time", "stop_time", "bill_to"} {
				assert.NotContains(t, body, k, "%s: %s", typ, k)
			}
		}
	})

	t.Run("unsupported type", func(t *testing.T) {
		_, err := specJSON(baseSpec("ntp", "example.com"))
		assert.ErrorContains(t, err, "unsupported measurement type")
	})
}

func TestSpecJSON_Options(t *testing.T) {
	t.Run("common", func(t *testing.T) {
		spec := baseSpec(plan.MsmTypePing, "1.2.3.4")
		spec.Options.Common = config.CommonOptions{
			ResolveOnProbe: true, Spread: 30, SkipDNSCheck: true, TargetUpdateHours: 6,
			AutoTopup: true, AutoTopupDaysOff: 3, AutoTopupSimilarity: 0.8,
			StartTime: "2030-01-01T00:00:00Z", StopTime: "2030-02-01T00:00:00Z", BillTo: "other@example.com",
		}
		body, def := requestBody(t, spec)
		assert.Equal(t, true, def["resolve_on_probe"])
		assert.EqualValues(t, 30, def["spread"])
		assert.Equal(t, true, def["skip_dns_check"])
		assert.EqualValues(t, 6, def["target_update_hours"])
		assert.Equal(t, true, def["auto_topup"])
		assert.EqualValues(t, 3, def["auto_topup_prb_days_off"])
		assert.EqualValues(t, 0.8, def["auto_topup_prb_similarity"])
		assert.EqualValues(t, 60, def["interval"])
		assert.Contains(t, body, "start_time")
		assert.Contains(t, body, "stop_time")
		assert.Equal(t, "other@example.com", body["bill_to"])
	})

	t.Run("ping", func(t *testing.T) {
		spec := baseSpec(plan.MsmTypePing, "1.2.3.4")
		spec.Options.Ping = config.PingOptions{Packets: 5, PacketInterval: 500, IncludeProbeID: true}
		_, def := requestBody(t, spec)
		assert.EqualValues(t, 5, def["packets"])
		assert.EqualValues(t, 500, def["packet_interval"])
		assert.Equal(t, true, def["include_probe_id"])
	})

	t.Run("traceroute", func(t *testing.T) {
		spec := baseSpec(plan.MsmTypeTraceroute, "1.2.3.4")
		spec.Options.Traceroute = config.TraceOptions{
			Protocol: "TCP", ResponseTimeout: 2000, Packets: 2, Paris: 8, FirstHop: 2, MaxHops: 20,
			DestinationOptSize: 8, HopByHopOptSize: 16, DontFragment: true,
		}
		_, def := requestBody(t, spec)
		assert.Equal(t, "TCP", def["protocol"])
		assert.EqualValues(t, 2000, def["response_timeout"])
		assert.EqualValues(t, 2, def["packets"])
		assert.EqualValues(t, 8, def["paris"])
		assert.EqualValues(t, 2, def["first_hop"])
		assert.EqualValues(t, 20, def["max_hops"])
		assert.EqualValues(t, 8, def["destination_option_size"])
		assert.EqualValues(t, 16, def["hop_by_hop_option_size"])
		assert.Equal(t, true, def["dont_fragment"])
	})

	t.Run("dns", func(t *testing.T) {
		spec := baseSpec(plan.MsmTypeDNS, "example.com")
		spec.Options.DNS = config.DNSOptions{
			QueryType: "AAAA", QueryClass: "CHAOS", Protocol: "TCP", UseMacros: true, SetNSIDBit: true,
			IncludeQbuf: true, PrependProbeID: true, SetRDBit: true, SetDOBit: true,
			UDPPayloadSize: 1232, Retry: 2, Timeout: 3000,
		}
		_, def := requestBody(t, spec)
		assert.Equal(t, "AAAA", def["query_type"], "query_type must reach the API, not a hardcoded A")
		assert.Equal(t, "CHAOS", def["query_class"])
		assert.Equal(t, "TCP", def["protocol"])
		assert.Equal(t, true, def["use_macros"])
		assert.Equal(t, true, def["set_nsid_bit"])
		assert.Equal(t, true, def["include_qbuf"])
		assert.Equal(t, true, def["prepend_probe_id"])
		assert.Equal(t, true, def["set_rd_bit"])
		assert.Equal(t, true, def["set_do_bit"])
		assert.EqualValues(t, 1232, def["udp_payload_size"])
		assert.EqualValues(t, 2, def["retry"])
		assert.EqualValues(t, 3000, def["timeout"])
		assert.Equal(t, true, def["use_probe_resolver"])
	})

	t.Run("tls hostname and port", func(t *testing.T) {
		spec := baseSpec(plan.MsmTypeTLS, "1.2.3.4")
		spec.Options.TLS = config.TLSOptions{Port: 8443, Hostname: "api.example.com"}
		_, def := requestBody(t, spec)
		assert.EqualValues(t, 8443, def["port"])
		assert.Equal(t, "api.example.com", def["hostname"])
	})

	t.Run("http extras with and without the flat fields", func(t *testing.T) {
		extras := config.HTTPOptions{QueryString: "a=b", HeaderBytes: 64, ExtendedTiming: true, MoreExtendedTiming: true}

		spec := baseSpec(plan.MsmTypeHTTP, "example.com")
		spec.Options.HTTP = extras
		_, def := requestBody(t, spec) // only extras set: must not be dropped
		assert.Equal(t, "a=b", def["query_string"])
		assert.EqualValues(t, 64, def["header_bytes"])
		assert.Equal(t, true, def["extended_timing"])
		assert.Equal(t, true, def["more_extended_timing"])
		assert.Equal(t, "HEAD", def["method"])

		spec.HttpMethod, spec.HttpPath, spec.HttpPort, spec.HttpVersion = "GET", "/health", 8080, "1.1"
		_, def = requestBody(t, spec)
		assert.Equal(t, "GET", def["method"])
		assert.Equal(t, "/health", def["path"])
		assert.EqualValues(t, 8080, def["port"])
		assert.Equal(t, "1.1", def["version"])
		assert.Equal(t, "a=b", def["query_string"])
	})
}

func TestSpecJSON_BadTime(t *testing.T) {
	spec := baseSpec(plan.MsmTypePing, "1.2.3.4")
	spec.Options.Common.StartTime = "tomorrow"
	_, err := specJSON(spec)
	assert.ErrorContains(t, err, "start_time")
}
