package netdiag

import (
	"fmt"
	"net"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var (
	// Tracepath regex: e.g. " 1:  192.168.1.1   1.234ms" or " 3:  no reply" or " 4:  1.1.1.1   12.345ms reached"
	tracepathHopRegex = regexp.MustCompile(`^\s*(\d+)(?:\?)?:\s+(?:\[[^\]]+\]\s+)?([^\s]+)(?:\s+([0-9.]+)\s*ms)?(.*)$`)

	// Traceroute regex: e.g. " 1  192.168.1.1  1.234 ms" or " 3  *" or " 2  gateway (10.0.0.1)  2.34 ms"
	tracerouteHopRegex = regexp.MustCompile(`^\s*(\d+)\s+([^\s]+)(?:\s+\(([0-9a-fA-F.:]+)\))?(?:\s+([0-9.]+)\s*ms|\s+\*|\s+no reply)?`)

	// Ping regexes
	pingStatsRegex = regexp.MustCompile(`(\d+)\s+packets transmitted,\s+(\d+)\s+(?:packets\s+)?received,.*?(?:([0-9.]+)%\s+packet loss)`)
	pingRTTRegex   = regexp.MustCompile(`(?:rtt|round-trip)\s+min/avg/max/(?:mdev|stddev)\s*=\s*([0-9.]+)/([0-9.]+)/([0-9.]+)/?([0-9.]*)?\s*ms`)

	// IP CIDRs for private/internal classification
	privateCIDRs []*net.IPNet
)

func init() {
	cidrs := []string{
		"10.0.0.0/8",
		"172.16.0.0/12",
		"192.168.0.0/16",
		"127.0.0.0/8",
		"100.64.0.0/10",   // CGNAT RFC 6598
		"169.254.0.0/16",  // Link Local
		"::1/128",         // IPv6 Loopback
		"fc00::/7",        // IPv6 Unique Local
		"fe80::/10",       // IPv6 Link Local
	}
	for _, cidr := range cidrs {
		_, ipNet, err := net.ParseCIDR(cidr)
		if err == nil {
			privateCIDRs = append(privateCIDRs, ipNet)
		}
	}
}

// ClassifyIPZone classifies an IP into private, transit, destination, or timeout.
func ClassifyIPZone(ipStr, target string) string {
	if ipStr == "" || ipStr == "*" || strings.EqualFold(ipStr, "no reply") {
		return "timeout"
	}
	cleanIP := strings.Trim(ipStr, "()")
	if target != "" && (cleanIP == target || strings.EqualFold(cleanIP, target)) {
		return "destination"
	}

	parsed := net.ParseIP(cleanIP)
	if parsed == nil {
		return "transit"
	}

	for _, block := range privateCIDRs {
		if block.Contains(parsed) {
			return "private"
		}
	}
	return "transit"
}

// ParseTracepathOrTracerouteLine parses a single line from tracepath or traceroute.
func ParseTracepathOrTracerouteLine(line string, target string) *HopInfo {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" || strings.HasPrefix(trimmed, "traceroute to") || strings.HasPrefix(trimmed, "Resume:") || strings.HasPrefix(trimmed, "Too many hops") {
		return nil
	}

	// 1. Try tracepath format: e.g. " 1:  192.168.1.1  1.234ms" or " 3:  no reply"
	if matches := tracepathHopRegex.FindStringSubmatch(line); len(matches) > 2 {
		hopNum, err := strconv.Atoi(matches[1])
		if err != nil || hopNum < 1 {
			return nil
		}
		rawHost := matches[2]
		rawRTT := matches[3]
		extra := matches[4]

		if rawHost == "no" || strings.EqualFold(rawHost, "no reply") || rawHost == "*" {
			return &HopInfo{
				Hop:    hopNum,
				Host:   "* * *",
				IP:     "",
				RTTMs:  0,
				Zone:   "timeout",
				Status: "timeout",
			}
		}

		var rtt float64
		if rawRTT != "" {
			rtt, _ = strconv.ParseFloat(rawRTT, 64)
		}

		zone := ClassifyIPZone(rawHost, target)
		if strings.Contains(strings.ToLower(extra), "reached") {
			zone = "destination"
		}

		status := "ok"
		if rtt > 100.0 {
			status = "warning"
		}

		return &HopInfo{
			Hop:    hopNum,
			Host:   rawHost,
			IP:     rawHost,
			RTTMs:  rtt,
			Zone:   zone,
			Status: status,
		}
	}

	// 2. Try traceroute format: e.g. " 1  192.168.1.1  1.234 ms" or " 2  gateway (10.0.0.1)  2.34 ms" or " 3  *"
	if matches := tracerouteHopRegex.FindStringSubmatch(line); len(matches) > 2 {
		hopNum, err := strconv.Atoi(matches[1])
		if err != nil || hopNum < 1 {
			return nil
		}
		rawHost := matches[2]
		ipInParens := matches[3]
		rawRTT := matches[4]

		if rawHost == "*" || strings.EqualFold(rawHost, "no reply") {
			return &HopInfo{
				Hop:    hopNum,
				Host:   "* * *",
				IP:     "",
				RTTMs:  0,
				Zone:   "timeout",
				Status: "timeout",
			}
		}

		effectiveIP := rawHost
		if ipInParens != "" {
			effectiveIP = ipInParens
		}

		var rtt float64
		if rawRTT != "" {
			rtt, _ = strconv.ParseFloat(rawRTT, 64)
		}

		zone := ClassifyIPZone(effectiveIP, target)
		status := "ok"
		if rtt > 100.0 {
			status = "warning"
		}

		return &HopInfo{
			Hop:    hopNum,
			Host:   rawHost,
			IP:     effectiveIP,
			RTTMs:  rtt,
			Zone:   zone,
			Status: status,
		}
	}

	return nil
}

