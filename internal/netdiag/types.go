package netdiag

import (
	"time"
)

// ProbeType defines the specific diagnostic probe kind.
type ProbeType string

const (
	ProbeTypePing       ProbeType = "ping"
	ProbeTypeTraceroute ProbeType = "traceroute"
	ProbeTypePort       ProbeType = "port_probe"
	ProbeTypeDNS        ProbeType = "dns_resolve"
)

// ProbeRequest specifies parameters for a network diagnostic run.
type ProbeRequest struct {
	SourceHostID  int64     `json:"source_host_id"`
	Type          ProbeType `json:"type"`
	TargetType    string    `json:"target_type"` // "host" or "custom"
	TargetHostID  int64     `json:"target_host_id,omitempty"`
	TargetAddress string    `json:"target_address"`
	Port          int       `json:"port,omitempty"` // For port_probe
}

// HopInfo represents an individual hop in a traceroute path.
type HopInfo struct {
	Hop    int     `json:"hop"`
	Host   string  `json:"host"`
	IP     string  `json:"ip"`
	RTTMs  float64 `json:"rtt_ms"`
	Zone   string  `json:"zone"`   // "private", "transit", "destination", "timeout"
	Status string  `json:"status"` // "ok", "warning", "timeout"
}

// HopAnalysis represents the automated heuristic diagnosis of a traceroute run.
type HopAnalysis struct {
	Summary          string    `json:"summary"`
	SummaryID        string    `json:"summary_id"` // Indonesian translation
	Status           string    `json:"status"`     // "optimal", "degraded", "failed"
	TotalHops        int       `json:"total_hops"`
	FinalRTTMs       float64   `json:"final_rtt_ms"`
	DestinationFound bool      `json:"destination_found"`
	DropAtHop        int       `json:"drop_at_hop,omitempty"`
	LatencySpikeHop  int       `json:"latency_spike_hop,omitempty"`
	GeneratedAt      time.Time `json:"generated_at"`
}

// PingStats captures parsed ICMP ping statistics.
type PingStats struct {
	PacketsTransmitted int     `json:"packets_transmitted"`
	PacketsReceived    int     `json:"packets_received"`
	PacketLossPct      float64 `json:"packet_loss_pct"`
	MinMs              float64 `json:"min_ms"`
	AvgMs              float64 `json:"avg_ms"`
	MaxMs              float64 `json:"max_ms"`
	Summary            string  `json:"summary"`
	SummaryID          string  `json:"summary_id"`
	Status             string  `json:"status"` // "healthy", "degraded", "down"
}

// PortStats captures TCP port reachability status.
type PortStats struct {
	Port      int    `json:"port"`
	Open      bool   `json:"open"`
	Summary   string `json:"summary"`
	SummaryID string `json:"summary_id"`
	Status    string `json:"status"` // "healthy", "down"
}

// DNSStats captures DNS resolution results.
type DNSStats struct {
	Target      string   `json:"target"`
	ResolvedIPs []string `json:"resolved_ips"`
	Summary     string   `json:"summary"`
	SummaryID   string   `json:"summary_id"`
	Status      string   `json:"status"` // "healthy", "down"
}

// ProbeEvent represents an SSE event streamed to the browser.
type ProbeEvent struct {
	Type     string       `json:"type"` // "progress", "raw", "hop", "ping_stats", "port_stats", "dns_stats", "hop_analysis", "error", "done"
	RawLine  string       `json:"raw_line,omitempty"`
	Hop      *HopInfo     `json:"hop,omitempty"`
	Ping     *PingStats   `json:"ping,omitempty"`
	Port     *PortStats   `json:"port,omitempty"`
	DNS      *DNSStats    `json:"dns,omitempty"`
	Analysis *HopAnalysis `json:"analysis,omitempty"`
	Message  string       `json:"message,omitempty"`
}

// TriageVerdict defines the canonical outcome classification of a triage matrix run.
type TriageVerdict string

const (
	VerdictHostSaturated       TriageVerdict = "HOST_SATURATED"
	VerdictAppDegraded         TriageVerdict = "APPLICATION_DEGRADED"
	VerdictTransitPathDegraded TriageVerdict = "TRANSIT_PATH_DEGRADED"
	VerdictBranchGatewayDown   TriageVerdict = "BRANCH_GATEWAY_DOWN"
	VerdictMTUBlackhole        TriageVerdict = "MTU_BLACKHOLE_DETECTED"
	VerdictICMPFilteredTCP     TriageVerdict = "ICMP_FILTERED_TCP_HEALTHY"
	VerdictHealthy             TriageVerdict = "HEALTHY"
)

