package netdiag

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"pantau/internal/inspector"
	"pantau/internal/logger"
	"pantau/internal/notify"
	"pantau/internal/store"
)

var (
	validTargetRegex = regexp.MustCompile(`^[a-zA-Z0-9_.:-]+$`)
	ErrBusy          = errors.New("a diagnostic probe is already in progress for this host")
)

type TargetState struct {
	LastVerdict       TriageVerdict
	LastAlertTime     time.Time
	IncidentStartTime time.Time
}

type Service struct {
	db           *store.DB
	inspector    *inspector.Inspector
	dispatcher   *notify.Dispatcher
	inFlight     sync.Map // sourceHostID -> struct{}{}
	stateMu      sync.Mutex
	targetStates map[int64]*TargetState // presetID -> TargetState
}

func NewService(db *store.DB, ins *inspector.Inspector, disp *notify.Dispatcher) *Service {
	return &Service{
		db:           db,
		inspector:    ins,
		dispatcher:   disp,
		targetStates: make(map[int64]*TargetState),
	}
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}

// ValidateTarget checks if the given target IP or hostname contains only safe characters.
func ValidateTarget(target string) error {
	t := strings.TrimSpace(target)
	if t == "" {
		return errors.New("target address cannot be empty")
	}
	if len(t) > 255 {
		return errors.New("target address too long")
	}
	if !validTargetRegex.MatchString(t) {
		return fmt.Errorf("target '%s' contains invalid characters", target)
	}
	return nil
}