// AnalyzeHops evaluates a sequence of hops and produces automated diagnostic insights.
// ponytail: Heuristic hop interpretation with latency jump and blackhole detection.
func AnalyzeHops(hops []*HopInfo, target string, targetReached bool) *HopAnalysis {
	analysis := &HopAnalysis{
		TotalHops:   len(hops),
		GeneratedAt: time.Now(),
		Status:      "optimal",
	}

	if len(hops) == 0 {
		analysis.Status = "failed"
		analysis.Summary = "No hops detected. Target might be unreachable or interface is down."
		analysis.SummaryID = "Tidak ada hop yang terdeteksi. Target tidak dapat dijangkau atau interface mati."
		return analysis
	}

	var lastReplyingHop *HopInfo
	var lastRespondingHopIndex = -1
	var maxRTT float64
	var maxDiff float64
	var spikeHop *HopInfo
	var reachedHop *HopInfo

	for i, h := range hops {
		if h.Zone == "destination" || targetReached || (target != "" && (h.IP == target || h.Host == target)) {
			reachedHop = h
			analysis.DestinationFound = true
		}

		if h.Status != "timeout" && h.RTTMs > 0 {
			if lastReplyingHop != nil {
				diff := h.RTTMs - lastReplyingHop.RTTMs
				if diff > 60.0 && diff > maxDiff {
					maxDiff = diff
					spikeHop = h
				}
			}
			lastReplyingHop = h
			lastRespondingHopIndex = i
			if h.RTTMs > maxRTT {
				maxRTT = h.RTTMs
			}
		}
	}

	if lastReplyingHop != nil {
		analysis.FinalRTTMs = lastReplyingHop.RTTMs
	}

	// 1. Check if destination was reached
	if reachedHop != nil || analysis.DestinationFound {
		analysis.DestinationFound = true
		finalMs := analysis.FinalRTTMs
		if reachedHop != nil && reachedHop.RTTMs > 0 {
			finalMs = reachedHop.RTTMs
		}

		if spikeHop != nil && maxDiff >= 70.0 {
			analysis.Status = "degraded"
			analysis.LatencySpikeHop = spikeHop.Hop
			analysis.Summary = fmt.Sprintf("Route reached destination (%d hops, final RTT %.1f ms), but latency spike (+%.1f ms) detected at Hop %d (%s).",
				len(hops), finalMs, maxDiff, spikeHop.Hop, spikeHop.Host)
			analysis.SummaryID = fmt.Sprintf("Rute mencapai tujuan (%d hop, latensi akhir %.1f ms), namun terdeteksi lonjakan latensi (+%.1f ms) pada Hop %d (%s).",
				len(hops), finalMs, maxDiff, spikeHop.Hop, spikeHop.Host)
		} else {
			analysis.Status = "optimal"
			analysis.Summary = fmt.Sprintf("Route healthy and optimal (Total %d hops, final RTT %.1f ms).",
				len(hops), finalMs)
			analysis.SummaryID = fmt.Sprintf("Rute sehat dan optimal (Total %d hop, latensi akhir %.1f ms).",
				len(hops), finalMs)
		}
		return analysis
	}

	// 2. Destination not reached: check if dropped midway
	if lastRespondingHopIndex >= 0 && lastRespondingHopIndex < len(hops)-1 {
		dropHop := hops[lastRespondingHopIndex]
		analysis.Status = "failed"
		analysis.DropAtHop = dropHop.Hop
		analysis.Summary = fmt.Sprintf("Traffic failed to reach destination; halted after Hop %d (%s) - likely upstream firewall drop or routing blackhole.",
			dropHop.Hop, dropHop.Host)
		analysis.SummaryID = fmt.Sprintf("Trafik tidak mencapai tujuan; terhenti setelah Hop %d (%s) - kemungkinan firewall upstream atau blackhole rute.",
			dropHop.Hop, dropHop.Host)
		return analysis
	}

	// 3. All timeouts or initial failure
	analysis.Status = "failed"
	analysis.Summary = fmt.Sprintf("Target did not respond (All hops timed out) - host offline or ICMP blocked.")
	analysis.SummaryID = fmt.Sprintf("Target tidak merespons (Semua hop timeout) - host offline atau ICMP diblokir.")
	return analysis
}

