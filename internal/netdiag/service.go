package netdiag

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"regexp"
	"strings"
	"sync"
	"time"

	"pantau/internal/inspector"
	"pantau/internal/logger"
	"pantau/internal/store"
)

var (
	validTargetRegex = regexp.MustCompile(`^[a-zA-Z0-9_.:-]+$`)
	ErrBusy          = errors.New("a diagnostic probe is already in progress for this host")
)

type Service struct {
	db        *store.DB
	inspector *inspector.Inspector
	inFlight  sync.Map // sourceHostID -> struct{}{}
}

func NewService(db *store.DB, ins *inspector.Inspector) *Service {
	return &Service{
		db:        db,
		inspector: ins,
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
