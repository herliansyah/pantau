package netdiag

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"

	"pantau/internal/logger"
	"pantau/internal/sshrunner"
	"pantau/internal/store"
)

func execRunnerCmd(runner sshrunner.Runner, cmd string, timeout time.Duration) string {
	stdout, stderr, _, _ := runner.ExecTimeout(cmd, timeout)
	if stderr != "" {
		if stdout != "" {
			return stdout + "\n" + stderr
		}
		return stderr
	}
	return stdout
}

// ExecuteTriage runs the automated, multi-layer branch diagnostic triage workflow.
// ponytail: Orchestrates host health, dual L7 HTTP, route discovery, and MTU sweep in a single non-blocking pass.
func (s *Service) ExecuteTriage(ctx context.Context, req TriageRequest, eventSink func(TriageEvent)) (*TriageResult, error) {
	if eventSink == nil {
		eventSink = func(TriageEvent) {}
	}
	startTime := time.Now()

	// 1. Resolve preset if preset_id is supplied
	if req.PresetID > 0 {
		preset, err := s.db.GetProbeTargetPreset(req.PresetID)
		if err == nil && preset != nil {
			if req.TargetAddress == "" {
				req.TargetAddress = preset.Target
			}
			if req.TargetName == "" {
				req.TargetName = preset.Name
			}
			if req.AppEndpoint == "" {
				req.AppEndpoint = preset.AppEndpoint
			}
			if req.BaselineMTU <= 0 {
				req.BaselineMTU = preset.BaselineMTU
			}
			if req.MaxLatencyMs <= 0 {
				req.MaxLatencyMs = preset.MaxLatencyMs
			}
			if req.MaxLossPct <= 0 {
				req.MaxLossPct = preset.MaxLossPct
			}
			if req.DBPort <= 0 {
				req.DBPort = preset.DBPort
			}
		}
	}

	// ponytail: Check host-level App & DB settings if not explicitly provided in the request
	if req.AppEndpoint == "" {
		if app, _ := s.db.GetSetting(fmt.Sprintf("triage_host_%d_app", req.SourceHostID)); app != "" {
			req.AppEndpoint = app
		}
	}
	if req.DBPort <= 0 {
		if dbStr, _ := s.db.GetSetting(fmt.Sprintf("triage_host_%d_db", req.SourceHostID)); dbStr != "" {
			if p, err := strconv.Atoi(dbStr); err == nil && p > 0 {
				req.DBPort = p
			}
		}
	}

	// Smart defaults
	req.TargetAddress = strings.TrimSpace(req.TargetAddress)
	if req.TargetName == "" {
		req.TargetName = req.TargetAddress
	}
	if req.BaselineMTU <= 0 {
		req.BaselineMTU = 1500
	}
	if req.MaxLatencyMs <= 0 {
		req.MaxLatencyMs = 80.0
	}
	if req.MaxLossPct <= 0 {
		req.MaxLossPct = 2.0
	}

	if err := ValidateTarget(req.TargetAddress); err != nil {
		return nil, err
	}

	// 2. Resolve source host
	sourceHost, err := s.db.GetHost(req.SourceHostID)
	if err != nil {
		return nil, fmt.Errorf("get source host: %w", err)
	}
	if sourceHost == nil {
		return nil, errors.New("source host not found")
	}
	if strings.EqualFold(sourceHost.Status, "down") {
		return nil, fmt.Errorf("source host '%s' is offline", sourceHost.Name)
	}

	// 3. Singleflight concurrency guard per source host
	if _, loaded := s.inFlight.LoadOrStore(req.SourceHostID, struct{}{}); loaded {
		return nil, ErrBusy
	}
	defer s.inFlight.Delete(req.SourceHostID)

	eventSink(TriageEvent{
		Type:      "stage",
		Stage:     "host_check",
		Message:   fmt.Sprintf("Starting Network Triage Matrix from %s to %s...", sourceHost.Name, req.TargetName),
		MessageID: fmt.Sprintf("Memulai Network Triage Matrix dari %s ke %s...", sourceHost.Name, req.TargetName),
	})

	// 4. Host Resource Assessment
	var cpuVal float64
	fields := strings.Fields(strings.ReplaceAll(sourceHost.CPULoad, ",", " "))
	if len(fields) > 0 {
		cpuVal, _ = strconv.ParseFloat(fields[0], 64)
	}

	var ramPct, swapPct float64
	if sourceHost.RAMTotalBytes > 0 {
		ramPct = (float64(sourceHost.RAMUsedBytes) / float64(sourceHost.RAMTotalBytes)) * 100.0
	}
	if sourceHost.SwapTotalBytes > 0 {
		swapPct = (float64(sourceHost.SwapUsedBytes) / float64(sourceHost.SwapTotalBytes)) * 100.0
	}

	hostResource := &HostResourceStats{
		CPULoad:           cpuVal,
		RAMUsedPct:        ramPct,
		SwapUsedPct:       swapPct,
		ActiveConnections: sourceHost.ActiveConnCount,
		Status:            "healthy",
	}

	// Universal OS-level saturation condition:
	// In Linux, RAM > 90% is normal for database buffer pools (MySQL InnoDB, Postgres shared buffers) and page caches.
	// Saturation is flagged if CPU load is pegged (>0.95), socket exhaustion (>2000 & load>0.85),
	// or true memory exhaustion with active swap thrashing (RAM > 95% AND Swap > 40%).
	if cpuVal > 0.95 || (ramPct > 95.0 && swapPct > 40.0) || (sourceHost.ActiveConnCount > 2000 && cpuVal > 0.85) {
		hostResource.Status = "saturated"
		hostResource.Summary = fmt.Sprintf("Host saturated: CPU %.2f, RAM %.1f%%, Swap %.1f%%, Sockets %d", cpuVal, ramPct, swapPct, sourceHost.ActiveConnCount)
		hostResource.SummaryID = fmt.Sprintf("Host kelebihan beban: CPU %.2f, RAM %.1f%%, Swap %.1f%%, Sockets %d", cpuVal, ramPct, swapPct, sourceHost.ActiveConnCount)
	} else {
		hostResource.Summary = fmt.Sprintf("Host resources healthy: CPU %.2f, RAM %.1f%%, Swap %.1f%%, Sockets %d", cpuVal, ramPct, swapPct, sourceHost.ActiveConnCount)
		hostResource.SummaryID = fmt.Sprintf("Sumber daya Host sehat: CPU %.2f, RAM %.1f%%, Swap %.1f%%, Sockets %d", cpuVal, ramPct, swapPct, sourceHost.ActiveConnCount)
	}

	// 5. Connect SSH Runner
	runner, err := s.inspector.GetRunnerForHost(sourceHost)
	if err != nil {
		logger.Error("netdiag_triage", sourceHost.Name, "SSH connection failed: "+err.Error())
		return nil, fmt.Errorf("ssh connection failed: %w", err)
	}
	defer runner.Close()

	// 6. DNS Resolve if target is hostname
	resolvedTargetIP := req.TargetAddress
	if net.ParseIP(req.TargetAddress) == nil {
		eventSink(TriageEvent{
			Type:      "stage",
			Stage:     "route_discovery",
			Message:   fmt.Sprintf("Resolving DNS for hostname %s...", req.TargetAddress),
			MessageID: fmt.Sprintf("Menyelesaikan DNS untuk hostname %s...", req.TargetAddress),
		})
		dnsOut := execRunnerCmd(runner, fmt.Sprintf("getent hosts %s 2>&1 || nslookup %s 2>&1", shellQuote(req.TargetAddress), shellQuote(req.TargetAddress)), 6*time.Second)
		dnsStats := ParseDNSOutput(dnsOut, req.TargetAddress)
		if len(dnsStats.ResolvedIPs) > 0 {
			resolvedTargetIP = dnsStats.ResolvedIPs[0]
		}
	}

	// 7. Route and Interface Discovery
	eventSink(TriageEvent{
		Type:      "stage",
		Stage:     "route_discovery",
		Message:   fmt.Sprintf("Verifying interface route to %s (%s)...", req.TargetName, resolvedTargetIP),
		MessageID: fmt.Sprintf("Memverifikasi rute antarmuka ke %s (%s)...", req.TargetName, resolvedTargetIP),
	})
	routeOut := execRunnerCmd(runner, fmt.Sprintf("ip route get %s 2>&1", shellQuote(resolvedTargetIP)), 5*time.Second)
	routeInfo := ParseRouteGetOutput(routeOut, resolvedTargetIP)

	// 8. Dual L7 HTTP Probe (Local Loopback on Host A)
	appPort := 80
	appPath := "/"
	cleanEp := strings.TrimSpace(req.AppEndpoint)
	explicitEndpoint := cleanEp != "" && cleanEp != "/"

	if strings.HasPrefix(cleanEp, "http://") || strings.HasPrefix(cleanEp, "https://") {
		if parsedU, err := url.Parse(cleanEp); err == nil {
			if parsedU.Port() != "" {
				if p, err := strconv.Atoi(parsedU.Port()); err == nil {
					appPort = p
				}
			} else if parsedU.Scheme == "https" {
				appPort = 443
			}
			appPath = parsedU.RequestURI()
		}
	} else if strings.HasPrefix(cleanEp, ":") {
		parts := strings.SplitN(cleanEp[1:], "/", 2)
		if p, err := strconv.Atoi(parts[0]); err == nil {
			appPort = p
		}
		if len(parts) > 1 && parts[1] != "" {
			appPath = "/" + parts[1]
		}
	} else if strings.HasPrefix(cleanEp, "/") {
		appPath = cleanEp
	}

	var l7Local *L7ProbeStats
	shouldProbeL7 := true
	if !explicitEndpoint {
		// ponytail: Auto-bypass L7 check if port 80 is not listening on loopback to prevent false alarms on non-web hosts.
		checkPortCmd := "timeout 1 bash -c '</dev/tcp/127.0.0.1/80' 2>/dev/null && echo OPEN || echo CLOSED"
		checkPortOut := strings.TrimSpace(execRunnerCmd(runner, checkPortCmd, 2*time.Second))
		if checkPortOut != "OPEN" {
			shouldProbeL7 = false
			l7Local = &L7ProbeStats{
				Endpoint:  "n/a",
				Vantage:   "skipped_no_listener",
				Status:    "healthy",
				HTTPCode:  200,
				Summary:   "Local L7 web probe bypassed (port 80 closed, no explicit endpoint)",
				SummaryID: "Uji L7 web lokal dilewati (port 80 tertutup, tanpa endpoint khusus)",
			}
		}
	}

	if shouldProbeL7 {
		eventSink(TriageEvent{
			Type:      "stage",
			Stage:     "l7_probe",
			Message:   fmt.Sprintf("Probing local HTTP application response on Host (%s)...", appPath),
			MessageID: fmt.Sprintf("Menguji respons HTTP aplikasi lokal di Host (%s)...", appPath),
		})

		curlCmd := fmt.Sprintf("timeout 5 curl -s -o /dev/null -w '%%{http_code}|%%{time_namelookup}|%%{time_connect}|%%{time_starttransfer}|%%{time_total}\\n' http://127.0.0.1:%d%s 2>&1", appPort, appPath)
		l7LocalOut := execRunnerCmd(runner, curlCmd, 6*time.Second)
		l7Local = ParseCurlL7Output(l7LocalOut, fmt.Sprintf("http://127.0.0.1:%d%s", appPort, appPath), "local_loopback")
	}

	// 8b. Auto-detect & Probe Local Database on Source Host A (e.g. 3306 for MySQL/Percona, 5432 for Postgres)
	targetDBPort := req.DBPort
	if targetDBPort <= 0 {
		detectCmd := "timeout 2 bash -c '</dev/tcp/127.0.0.1/3306' 2>/dev/null && echo 3306 || (timeout 2 bash -c '</dev/tcp/127.0.0.1/5432' 2>/dev/null && echo 5432 || echo 0)"
		detectOut := strings.TrimSpace(execRunnerCmd(runner, detectCmd, 3*time.Second))
		if p, err := strconv.Atoi(detectOut); err == nil && p > 0 {
			targetDBPort = p
		}
	}

	var dbPortStats *PortStats
	if targetDBPort > 0 {
		dbProbeCmd := fmt.Sprintf("(timeout 3 nc -z -w 2 127.0.0.1 %d 2>&1 && echo 'PORT_OPEN') || (timeout 3 bash -c '</dev/tcp/127.0.0.1/%d' 2>&1 && echo 'PORT_OPEN') || echo 'PORT_CLOSED'", targetDBPort, targetDBPort)
		dbOut := execRunnerCmd(runner, dbProbeCmd, 4*time.Second)
		dbPortStats = ParsePortOutput(dbOut, targetDBPort)
	}

	// 9. Path & Reachability Diagnostics
	eventSink(TriageEvent{
		Type:      "stage",
		Stage:     "path_probe",
		Message:   fmt.Sprintf("Probing branch path reachability & MTU fragmentation to %s...", req.TargetName),
		MessageID: fmt.Sprintf("Menguji keterjangkauan jalur dan fragmentasi MTU ke %s...", req.TargetName),
	})

	var mtuStats *MTUProbeStats
	var tcpStats *PortStats
	var hopAnalysis *HopAnalysis

	// If route is private or target is private IP, perform MTU Sweep Probe
	if routeInfo.IsPrivate || IsPrivateIP(net.ParseIP(resolvedTargetIP)) {
		mtuStats = &MTUProbeStats{
			BaselineMTU: req.BaselineMTU,
			Status:      "healthy",
		}
		// Test sizes: small (1280), baseline - 28 (payload for ICMP), and standard 1472
		targetPayloads := []int{1200}
		if req.BaselineMTU > 28 {
			targetPayloads = append(targetPayloads, req.BaselineMTU-28)
		}
		if req.BaselineMTU < 1500 {
			targetPayloads = append(targetPayloads, 1472) // Oversized test for VPN
		}

		mtuStats.TestedSizes = targetPayloads
		for _, sz := range targetPayloads {
			mtuCmd := fmt.Sprintf("timeout 3 ping -c 2 -M do -s %d -W 1 %s 2>&1", sz, shellQuote(resolvedTargetIP))
			mtuOut := execRunnerCmd(runner, mtuCmd, 4*time.Second)
			if ParseMTUPingSuccess(mtuOut) {
				mtuStats.PassedSizes = append(mtuStats.PassedSizes, sz)
			}
		}

		// Detect PMTU Blackhole: small packet passed, but baseline tunnel MTU failed
		smallPassed := false
		baselinePassed := false
		for _, sz := range mtuStats.PassedSizes {
			if sz <= 1200 {
				smallPassed = true
			}
			if sz == req.BaselineMTU-28 {
				baselinePassed = true
			}
		}

		if smallPassed && !baselinePassed && req.BaselineMTU > 28 {
			mtuStats.BlackholeDetected = true
			mtuStats.Status = "blackhole"
			mtuStats.Summary = fmt.Sprintf("PMTU Blackhole detected! Small packets (%d bytes) pass, but tunnel MTU (%d bytes) is dropped.", 1200, req.BaselineMTU)
			mtuStats.SummaryID = fmt.Sprintf("Terdeteksi PMTU Blackhole! Paket kecil (%d byte) lolos, namun ukuran MTU tunnel (%d byte) terpotong/drop.", 1200, req.BaselineMTU)
		} else {
			mtuStats.Summary = fmt.Sprintf("MTU sweep verification passed (Baseline %d bytes).", req.BaselineMTU)
			mtuStats.SummaryID = fmt.Sprintf("Verifikasi fragmentasi MTU normal (Baseline %d byte).", req.BaselineMTU)
		}
	}

	// Standard ICMP Ping Probe
	pingCmd := fmt.Sprintf("timeout 7 ping -c 4 -W 2 %s 2>&1", shellQuote(resolvedTargetIP))
	pingOut := execRunnerCmd(runner, pingCmd, 8*time.Second)
	pingStats := ParsePingOutput(pingOut)

	// Fallback to TCP port probe if ping is 100% loss (Anti-False Alarm)
	if pingStats.PacketLossPct >= 99.0 {
		tcpProbeCmd := fmt.Sprintf("timeout 3 bash -c '</dev/tcp/%s/%d' 2>/dev/null && echo 'PORT_OPEN' || echo 'PORT_CLOSED'", resolvedTargetIP, appPort)
		tcpOut := execRunnerCmd(runner, tcpProbeCmd, 4*time.Second)
		tcpStats = ParsePortOutput(tcpOut, appPort)
	}

	// If public IP, optionally execute lightweight hop analysis
	if !routeInfo.IsPrivate && !IsPrivateIP(net.ParseIP(resolvedTargetIP)) {
		traceCmd := fmt.Sprintf("command -v tracepath >/dev/null 2>&1 && (timeout 8 tracepath -n -m 12 %s 2>&1) || (timeout 8 traceroute -n -m 12 -q 1 -w 1 %s 2>&1)", shellQuote(resolvedTargetIP), shellQuote(resolvedTargetIP))
		traceOut := execRunnerCmd(runner, traceCmd, 10*time.Second)
		var hops []*HopInfo
		lines := strings.Split(traceOut, "\n")
		for _, l := range lines {
			if h := ParseTracepathOrTracerouteLine(l, resolvedTargetIP); h != nil {
				hops = append(hops, h)
			}
		}
		if len(hops) > 0 {
			hopAnalysis = AnalyzeHops(hops, resolvedTargetIP, strings.Contains(traceOut, "reached"))
		}
	}

	// 10. Verdict Engine Computation
	eventSink(TriageEvent{
		Type:      "stage",
		Stage:     "verdict",
		Message:   "Computing final Triage Verdict...",
		MessageID: "Menghitung kesimpulan akhir Triage Verdict...",
	})

	var verdict TriageVerdict
	var summary, summaryID string

	switch {
	case hostResource.Status == "saturated":
		verdict = VerdictHostSaturated
		summary = fmt.Sprintf("Host '%s' is resource saturated (CPU %.2f, RAM %.1f%%, Swap %.1f%%). Server compute bottleneck.", sourceHost.Name, hostResource.CPULoad, hostResource.RAMUsedPct, hostResource.SwapUsedPct)
		summaryID = fmt.Sprintf("Host '%s' mengalami kehabisan sumber daya komputasi (CPU %.2f, RAM %.1f%%, Swap %.1f%%). Kendala ada pada server.", sourceHost.Name, hostResource.CPULoad, hostResource.RAMUsedPct, hostResource.SwapUsedPct)

	case l7Local.HTTPCode == 502 || l7Local.HTTPCode == 504:
		verdict = VerdictAppDegraded
		dbNoteEn := ""
		dbNoteId := ""
		if dbPortStats != nil && !dbPortStats.Open {
			dbNoteEn = fmt.Sprintf(" Local auxiliary service / DB port %d is CLOSED/NOT RESPONDING.", req.DBPort)
			dbNoteId = fmt.Sprintf(" Port layanan / DB lokal %d TERTUTUP / TIDAK MERESPONS.", req.DBPort)
		}
		summary = fmt.Sprintf("Reverse proxy returned HTTP %d (Bad Gateway / Gateway Timeout) on loopback. Upstream backend daemon or database query stalled.%s", l7Local.HTTPCode, dbNoteEn)
		summaryID = fmt.Sprintf("Reverse proxy mengembalikan HTTP %d (Bad Gateway / Gateway Timeout) pada loopback. Daemon backend atau query database di server mengalami stall/timeout.%s", l7Local.HTTPCode, dbNoteId)

	case l7Local.HTTPCode == 500:
		verdict = VerdictAppDegraded
		dbNoteEn := ""
		dbNoteId := ""
		if dbPortStats != nil && !dbPortStats.Open {
			dbNoteEn = fmt.Sprintf(" Local DB port %d is closed.", req.DBPort)
			dbNoteId = fmt.Sprintf(" Port DB lokal %d tertutup.", req.DBPort)
		}
		summary = fmt.Sprintf("Application returned HTTP 500 Internal Server Error on loopback. Unhandled application runtime exception or database crash.%s", dbNoteEn)
		summaryID = fmt.Sprintf("Aplikasi mengembalikan HTTP 500 Internal Server Error pada loopback. Terjadi error/exception runtime pada aplikasi atau kegagalan koneksi database.%s", dbNoteId)

	case l7Local.HTTPCode == 503:
		verdict = VerdictAppDegraded
		summary = fmt.Sprintf("Application returned HTTP 503 Service Unavailable on loopback. Server backend overloaded, rate-limited, or in maintenance mode.")
		summaryID = fmt.Sprintf("Aplikasi mengembalikan HTTP 503 Service Unavailable pada loopback. Backend server kelebihan beban atau sedang dalam mode pemeliharaan.")

	case l7Local.HTTPCode >= 501:
		verdict = VerdictAppDegraded
		summary = fmt.Sprintf("Application returned HTTP %d Error on loopback. Web application/backend is failing.", l7Local.HTTPCode)
		summaryID = fmt.Sprintf("Aplikasi web mengembalikan HTTP %d Error pada loopback. Terjadi kegagalan pada backend aplikasi/database.", l7Local.HTTPCode)

	case l7Local.HTTPCode == 0 && shouldProbeL7 && explicitEndpoint:
		verdict = VerdictAppDegraded
		summary = fmt.Sprintf("Application port %d connection refused or web service daemon is dead on Host '%s'.", appPort, sourceHost.Name)
		summaryID = fmt.Sprintf("Port aplikasi %d koneksi ditolak atau daemon web service mati pada Host '%s'.", appPort, sourceHost.Name)

	case mtuStats != nil && mtuStats.BlackholeDetected:
		verdict = VerdictMTUBlackhole
		summary = fmt.Sprintf("PMTU Blackhole detected between Host '%s' and '%s'. Packets >%d bytes dropped by tunnel.", sourceHost.Name, req.TargetName, req.BaselineMTU)
		summaryID = fmt.Sprintf("Terdeteksi PMTU Blackhole antara Host '%s' dan '%s'. Paket >%d byte terpotong oleh terowongan VPN.", sourceHost.Name, req.TargetName, req.BaselineMTU)

	case pingStats.PacketLossPct >= 99.0 && tcpStats != nil && tcpStats.Open:
		verdict = VerdictICMPFilteredTCP
		summary = fmt.Sprintf("Target '%s' filters ICMP ping, but TCP service port %d is OPEN and healthy.", req.TargetName, appPort)
		summaryID = fmt.Sprintf("Target '%s' memblokir ICMP ping (firewall), namun port TCP layanan %d TERBUKA dan sehat.", req.TargetName, appPort)

	case pingStats.PacketLossPct >= 99.0 && (tcpStats == nil || !tcpStats.Open):
		verdict = VerdictBranchGatewayDown
		summary = fmt.Sprintf("Target '%s' (%s) is completely UNREACHABLE. VPN tunnel, network gateway, or remote host may be down.", req.TargetName, resolvedTargetIP)
		summaryID = fmt.Sprintf("Target '%s' (%s) TIDAK DAPAT DIJANGKAU sama sekali. Terowongan VPN, gateway jaringan, atau host tujuan mungkin mati.", req.TargetName, resolvedTargetIP)

	case pingStats.PacketLossPct > req.MaxLossPct || pingStats.AvgMs > req.MaxLatencyMs:
		verdict = VerdictTransitPathDegraded
		summary = fmt.Sprintf("Network transit degraded to '%s': Packet Loss %.1f%% (max %.1f%%), Latency %.1f ms (max %.1f ms).", req.TargetName, pingStats.PacketLossPct, req.MaxLossPct, pingStats.AvgMs, req.MaxLatencyMs)
		summaryID = fmt.Sprintf("Jalur transmisi ke '%s' terdegradasi: Packet Loss %.1f%% (maks %.1f%%), Latensi %.1f ms (maks %.1f ms).", req.TargetName, pingStats.PacketLossPct, req.MaxLossPct, pingStats.AvgMs, req.MaxLatencyMs)

	case l7Local.TTFBMs > 2500.0:
		verdict = VerdictAppDegraded
		dbNoteEn := ""
		dbNoteId := ""
		if dbPortStats != nil && !dbPortStats.Open {
			dbNoteEn = fmt.Sprintf(" Local auxiliary service / DB port %d is NOT responding.", req.DBPort)
			dbNoteId = fmt.Sprintf(" Port layanan / DB lokal %d TIDAK merespons.", req.DBPort)
		}
		summary = fmt.Sprintf("Application TTFB response is slow (%.1f ms). Heavy backend computation, thread pool saturation, or slow database query.%s", l7Local.TTFBMs, dbNoteEn)
		summaryID = fmt.Sprintf("Respons TTFB aplikasi lambat (%.1f ms). Beban komputasi backend tinggi, saturasi thread pool, atau query database lambat.%s", l7Local.TTFBMs, dbNoteId)

	default:
		verdict = VerdictHealthy
		summary = fmt.Sprintf("All layers healthy! Host CPU %.2f, L7 HTTP %d (TTFB %.1f ms), Transit ping %.1f ms, 0%% loss.", hostResource.CPULoad, l7Local.HTTPCode, l7Local.TTFBMs, pingStats.AvgMs)
		summaryID = fmt.Sprintf("Semua lapisan sehat! Host CPU %.2f, L7 HTTP %d (TTFB %.1f ms), Latensi rute %.1f ms, 0%% loss.", hostResource.CPULoad, l7Local.HTTPCode, l7Local.TTFBMs, pingStats.AvgMs)
	}

	// 11. Format Ticket Snippet (WhatsApp / Markdown ready)
	ticket := formatIncidentTicket(sourceHost.Name, req.TargetName, resolvedTargetIP, verdict, summaryID, hostResource, l7Local, dbPortStats, targetDBPort, routeInfo, pingStats, mtuStats)

	durationMs := time.Since(startTime).Milliseconds()

	result := &TriageResult{
		SourceHostName: sourceHost.Name,
		TargetName:     req.TargetName,
		TargetAddress:  req.TargetAddress,
		ResolvedIP:     resolvedTargetIP,
		Verdict:        verdict,
		Summary:        summary,
		SummaryID:      summaryID,
		HostResource:   hostResource,
		L7Local:        l7Local,
		DBPort:         dbPortStats,
		Route:          routeInfo,
		Ping:           pingStats,
		MTU:            mtuStats,
		TCPPort:        tcpStats,
		HopAnalysis:    hopAnalysis,
		TicketSnippet:  ticket,
		DurationMs:     durationMs,
		ExecutedAt:     time.Now().UTC(),
	}

	// 12. Persist Incident Log into SQLite
	detailsJSONBytes, _ := json.Marshal(result)
	var presetIDPtr *int64
	if req.PresetID > 0 {
		pid := req.PresetID
		presetIDPtr = &pid
	}

	var hostSt, appSt, pathSt, mtuSt string
	hostSt = hostResource.Status
	appSt = l7Local.Status
	pathSt = pingStats.Status
	if mtuStats != nil {
		mtuSt = mtuStats.Status
	} else {
		mtuSt = "n/a"
	}

	_, _ = s.db.CreateTriageIncidentLog(&store.TriageIncidentLog{
		SourceHostID:  sourceHost.ID,
		PresetID:      presetIDPtr,
		TargetName:    req.TargetName,
		TargetAddress: resolvedTargetIP,
		Verdict:       string(verdict),
		Summary:       summary,
		SummaryID:     summaryID,
		HostStatus:    hostSt,
		AppStatus:     appSt,
		PathStatus:    pathSt,
		MTUStatus:     mtuSt,
		DetailsJSON:   string(detailsJSONBytes),
	})

	eventSink(TriageEvent{
		Type:      "result",
		Stage:     "done",
		Message:   "Network Triage Matrix completed.",
		MessageID: "Network Triage Matrix selesai.",
		Result:    result,
	})

	return result, nil
}