// ParsePingOutput parses the output of `ping -c 4 -W 2`.
func ParsePingOutput(output string) *PingStats {
	stats := &PingStats{
		Status: "down",
	}

	statsMatches := pingStatsRegex.FindStringSubmatch(output)
	if len(statsMatches) >= 4 {
		stats.PacketsTransmitted, _ = strconv.Atoi(statsMatches[1])
		stats.PacketsReceived, _ = strconv.Atoi(statsMatches[2])
		stats.PacketLossPct, _ = strconv.ParseFloat(statsMatches[3], 64)
	}

	rttMatches := pingRTTRegex.FindStringSubmatch(output)
	if len(rttMatches) >= 4 {
		stats.MinMs, _ = strconv.ParseFloat(rttMatches[1], 64)
		stats.AvgMs, _ = strconv.ParseFloat(rttMatches[2], 64)
		stats.MaxMs, _ = strconv.ParseFloat(rttMatches[3], 64)
	}

	if stats.PacketsTransmitted > 0 && stats.PacketsReceived == 0 {
		stats.Status = "down"
		stats.Summary = fmt.Sprintf("Target unreachable (100%% packet loss, %d packets sent).", stats.PacketsTransmitted)
		stats.SummaryID = fmt.Sprintf("Target tidak merespons (100%% packet loss, %d paket terkirim).", stats.PacketsTransmitted)
	} else if stats.PacketLossPct > 0 {
		stats.Status = "degraded"
		stats.Summary = fmt.Sprintf("Connection degraded (%.0f%% packet loss, avg RTT %.1f ms).", stats.PacketLossPct, stats.AvgMs)
		stats.SummaryID = fmt.Sprintf("Koneksi mengalami degradasi (%.0f%% packet loss, RTT rata-rata %.1f ms).", stats.PacketLossPct, stats.AvgMs)
	} else if stats.PacketsReceived > 0 {
		stats.Status = "healthy"
		stats.Summary = fmt.Sprintf("Connection stable (0%% packet loss, avg RTT %.1f ms, min/max %.1f/%.1f ms).", stats.AvgMs, stats.MinMs, stats.MaxMs)
		stats.SummaryID = fmt.Sprintf("Koneksi stabil (0%% packet loss, RTT rata-rata %.1f ms, min/max %.1f/%.1f ms).", stats.AvgMs, stats.MinMs, stats.MaxMs)
	} else {
		stats.Status = "down"
		stats.Summary = "Ping command failed or output could not be parsed."
		stats.SummaryID = "Perintah ping gagal atau output tidak dapat diuraikan."
	}

	return stats
}

