package config

// CreditsPerResult returns the number of RIPE Atlas credits consumed per probe
// per measurement result for the given measurement type.
//
// Costs are fixed by the RIPE Atlas platform:
//
//	DNS        10 credits / result
//	TLS        10 credits / result
//	Ping        3 credits / result
//	Traceroute 30 credits / result
//
// One-off (non-periodic) measurements cost 2× the periodic rate; that factor
// is applied by the caller if needed.
func CreditsPerResult(t MeasurementType) int {
	switch t {
	case TypeDNS:
		return 10
	case TypeTLS:
		return 10
	case TypePing:
		return 3
	case TypeTraceroute:
		return 30
	case TypeHTTP:
		return 10
	default:
		return 0
	}
}
