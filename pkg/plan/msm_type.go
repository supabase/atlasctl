package plan

// MsmType is one of the RIPE Atlas measurement types supported by atlasctl.
type MsmType string

const (
	MsmTypeDNS        MsmType = "dns"
	MsmTypePing       MsmType = "ping"
	MsmTypeTLS        MsmType = "tls"
	MsmTypeTraceroute MsmType = "traceroute"
)