// ParsePortOutput parses port probe output.
func ParsePortOutput(output string, port int) *PortStats {
	isOpen := strings.Contains(output, "PORT_OPEN")
	stats := &PortStats{
		Port: port,
		Open: isOpen,
	}

	if isOpen {
		stats.Status = "healthy"
		stats.Summary = fmt.Sprintf("Port %d is OPEN and reachable.", port)
		stats.SummaryID = fmt.Sprintf("Port %d TERBUKA dan dapat dijangkau.", port)
	} else {
		stats.Status = "down"
		stats.Summary = fmt.Sprintf("Port %d is CLOSED or filtered by firewall.", port)
		stats.SummaryID = fmt.Sprintf("Port %d TERTUTUP atau diblokir firewall.", port)
	}
	return stats
}

// ParseDNSOutput parses `getent hosts` or `nslookup` output.
func ParseDNSOutput(output string, target string) *DNSStats {
	stats := &DNSStats{
		Target: target,
		Status: "down",
	}

	// Extract unique IP addresses
	ipRegex := regexp.MustCompile(`\b(?:\d{1,3}\.){3}\d{1,3}\b`)
	rawIPs := ipRegex.FindAllString(output, -1)
	seen := make(map[string]bool)

	for _, ip := range rawIPs {
		// Filter out 0.0.0.0 or 127.0.0.53 (systemd-resolved local listener)
		if ip == "0.0.0.0" || ip == "127.0.0.53" {
			continue
		}
		if !seen[ip] {
			seen[ip] = true
			stats.ResolvedIPs = append(stats.ResolvedIPs, ip)
		}
	}

	if len(stats.ResolvedIPs) > 0 {
		stats.Status = "healthy"
		stats.Summary = fmt.Sprintf("Successfully resolved %s to: %s", target, strings.Join(stats.ResolvedIPs, ", "))
		stats.SummaryID = fmt.Sprintf("Berhasil me-resolve %s ke: %s", target, strings.Join(stats.ResolvedIPs, ", "))
	} else {
		stats.Status = "down"
		stats.Summary = fmt.Sprintf("Failed to resolve domain name '%s'. Check local DNS configuration.", target)
		stats.SummaryID = fmt.Sprintf("Gagal me-resolve nama domain '%s'. Periksa konfigurasi DNS lokal.", target)
	}
	return stats
}

var (
	routeDevRegex = regexp.MustCompile(`\bdev\s+([a-zA-Z0-9_.-]+)`)
	routeViaRegex = regexp.MustCompile(`\bvia\s+([0-9a-fA-F.:]+)`)
	routeMtuRegex = regexp.MustCompile(`\bmtu\s+(\d+)`)
)

// ParseRouteGetOutput parses the output of `ip route get <target>`.
func ParseRouteGetOutput(output string, target string) *RouteInfo {
	info := &RouteInfo{
		TargetAddress: target,
		MTU:           1500,
	}

	cleanOutput := strings.ReplaceAll(output, "\\\n", " ")
	cleanOutput = strings.ReplaceAll(cleanOutput, "\n", " ")

	if m := routeDevRegex.FindStringSubmatch(cleanOutput); len(m) > 1 {
		info.Interface = m[1]
	}
	if m := routeViaRegex.FindStringSubmatch(cleanOutput); len(m) > 1 {
		info.Gateway = m[1]
	}
	if m := routeMtuRegex.FindStringSubmatch(cleanOutput); len(m) > 1 {
		if mtuVal, err := strconv.Atoi(m[1]); err == nil && mtuVal > 0 {
			info.MTU = mtuVal
		}
	}

	ip := net.ParseIP(target)
	info.IsPrivate = IsPrivateIP(ip)

	if info.Interface != "" {
		gwText := ""
		if info.Gateway != "" {
			gwText = fmt.Sprintf(" via %s", info.Gateway)
		}
		info.Summary = fmt.Sprintf("Routed via interface %s%s (MTU %d)", info.Interface, gwText, info.MTU)
	} else {
		info.Summary = fmt.Sprintf("Default system route to %s", target)
	}

	return info
}