func formatIncidentTicket(sourceHost, targetName, targetIP string, verdict TriageVerdict, summaryID string, hr *HostResourceStats, l7 *L7ProbeStats, dbPort *PortStats, dbPortNum int, route *RouteInfo, ping *PingStats, mtu *MTUProbeStats) string {
	var b strings.Builder
	nowStr := time.Now().Format("2006-01-02 15:04:05 MST")

	b.WriteString("🚨 *[LAPORAN TRIAGE JARINGAN & SERVER PANTAU]*\n")
	b.WriteString(fmt.Sprintf("⏰ *Waktu*: %s\n", nowStr))
	b.WriteString(fmt.Sprintf("🖥️ *Sumber (Host)*: %s\n", sourceHost))
	b.WriteString(fmt.Sprintf("🎯 *Target*: %s (%s)\n", targetName, targetIP))
	b.WriteString("-------------------------------------------\n")

	// Host & App
	swapStr := ""
	if hr.SwapUsedPct > 0 {
		swapStr = fmt.Sprintf(" | Swap %.1f%%", hr.SwapUsedPct)
	}
	b.WriteString(fmt.Sprintf("⚙️ *Status Server*: CPU %.2f | RAM %.1f%%%s | Soket %d (%s)\n", hr.CPULoad, hr.RAMUsedPct, swapStr, hr.ActiveConnections, strings.ToUpper(hr.Status)))
	if l7 != nil {
		b.WriteString(fmt.Sprintf("🌐 *Status Aplikasi*: HTTP %d | TTFB %.1f ms | Total %.1f ms (%s)\n", l7.HTTPCode, l7.TTFBMs, l7.TotalMs, strings.ToUpper(l7.Status)))
	}
	if dbPort != nil && dbPortNum > 0 {
		st := "TERBUKA (LANCAR)"
		if !dbPort.Open {
			st = "TERTUTUP / TIDAK RESPON"
		}
		b.WriteString(fmt.Sprintf("🗄️ *Port Layanan/DB (%d)*: %s\n", dbPortNum, st))
	}

	// Route & Path
	if route != nil && route.Interface != "" {
		b.WriteString(fmt.Sprintf("🛤️ *Antarmuka*: %s (MTU %d)\n", route.Interface, route.MTU))
	}
	if ping != nil {
		b.WriteString(fmt.Sprintf("📡 *Rute Jalur*: Latensi %.1f ms | Loss %.1f%% (%s)\n", ping.AvgMs, ping.PacketLossPct, strings.ToUpper(ping.Status)))
	}
	if mtu != nil && mtu.BlackholeDetected {
		b.WriteString(fmt.Sprintf("⚠️ *Status MTU*: PMTU BLACKHOLE! Paket tunnel %d byte di-drop.\n", mtu.BaselineMTU))
	} else if mtu != nil {
		b.WriteString(fmt.Sprintf("✅ *Status MTU*: Normal (Baseline %d byte lolos).\n", mtu.BaselineMTU))
	}

	b.WriteString("-------------------------------------------\n")
	b.WriteString(fmt.Sprintf("🏁 *VERDICT*: *%s*\n", verdict))
	b.WriteString(fmt.Sprintf("📝 *Diagnosa*: %s\n", summaryID))
	b.WriteString("-------------------------------------------\n")
	b.WriteString("_Dihasilkan otomatis secara agentless oleh Pantau Monitoring._")

	return b.String()
}