// ExecuteProbe runs the requested diagnostic probe and streams events in real-time.
// ponytail: Concurrency guard / singleflight prevents overlapping probes and SSH command stampedes on remote hosts.
func (s *Service) ExecuteProbe(ctx context.Context, req ProbeRequest, eventSink func(ProbeEvent)) error {
	if eventSink == nil {
		eventSink = func(ProbeEvent) {}
	}

	// 1. Validate Target and Port
	if req.Type == ProbeTypePort {
		if req.Port < 1 || req.Port > 65535 {
			return errors.New("port must be between 1 and 65535")
		}
	}

	// 2. Resolve target if Inter-Host
	if req.TargetType == "host" {
		if req.TargetHostID <= 0 {
			return errors.New("target host ID required for inter-host probe")
		}
		targetHost, err := s.db.GetHost(req.TargetHostID)
		if err != nil {
			return fmt.Errorf("get target host: %w", err)
		}
		if targetHost == nil {
			return errors.New("target host not found")
		}
		if strings.EqualFold(targetHost.Status, "down") {
			return fmt.Errorf("target host '%s' is offline and cannot be probed", targetHost.Name)
		}
		req.TargetAddress = targetHost.Host
	}

	if err := ValidateTarget(req.TargetAddress); err != nil {
		return err
	}

	// 3. Resolve source host
	sourceHost, err := s.db.GetHost(req.SourceHostID)
	if err != nil {
		return fmt.Errorf("get source host: %w", err)
	}
	if sourceHost == nil {
		return errors.New("source host not found")
	}
	if strings.EqualFold(sourceHost.Status, "down") {
		return fmt.Errorf("source host '%s' is offline", sourceHost.Name)
	}

	// 4. Singleflight guard per source host
	if _, loaded := s.inFlight.LoadOrStore(req.SourceHostID, struct{}{}); loaded {
		return ErrBusy
	}
	defer s.inFlight.Delete(req.SourceHostID)

	// 5. Connect SSH runner
	runner, err := s.inspector.GetRunnerForHost(sourceHost)
	if err != nil {
		logger.Error("netdiag", sourceHost.Name, "SSH connection failed: "+err.Error())
		return fmt.Errorf("ssh connection failed: %w", err)
	}
	defer runner.Close()

	// 6. Build command and timeout budget
	var cmd string
	var probeTimeout time.Duration

	quotedTarget := shellQuote(req.TargetAddress)

	switch req.Type {
	case ProbeTypePing:
		probeTimeout = 16 * time.Second
		cmd = fmt.Sprintf("timeout 14 ping -c 4 -W 2 %s 2>&1", quotedTarget)

	case ProbeTypeTraceroute:
		probeTimeout = 18 * time.Second
		// ponytail: Prefer tracepath without raw root socket, fallback to traceroute if installed.
		cmd = fmt.Sprintf("command -v tracepath >/dev/null 2>&1 && (timeout 15 tracepath -n -m 20 %s 2>&1) || (timeout 15 traceroute -n -m 20 -q 1 -w 1 %s 2>&1)", quotedTarget, quotedTarget)

	case ProbeTypePort:
		probeTimeout = 8 * time.Second
		// ponytail: Bash built-in /dev/tcp gives zero-dependency TCP socket probe without requiring nc or nmap.
		cmd = fmt.Sprintf("timeout 3 bash -c '</dev/tcp/%s/%d' 2>/dev/null && echo 'PORT_OPEN' || echo 'PORT_CLOSED'", req.TargetAddress, req.Port)

	case ProbeTypeDNS:
		probeTimeout = 8 * time.Second
		cmd = fmt.Sprintf("getent hosts %s 2>&1 || nslookup %s 2>&1", quotedTarget, quotedTarget)

	default:
		return fmt.Errorf("unsupported probe type: %s", req.Type)
	}

	probeCtx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()

	// Abort remote runner if context expires
	doneChan := make(chan struct{})
	defer close(doneChan)
	go func() {
		select {
		case <-probeCtx.Done():
			_ = runner.Close()
		case <-doneChan:
		}
	}()

	logger.Info("netdiag", sourceHost.Name, fmt.Sprintf("Starting %s probe to %s", req.Type, req.TargetAddress))

	// 7. Execute with streaming pipe
	pr, pw := io.Pipe()

	runErrChan := make(chan error, 1)
	go func() {
		err := runner.Stream(cmd, pw)
		_ = pw.CloseWithError(err)
		runErrChan <- err
	}()

	scanner := bufio.NewScanner(pr)
	var fullOutput strings.Builder
	var hops []*HopInfo
	hopsMap := make(map[int]*HopInfo)
	targetReached := false

	for scanner.Scan() {
		line := scanner.Text()
		fullOutput.WriteString(line)
		fullOutput.WriteString("\n")

		// Emit raw line for live CLI viewing
		eventSink(ProbeEvent{
			Type:    "raw",
			RawLine: line,
		})

		switch req.Type {
		case ProbeTypeTraceroute:
			if strings.Contains(strings.ToLower(line), "reached") {
				targetReached = true
			}
			hop := ParseTracepathOrTracerouteLine(line, req.TargetAddress)
			if hop != nil {
				// Keep best / latest hop reading
				existing, exists := hopsMap[hop.Hop]
				if !exists || (existing.Status == "timeout" && hop.Status != "timeout") || (hop.RTTMs > 0 && existing.RTTMs == 0) {
					hopsMap[hop.Hop] = hop
				}
				eventSink(ProbeEvent{
					Type: "hop",
					Hop:  hop,
				})
			}
		}
	}

	execErr := <-runErrChan
	if probeCtx.Err() != nil && !targetReached && len(hopsMap) == 0 && fullOutput.Len() == 0 {
		return errors.New("probe timed out after " + probeTimeout.String())
	}

	// 8. Finalize statistics & automated analysis
	switch req.Type {
	case ProbeTypeTraceroute:
		// Collect sorted hops
		maxHop := 0
		for h := range hopsMap {
			if h > maxHop {
				maxHop = h
			}
		}
		for i := 1; i <= maxHop; i++ {
			if h, ok := hopsMap[i]; ok {
				hops = append(hops, h)
			} else {
				hops = append(hops, &HopInfo{
					Hop:    i,
					Host:   "* * *",
					IP:     "",
					RTTMs:  0,
					Zone:   "timeout",
					Status: "timeout",
				})
			}
		}
		analysis := AnalyzeHops(hops, req.TargetAddress, targetReached)
		eventSink(ProbeEvent{
			Type:     "hop_analysis",
			Analysis: analysis,
		})

	case ProbeTypePing:
		stats := ParsePingOutput(fullOutput.String())
		eventSink(ProbeEvent{
			Type: "ping_stats",
			Ping: stats,
		})

	case ProbeTypePort:
		stats := ParsePortOutput(fullOutput.String(), req.Port)
		eventSink(ProbeEvent{
			Type: "port_stats",
			Port: stats,
		})

	case ProbeTypeDNS:
		stats := ParseDNSOutput(fullOutput.String(), req.TargetAddress)
		eventSink(ProbeEvent{
			Type: "dns_stats",
			DNS:  stats,
		})
	}

	eventSink(ProbeEvent{
		Type:    "done",
		Message: "Probe completed successfully",
	})

	_ = execErr
	return nil
}

// IsPrivateIP checks if an IP belongs to private/internal RFC blocks.
func IsPrivateIP(ip net.IP) bool {
	if ip == nil {
		return false
	}
	for _, block := range privateCIDRs {
		if block.Contains(ip) {
			return true
		}
	}
	return false
}

// StartAutoTriageScheduler runs the periodic background auto-triage loop.
// ponytail: Background goroutine checks due presets and handles state-change alerts + reminder dampening.
func (s *Service) StartAutoTriageScheduler(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.runAutoTriageCycle(ctx)
		}
	}
}