// ParseCurlL7Output parses curl output formatted as: http_code|time_namelookup|time_connect|time_starttransfer|time_total
func ParseCurlL7Output(output string, endpoint string, vantage string) *L7ProbeStats {
	stats := &L7ProbeStats{
		Endpoint: endpoint,
		Vantage:  vantage,
		Status:   "down",
	}

	lines := strings.Split(strings.TrimSpace(output), "\n")
	var targetLine string
	for i := len(lines) - 1; i >= 0; i-- {
		l := strings.TrimSpace(lines[i])
		if strings.Contains(l, "|") {
			targetLine = l
			break
		}
	}

	if targetLine == "" {
		stats.Summary = "HTTP probe failed: no response from server or connection refused."
		stats.SummaryID = "Uji HTTP gagal: tidak ada respons dari server atau koneksi ditolak."
		return stats
	}

	parts := strings.Split(targetLine, "|")
	if len(parts) >= 5 {
		code, _ := strconv.Atoi(strings.TrimSpace(parts[0]))
		dnsSec, _ := strconv.ParseFloat(strings.TrimSpace(parts[1]), 64)
		connSec, _ := strconv.ParseFloat(strings.TrimSpace(parts[2]), 64)
		ttfbSec, _ := strconv.ParseFloat(strings.TrimSpace(parts[3]), 64)
		totalSec, _ := strconv.ParseFloat(strings.TrimSpace(parts[4]), 64)

		stats.HTTPCode = code
		stats.DNSMs = dnsSec * 1000.0
		stats.ConnectMs = connSec * 1000.0
		stats.TTFBMs = ttfbSec * 1000.0
		stats.TotalMs = totalSec * 1000.0

		if code >= 200 && code < 400 {
			if stats.TTFBMs > 2500.0 {
				stats.Status = "degraded"
				stats.Summary = fmt.Sprintf("HTTP %d OK, but slow TTFB (%.1f ms). Application response degraded.", code, stats.TTFBMs)
				stats.SummaryID = fmt.Sprintf("HTTP %d OK, namun TTFB lambat (%.1f ms). Kinerja aplikasi terdegradasi.", code, stats.TTFBMs)
			} else {
				stats.Status = "healthy"
				stats.Summary = fmt.Sprintf("HTTP %d OK, TTFB %.1f ms, Total %.1f ms.", code, stats.TTFBMs, stats.TotalMs)
				stats.SummaryID = fmt.Sprintf("HTTP %d OK, TTFB %.1f ms, Total %.1f ms.", code, stats.TTFBMs, stats.TotalMs)
			}
		} else if code >= 400 && code < 500 {
			stats.Status = "degraded"
			stats.Summary = fmt.Sprintf("HTTP %d Client Error (TTFB %.1f ms). Endpoint path may be invalid.", code, stats.TTFBMs)
			stats.SummaryID = fmt.Sprintf("HTTP %d Client Error (TTFB %.1f ms). Path endpoint mungkin tidak valid.", code, stats.TTFBMs)
		} else if code >= 500 {
			stats.Status = "degraded"
			stats.Summary = fmt.Sprintf("HTTP %d Server Error (Internal / Bad Gateway). Application is throwing errors.", code)
			stats.SummaryID = fmt.Sprintf("HTTP %d Server Error (Internal / Bad Gateway). Aplikasi mengalami error internal.", code)
		} else {
			stats.Status = "down"
			stats.Summary = "HTTP probe failed: server returned status 0 (connection refused/timeout)."
			stats.SummaryID = "Uji HTTP gagal: server mengembalikan status 0 (koneksi ditolak/timeout)."
		}
	} else {
		stats.Summary = "Malformed curl output."
		stats.SummaryID = "Format output curl tidak valid."
	}

	return stats
}

// ParseMTUPingSuccess returns true if an MTU test ping passed with Don't Fragment flag.
func ParseMTUPingSuccess(output string) bool {
	lower := strings.ToLower(output)
	if strings.Contains(lower, "frag needed") ||
		strings.Contains(lower, "too long") ||
		strings.Contains(lower, "oversized") ||
		strings.Contains(lower, "100% packet loss") ||
		strings.Contains(lower, "100.0% packet loss") {
		return false
	}
	// Verify at least 1 packet received
	m := pingStatsRegex.FindStringSubmatch(output)
	if len(m) >= 3 {
		rcv, _ := strconv.Atoi(m[2])
		return rcv > 0
	}
	return false
}