// L7ProbeStats captures HTTP L7 application response metrics.
type L7ProbeStats struct {
	Endpoint  string  `json:"endpoint"`
	HTTPCode  int     `json:"http_code"`
	DNSMs     float64 `json:"dns_ms"`
	ConnectMs float64 `json:"connect_ms"`
	TTFBMs    float64 `json:"ttfb_ms"`
	TotalMs   float64 `json:"total_ms"`
	Vantage   string  `json:"vantage"` // "local_loopback" or "master_remote"
	Status    string  `json:"status"`  // "healthy", "degraded", "down"
	Summary   string  `json:"summary"`
	SummaryID string  `json:"summary_id"`
}

// MTUProbeStats captures Path MTU sweep verification results.
type MTUProbeStats struct {
	BaselineMTU       int    `json:"baseline_mtu"`
	TestedSizes       []int  `json:"tested_sizes"`
	PassedSizes       []int  `json:"passed_sizes"`
	BlackholeDetected bool   `json:"blackhole_detected"`
	Status            string `json:"status"` // "healthy", "warning", "blackhole"
	Summary           string `json:"summary"`
	SummaryID         string `json:"summary_id"`
}

// RouteInfo represents destination route discovery details.
type RouteInfo struct {
	TargetAddress string `json:"target_address"`
	Interface     string `json:"interface"`
	Gateway       string `json:"gateway,omitempty"`
	MTU           int    `json:"mtu,omitempty"`
	IsPrivate     bool   `json:"is_private"`
	Summary       string `json:"summary"`
}

// HostResourceStats captures local compute and socket health.
type HostResourceStats struct {
	CPULoad           float64 `json:"cpu_load"`
	RAMUsedPct        float64 `json:"ram_used_pct"`
	SwapUsedPct       float64 `json:"swap_used_pct"`
	ActiveConnections int     `json:"active_connections"`
	Status            string  `json:"status"` // "healthy", "saturated"
	Summary           string  `json:"summary"`
	SummaryID         string  `json:"summary_id"`
}

// TriageRequest specifies parameters to execute a triage matrix run.
type TriageRequest struct {
	SourceHostID  int64   `json:"source_host_id"`
	PresetID      int64   `json:"preset_id,omitempty"`
	TargetAddress string  `json:"target_address"`
	TargetName    string  `json:"target_name,omitempty"`
	AppEndpoint   string  `json:"app_endpoint,omitempty"` // e.g. "/" or ":8080/health"
	BaselineMTU   int     `json:"baseline_mtu,omitempty"`
	MaxLatencyMs  float64 `json:"max_latency_ms,omitempty"`
	MaxLossPct    float64 `json:"max_loss_pct,omitempty"`
	DBPort        int     `json:"db_port,omitempty"` // Optional auxiliary service/DB port on localhost (e.g. 3306, 5432)
}

// TriageResult represents the comprehensive triage outcome.
type TriageResult struct {
	SourceHostName string             `json:"source_host_name"`
	TargetName     string             `json:"target_name"`
	TargetAddress  string             `json:"target_address"`
	ResolvedIP     string             `json:"resolved_ip,omitempty"`
	Verdict        TriageVerdict      `json:"verdict"`
	Summary        string             `json:"summary"`
	SummaryID      string             `json:"summary_id"`
	HostResource   *HostResourceStats `json:"host_resource"`
	L7Local        *L7ProbeStats      `json:"l7_local,omitempty"`
	L7Master       *L7ProbeStats      `json:"l7_master,omitempty"`
	DBPort         *PortStats         `json:"db_port,omitempty"`
	Route          *RouteInfo         `json:"route,omitempty"`
	Ping           *PingStats         `json:"ping,omitempty"`
	MTU            *MTUProbeStats     `json:"mtu,omitempty"`
	TCPPort        *PortStats         `json:"tcp_port,omitempty"`
	HopAnalysis    *HopAnalysis       `json:"hop_analysis,omitempty"`
	TicketSnippet  string             `json:"ticket_snippet"`
	DurationMs     int64              `json:"duration_ms"`
	ExecutedAt     time.Time          `json:"executed_at"`
}

// TriageEvent represents progress streamed via SSE during a triage run.
type TriageEvent struct {
	Type      string        `json:"type"`                 // "stage", "log", "result", "error", "done"
	Stage     string        `json:"stage,omitempty"`      // "host_check", "route_discovery", "l7_probe", "path_probe", "correlate"
	Message   string        `json:"message,omitempty"`    // English message
	MessageID string        `json:"message_id,omitempty"` // Indonesian message
	Result    *TriageResult `json:"result,omitempty"`
}