func (s *Service) runAutoTriageCycle(ctx context.Context) {
	enabledStr, _ := s.db.GetSetting("auto_triage_enabled")
	if enabledStr != "1" && enabledStr != "true" {
		return
	}

	intervalMin := 10
	if intStr, _ := s.db.GetSetting("auto_triage_interval_min"); intStr != "" {
		if v, err := strconv.Atoi(intStr); err == nil && v > 0 {
			intervalMin = v
		}
	}
	intervalDuration := time.Duration(intervalMin) * time.Minute

	// 1. Fetch presets with auto_triage_enabled = true
	presets, err := s.db.ListProbeTargetPresets()
	if err != nil {
		return
	}

	// 2. Fetch default source host id from settings
	var defaultHostID int64
	if defStr, _ := s.db.GetSetting("auto_triage_default_host_id"); defStr != "" {
		defaultHostID, _ = strconv.ParseInt(defStr, 10, 64)
	}

	hosts, _ := s.db.ListHosts()
	var fallbackHostID int64
	for _, h := range hosts {
		if !strings.EqualFold(h.Status, "down") {
			fallbackHostID = h.ID
			break
		}
	}

	for _, p := range presets {
		if !p.AutoTriageEnabled {
			continue
		}

		// Determine source host
		sourceHostID := defaultHostID
		if p.SourceHostID != nil && *p.SourceHostID > 0 {
			sourceHostID = *p.SourceHostID
		}
		if sourceHostID <= 0 {
			sourceHostID = fallbackHostID
		}
		if sourceHostID <= 0 {
			continue
		}

		s.stateMu.Lock()
		state := s.targetStates[p.ID]
		if state == nil {
			state = &TargetState{}
			s.targetStates[p.ID] = state
		}
		timeSinceAlert := time.Since(state.LastAlertTime)
		s.stateMu.Unlock()

		// Check interval: don't run more frequently than intervalDuration unless state is unknown
		if state.LastVerdict != "" && time.Since(state.LastAlertTime) < intervalDuration && state.LastVerdict == VerdictHealthy {
			continue
		}

		// Execute triage
		req := TriageRequest{
			SourceHostID:  sourceHostID,
			PresetID:      p.ID,
			TargetAddress: p.Target,
			TargetName:    p.Name,
			AppEndpoint:   p.AppEndpoint,
			BaselineMTU:   p.BaselineMTU,
			MaxLatencyMs:  p.MaxLatencyMs,
			MaxLossPct:    p.MaxLossPct,
			DBPort:        p.DBPort,
		}

		result, err := s.ExecuteTriage(ctx, req, nil)
		if err != nil || result == nil {
			continue
		}

		s.stateMu.Lock()
		prevVerdict := state.LastVerdict
		state.LastVerdict = result.Verdict

		if result.Verdict != VerdictHealthy {
			// Incident condition!
			if prevVerdict == VerdictHealthy || prevVerdict == "" {
				// State change from Healthy -> Degraded: Send immediate alert!
				state.IncidentStartTime = time.Now()
				state.LastAlertTime = time.Now()
				if s.dispatcher != nil {
					s.dispatcher.SendTriageAlert(result.TicketSnippet, false)
				}
			} else if timeSinceAlert >= 60*time.Minute {
				// Still degraded after 60 min: Send reminder!
				state.LastAlertTime = time.Now()
				durationStr := time.Since(state.IncidentStartTime).Round(time.Minute).String()
				reminderTicket := fmt.Sprintf("⚠️ *[REMINDER INSIDEN MASIH AKTIF]*\n⏱️ *Durasi Gangguan*: %s\n\n%s", durationStr, result.TicketSnippet)
				if s.dispatcher != nil {
					s.dispatcher.SendTriageAlert(reminderTicket, false)
				}
			}
		} else {
			// Healthy condition!
			if prevVerdict != "" && prevVerdict != VerdictHealthy {
				// Recovered from degradation!
				durationStr := time.Since(state.IncidentStartTime).Round(time.Minute).String()
				var pingAvg float64
				var pingLoss float64
				if result.Ping != nil {
					pingAvg = result.Ping.AvgMs
					pingLoss = result.Ping.PacketLossPct
				}
				recoveryMsg := fmt.Sprintf("✅ *[PULIH: %s]*\nJalur & layanan menuju %s (%s) telah kembali NORMAL.\n⏱️ *Durasi Gangguan*: %s\n📡 *Status*: Latensi %.1f ms | Loss %.1f%% | Baseline MTU %d\n_Dihasilkan otomatis oleh Pantau Auto-Triage._",
					p.Name, p.Name, p.Target, durationStr, pingAvg, pingLoss, p.BaselineMTU)
				if s.dispatcher != nil {
					s.dispatcher.SendTriageAlert(recoveryMsg, true)
				}
				state.IncidentStartTime = time.Time{}
			}
			state.LastAlertTime = time.Now()
		}
		s.stateMu.Unlock()
	}
}
