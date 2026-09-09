package inspector

import (
	"encoding/json"
	"fmt"
	"math"
	"net"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"pantau/internal/sshrunner"
	"pantau/internal/store"
)

type Notifier interface {
	SendAlert(host *store.Host, rule *store.DesiredRule, summary, rootCause string)
}

type Inspector struct {
	db       *store.DB
	factory  sshrunner.RunnerFactory
	notifier Notifier
}

func New(db *store.DB, factory sshrunner.RunnerFactory, notifier Notifier) *Inspector {
	if factory == nil {
		factory = sshrunner.DefaultFactory()
	}
	return &Inspector{
		db:       db,
		factory:  factory,
		notifier: notifier,
	}
}

func (ins *Inspector) GetRunnerForHost(h *store.Host) (sshrunner.Runner, error) {
	key := h.CustomKey
	if strings.TrimSpace(key) == "" {
		globalKey, err := ins.db.GetSetting("ssh_private_key")
		if err != nil {
			return nil, fmt.Errorf("get global ssh key: %w", err)
		}
		key = globalKey
	}
	return ins.factory(h.Host, h.Port, h.User, key)
}

// InspectHost performs a full health check, metrics scrape, and drift detection on a single host.
func (ins *Inspector) InspectHost(hostID int64) error {
	host, err := ins.db.GetHost(hostID)
	if err != nil {
		return err
	}
	if host == nil {
		return fmt.Errorf("host %d not found", hostID)
	}

	runner, err := ins.GetRunnerForHost(host)
	if err != nil {
		host.Status = "down"
		host.LifecycleNotes = fmt.Sprintf("SSH connection failed: %v", err)
		_ = ins.db.UpdateHostInspection(host)
		return err
	}
	defer runner.Close()

	// 1. Inspect System Metrics
	if err := ins.scrapeSystemMetrics(host, runner); err != nil {
		host.Status = "degraded"
		_ = ins.db.UpdateHostInspection(host)
		return err
	}

	// 2. Evaluate Desired State Rules & Detect Drift
	driftFound, err := ins.evaluateDesiredRules(host, runner)
	if err != nil {
		return err
	}

	if driftFound {
		host.Status = "degraded"
	} else {
		host.Status = "healthy"
	}

	return ins.db.UpdateHostInspection(host)
}

// SystemMetricsBatchCmd is the single-shot batch command to gather system, network, and security metrics.
// ponytail: Batching commands into a single roundtrip keeps agentless overhead near zero.
const SystemMetricsBatchCmd = `uname -r; echo "---"; cat /etc/os-release 2>/dev/null || cat /usr/lib/os-release 2>/dev/null; echo "---"; uptime; echo "---"; free -b 2>/dev/null; echo "---"; df -Pk / 2>/dev/null; echo "---"; dmesg --level=err,crit 2>/dev/null | grep -iE 'I/O error|EXT4-fs error|BTRFS error' | wc -l; echo "---"; cat /proc/net/dev 2>/dev/null | grep -vE 'lo|Inter-|face' | awk '{rx+=$2; tx+=$10} END {print rx, tx}'; echo "---"; (ping -c 1 -W 2 1.1.1.1 2>/dev/null | grep -oE 'time=[0-9.]+' | cut -d= -f2) || echo "OFFLINE"; echo "---"; curl -s --connect-timeout 2 https://icanhazip.com 2>/dev/null || curl -s --connect-timeout 2 https://ifconfig.me 2>/dev/null || echo ""; echo "---"; ss -H -nt state established 2>/dev/null | awk '{print $4, $5}' | head -n 50; echo "---"; ss -H -tlpn 2>/dev/null | awk '{print $1, $4, $6}' | head -n 30; echo "---"; (grep -i "Failed password" /var/log/auth.log 2>/dev/null || grep -i "Failed password" /var/log/secure 2>/dev/null || true) | wc -l`

func (ins *Inspector) scrapeSystemMetrics(h *store.Host, runner sshrunner.Runner) error {
	stdout, _, _, err := runner.Exec(SystemMetricsBatchCmd)
	if err != nil {
		return fmt.Errorf("exec metrics batch: %w", err)
	}

	sections := strings.Split(stdout, "---")
	if len(sections) >= 1 {
		h.Kernel = strings.TrimSpace(sections[0])
	}
	if len(sections) >= 2 {
		h.OSInfo = parseOSInfo(sections[1])
	}
	if len(sections) >= 3 {
		uptimeOut := strings.TrimSpace(sections[2])
		h.Uptime, h.CPULoad = parseUptime(uptimeOut)
	}
	if len(sections) >= 4 {
		h.RAMUsedBytes, h.RAMTotalBytes = parseFree(sections[3])
	}
	if len(sections) >= 5 {
		h.DiskUsedBytes, h.DiskTotalBytes = parseDf(sections[4])
	}

	ioErrors := 0
	if len(sections) >= 6 {
		ioErrors, _ = strconv.Atoi(strings.TrimSpace(sections[5]))
	}

	// Calculate Lifecycle Score (0-100)
	score, notes := calculateLifecycleScore(h.OSInfo, h.CPULoad, h.RAMUsedBytes, h.RAMTotalBytes, ioErrors)
	h.LifecycleScore = score
	h.LifecycleNotes = notes

	// Parse Network & Security Observability Metrics
	var netDevSec, pingSec, pubIPSec, estSec, listenSec, failedSec string
	if len(sections) >= 7 {
		netDevSec = sections[6]
	}
	if len(sections) >= 8 {
		pingSec = sections[7]
	}
	if len(sections) >= 9 {
		pubIPSec = sections[8]
	}
	if len(sections) >= 10 {
		estSec = sections[9]
	}
	if len(sections) >= 11 {
		listenSec = sections[10]
	}
	if len(sections) >= 12 {
		failedSec = sections[11]
	}
	parseNetworkMetrics(h, netDevSec, pingSec, pubIPSec, estSec, listenSec, failedSec)

	return nil
}

func parseNetworkMetrics(h *store.Host, netDevSec, pingSec, pubIPSec, estSec, listenSec, failedSec string) {
	// 1. Bandwidth & Transfer
	fields := strings.Fields(strings.TrimSpace(netDevSec))
	if len(fields) >= 2 {
		rx, _ := strconv.ParseInt(fields[0], 10, 64)
		tx, _ := strconv.ParseInt(fields[1], 10, 64)

		if h.LastInspected != nil && h.NetRxBytes > 0 {
			elapsed := time.Since(*h.LastInspected).Seconds()
			if elapsed > 0 {
				deltaRx := rx - h.NetRxBytes
				deltaTx := tx - h.NetTxBytes
				if deltaRx >= 0 {
					h.NetRxSpeedBps = int64(float64(deltaRx) / elapsed)
				}
				if deltaTx >= 0 {
					h.NetTxSpeedBps = int64(float64(deltaTx) / elapsed)
				}
			}
		}
		h.NetRxBytes = rx
		h.NetTxBytes = tx
	}

	// 2. Internet Egress & Latency
	pingOut := strings.TrimSpace(pingSec)
	if pingOut == "" || strings.EqualFold(pingOut, "OFFLINE") {
		h.InternetOnline = false
		h.InternetLatencyMs = 0
	} else {
		h.InternetOnline = true
		latFloat, _ := strconv.ParseFloat(pingOut, 64)
		h.InternetLatencyMs = int64(math.Round(latFloat))
	}

	// 3. Public IP
	h.PublicIP = strings.TrimSpace(pubIPSec)

	// 4. Established Connections & Top Remote IPs
	ipCounts := make(map[string]int)
	totalActive := 0
	for _, line := range strings.Split(estSec, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) >= 2 {
			totalActive++
			remote := parts[1]
			host, _, err := net.SplitHostPort(remote)
			if err != nil {
				host = remote
			}
			if host != "" {
				ipCounts[host]++
			}
		}
	}
	h.ActiveConnCount = totalActive

	type ipCountPair struct {
		ip    string
		count int
	}
	var pairs []ipCountPair
	for ip, count := range ipCounts {
		pairs = append(pairs, ipCountPair{ip: ip, count: count})
	}
	sort.Slice(pairs, func(i, j int) bool {
		return pairs[i].count > pairs[j].count
	})

	topConns := []store.TopConn{}
	limit := 10
	if len(pairs) < limit {
		limit = len(pairs)
	}
	for i := 0; i < limit; i++ {
		topConns = append(topConns, store.TopConn{
			RemoteIP: pairs[i].ip,
			Count:    pairs[i].count,
		})
	}
	topBytes, _ := json.Marshal(topConns)
	h.TopConnections = string(topBytes)

	// 5. Listening Ports & Public Exposure
	sensitivePorts := map[string]bool{
		"3306": true, "5432": true, "6379": true, "27017": true,
		"9200": true, "2375": true, "11211": true,
	}
	listening := []store.ListeningPort{}
	for _, line := range strings.Split(listenSec, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) >= 2 {
			addr := parts[1] // e.g. 0.0.0.0:80, *:22, 127.0.0.1:3306
			proc := ""
			if len(parts) >= 3 {
				proc = parts[2]
			}
			host, port, err := net.SplitHostPort(addr)
			if err != nil {
				continue
			}
			isPublic := host == "0.0.0.0" || host == "*" || host == "::" || host == ""
			risk := "low"
			if isPublic && sensitivePorts[port] {
				risk = "high"
			}
			listening = append(listening, store.ListeningPort{
				Proto:   "tcp",
				Port:    port,
				Address: addr,
				Public:  isPublic,
				Process: proc,
				Risk:    risk,
			})
		}
	}
	listenBytes, _ := json.Marshal(listening)
	h.ListeningPorts = string(listenBytes)

	// 6. Failed Logins Count
	h.FailedLoginsCount, _ = strconv.Atoi(strings.TrimSpace(failedSec))
}

func parseOSInfo(osRelease string) string {
	var prettyName, name, version string
	for _, line := range strings.Split(osRelease, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "PRETTY_NAME=") {
			prettyName = strings.Trim(strings.TrimPrefix(line, "PRETTY_NAME="), `"`)
		} else if strings.HasPrefix(line, "NAME=") {
			name = strings.Trim(strings.TrimPrefix(line, "NAME="), `"`)
		} else if strings.HasPrefix(line, "VERSION=") {
			version = strings.Trim(strings.TrimPrefix(line, "VERSION="), `"`)
		}
	}
	if prettyName != "" {
		return prettyName
	}
	if name != "" && version != "" {
		return name + " " + version
	}
	if name != "" {
		return name
	}
	return "Linux"
}

func parseUptime(line string) (uptimeStr, loadStr string) {
	// e.g. " 14:00:01 up 12 days,  3:15,  1 user,  load average: 0.15, 0.08, 0.05"
	parts := strings.Split(line, "load average:")
	if len(parts) == 2 {
		loadStr = strings.TrimSpace(parts[1])
	}
	upParts := strings.Split(parts[0], "up ")
	if len(upParts) == 2 {
		uptimeRaw := strings.Split(upParts[1], ",")
		if len(uptimeRaw) >= 2 {
			uptimeStr = strings.TrimSpace(uptimeRaw[0] + ", " + uptimeRaw[1])
		} else {
			uptimeStr = strings.TrimSpace(uptimeRaw[0])
		}
	} else {
		uptimeStr = line
	}
	return uptimeStr, loadStr
}

func parseFree(output string) (used, total int64) {
	lines := strings.Split(output, "\n")
	for _, l := range lines {
		if strings.HasPrefix(l, "Mem:") {
			fields := strings.Fields(l)
			if len(fields) >= 3 {
				tot, _ := strconv.ParseInt(fields[1], 10, 64)
				usd, _ := strconv.ParseInt(fields[2], 10, 64)
				return usd, tot
			}
		}
	}
	return 0, 0
}

func parseDf(output string) (used, total int64) {
	lines := strings.Split(output, "\n")
	for _, l := range lines {
		fields := strings.Fields(l)
		if len(fields) >= 6 && (fields[5] == "/" || strings.HasSuffix(fields[5], "/")) {
			totK, _ := strconv.ParseInt(fields[1], 10, 64)
			usdK, _ := strconv.ParseInt(fields[2], 10, 64)
			return usdK * 1024, totK * 1024
		}
	}
	return 0, 0
}

func calculateLifecycleScore(osInfo, loadStr string, ramUsed, ramTotal int64, ioErrors int) (int, string) {
	score := 100
	var notes []string

	// Check EOL OS signatures
	eolPatterns := []string{"Ubuntu 14.04", "Ubuntu 16.04", "Ubuntu 18.04", "Debian 8", "Debian 9", "CentOS Linux 7", "CentOS Linux 8"}
	for _, pat := range eolPatterns {
		if strings.Contains(osInfo, pat) {
			score -= 30
			notes = append(notes, "OS reached End-Of-Life ("+pat+")")
			break
		}
	}

	// Sustained RAM pressure
	if ramTotal > 0 {
		pct := float64(ramUsed) / float64(ramTotal) * 100
		if pct > 92.0 {
			score -= 20
			notes = append(notes, fmt.Sprintf("Critical memory pressure (%.1f%% RAM utilized)", pct))
		}
	}

	// Load average check
	if loadStr != "" {
		loads := strings.Split(loadStr, ",")
		if len(loads) >= 1 {
			val, _ := strconv.ParseFloat(strings.TrimSpace(loads[0]), 64)
			if val > 8.0 {
				score -= 20
				notes = append(notes, fmt.Sprintf("High CPU load average (%.2f)", val))
			}
		}
	}

	// Hardware I/O disk errors
	if ioErrors > 0 {
		score -= 40
		notes = append(notes, fmt.Sprintf("%d disk I/O hardware errors recorded in kernel ring buffer", ioErrors))
	}

	if score < 0 {
		score = 0
	}
	if len(notes) == 0 {
		return score, "Healthy — Hardware and OS are in good standing"
	}
	return score, strings.Join(notes, "; ")
}

func (ins *Inspector) evaluateDesiredRules(h *store.Host, runner sshrunner.Runner) (bool, error) {
	rules, err := ins.db.ListDesiredRules(h.ID)
	if err != nil {
		return false, err
	}

	driftFound := false

	for _, rule := range rules {
		if !rule.Enabled {
			continue
		}

		current, status, details, rootCause := ins.evaluateRule(h, runner, &rule)
		if status == "drift" {
			driftFound = true
		}

		// Check previous status for incident recording
		existingItems, _ := ins.db.ListActualItems(h.ID)
		var prevStatus string
		for _, ex := range existingItems {
			if ex.Kind == rule.Kind && ex.Target == rule.Target {
				prevStatus = ex.Status
				break
			}
		}

		// Record actual item state
		actualItem := &store.ActualItem{
			HostID:  h.ID,
			Kind:    rule.Kind,
			Target:  rule.Target,
			Current: current,
			Status:  status,
			Details: details,
		}
		_ = ins.db.UpsertActualItem(actualItem)

		// Transition into drift -> record incident + alert + root cause excerpt
		if status == "drift" && prevStatus != "drift" {
			inc := &store.Incident{
				HostID:           h.ID,
				Kind:             rule.Kind,
				Target:           rule.Target,
				EventType:        "drift_detected",
				Summary:          fmt.Sprintf("Drift detected: %s '%s' is %s (expected %s)", rule.Kind, rule.Target, current, rule.Expected),
				RootCauseExcerpt: rootCause,
			}
			_ = ins.db.RecordIncident(inc)

			alt := &store.Alert{
				HostID:    h.ID,
				Level:     "critical",
				Message:   inc.Summary,
				RootCause: rootCause,
			}
			_, _ = ins.db.CreateAlert(alt)

			if ins.notifier != nil {
				ins.notifier.SendAlert(h, &rule, inc.Summary, rootCause)
			}
		} else if status == "ok" && prevStatus == "drift" {
			// Drift resolved
			inc := &store.Incident{
				HostID:           h.ID,
				Kind:             rule.Kind,
				Target:           rule.Target,
				EventType:        "drift_resolved",
				Summary:          fmt.Sprintf("Resolved: %s '%s' is back to desired state (%s)", rule.Kind, rule.Target, current),
				RootCauseExcerpt: "",
			}
			_ = ins.db.RecordIncident(inc)
		}
	}

	return driftFound, nil
}

func (ins *Inspector) evaluateRule(h *store.Host, runner sshrunner.Runner, rule *store.DesiredRule) (current, status, details, rootCause string) {
	switch rule.Kind {
	case "container":
		// Check docker container status
		inspectCmd := fmt.Sprintf(`docker inspect -f '{{.State.Status}}|{{.State.ExitCode}}|{{.State.OOMKilled}}|{{.State.Error}}' %s 2>/dev/null`, rule.Target)
		out, _, exitCode, _ := runner.Exec(inspectCmd)
		out = strings.TrimSpace(out)
		if exitCode != 0 || out == "" {
			current = "not_found"
			status = "drift"
			rootCause = fmt.Sprintf("Container %s does not exist on host", rule.Target)
			return
		}

		parts := strings.Split(out, "|")
		stateStatus := parts[0]
		current = stateStatus
		if strings.EqualFold(stateStatus, rule.Expected) {
			status = "ok"
		} else {
			status = "drift"
			// Extract Root Cause Excerpt
			var diag strings.Builder
			if len(parts) >= 4 {
				diag.WriteString(fmt.Sprintf("ExitCode: %s, OOMKilled: %s, Error: %s\n", parts[1], parts[2], parts[3]))
			}
			logOut, _, _, _ := runner.Exec(fmt.Sprintf(`docker logs --tail 50 %s 2>&1`, rule.Target))
			diag.WriteString("--- Last 50 lines of container logs ---\n")
			diag.WriteString(strings.TrimSpace(logOut))
			rootCause = diag.String()
		}
		return

	case "disk":
		// Target is mount point (e.g. "/")
		target := rule.Target
		if target == "" {
			target = "/"
		}
		dfCmd := fmt.Sprintf(`df -Pk %s 2>/dev/null`, target)
		out, _, exitCode, _ := runner.Exec(dfCmd)
		if exitCode != 0 {
			current = "unreachable"
			status = "drift"
			rootCause = fmt.Sprintf("Failed to query filesystem mount: %s", target)
			return
		}
		used, total := parseDf(out)
		if total > 0 {
			pct := int(float64(used) / float64(total) * 100)
			current = fmt.Sprintf("%d%%", pct)

			// Expected format like "<85%"
			limit := 85
			cleanExp := strings.TrimPrefix(strings.TrimSuffix(rule.Expected, "%"), "<")
			if l, err := strconv.Atoi(cleanExp); err == nil {
				limit = l
			}

			if pct < limit {
				status = "ok"
			} else {
				status = "drift"
				// Top directory usage diagnostic
				topUsage, _, _, _ := runner.Exec(`du -sh /* 2>/dev/null | sort -rh | head -n 10`)
				rootCause = fmt.Sprintf("Disk usage %d%% exceeded limit <%d%%\n--- Top space consumers ---\n%s", pct, limit, topUsage)
			}
		} else {
			current = "unknown"
			status = "drift"
			rootCause = "Could not parse mount point usage"
		}
		return

	case "backup":
		// Target is file path or glob (e.g. /var/backups/db-*.sql.gz)
		statCmd := fmt.Sprintf(`stat -c "%%s %%Y" %s 2>/dev/null || ls -l %s 2>/dev/null`, rule.Target, rule.Target)
		out, _, exitCode, _ := runner.Exec(statCmd)
		out = strings.TrimSpace(out)
		if exitCode != 0 || out == "" {
			current = "missing"
			status = "drift"
			rootCause = fmt.Sprintf("Backup file does not exist at path: %s", rule.Target)
			return
		}

		fields := strings.Fields(out)
		if len(fields) >= 2 {
			sizeBytes, _ := strconv.ParseInt(fields[0], 10, 64)
			mtimeUnix, _ := strconv.ParseInt(fields[1], 10, 64)
			modTime := time.Unix(mtimeUnix, 0)
			ageHours := time.Since(modTime).Hours()

			details = fmt.Sprintf("Size: %d bytes, Modified: %s (%.1fh ago)", sizeBytes, modTime.Format(time.RFC3339), ageHours)

			if sizeBytes == 0 {
				current = "empty_file (0 bytes)"
				status = "drift"
				rootCause = fmt.Sprintf("Backup file exists but is 0 bytes empty at %s", rule.Target)
				return
			}

			// Expected freshness window (default 24h)
			maxHours := 24.0
			if strings.HasPrefix(rule.Expected, "fresh_") {
				raw := strings.TrimSuffix(strings.TrimPrefix(rule.Expected, "fresh_"), "h")
				if h, err := strconv.ParseFloat(raw, 64); err == nil {
					maxHours = h
				}
			}

			if ageHours <= maxHours {
				current = fmt.Sprintf("fresh (%.1fh ago, %d B)", ageHours, sizeBytes)
				status = "ok"
			} else {
				current = fmt.Sprintf("stale (%.1fh ago)", ageHours)
				status = "drift"
				rootCause = fmt.Sprintf("Backup file at %s is stale (age: %.1f hours, max allowed: %.1f hours)", rule.Target, ageHours, maxHours)
			}
		} else {
			current = "stat_error"
			status = "drift"
			rootCause = "Could not parse backup file metadata"
		}
		return

	case "cron":
		// Target is keyword or command in crontab
		cronCmd := `crontab -l 2>/dev/null`
		out, _, _, _ := runner.Exec(cronCmd)
		if strings.Contains(out, rule.Target) {
			current = "configured"
			status = "ok"
		} else {
			current = "missing"
			status = "drift"
			rootCause = fmt.Sprintf("Expected crontab entry containing '%s' was not found in user crontab", rule.Target)
		}
		return

	case "service":
		// Target is systemd service name (e.g. "mysql", "nginx")
		svcCmd := fmt.Sprintf(`systemctl is-active %s 2>/dev/null`, rule.Target)
		out, _, _, _ := runner.Exec(svcCmd)
		activeState := strings.TrimSpace(out)
		current = activeState
		if activeState == "active" {
			status = "ok"
		} else {
			status = "drift"
			diagOut, _, _, _ := runner.Exec(fmt.Sprintf(`journalctl -u %s -n 50 --no-pager 2>&1`, rule.Target))
			rootCause = fmt.Sprintf("Service %s is %s (expected active)\n--- Last 50 lines of journalctl ---\n%s", rule.Target, activeState, strings.TrimSpace(diagOut))
		}
		return
	}

	return "unknown", "ok", "", ""
}

// GenerateBaseline autodetects containers, disks, services, and cron jobs and creates DesiredRules.
func (ins *Inspector) GenerateBaseline(hostID int64) ([]store.DesiredRule, error) {
	host, err := ins.db.GetHost(hostID)
	if err != nil {
		return nil, err
	}
	if host == nil {
		return nil, fmt.Errorf("host %d not found", hostID)
	}

	runner, err := ins.GetRunnerForHost(host)
	if err != nil {
		return nil, err
	}
	defer runner.Close()

	var rules []store.DesiredRule

	// 1. Root disk rule
	diskRule := store.DesiredRule{
		HostID:   hostID,
		Kind:     "disk",
		Target:   "/",
		Expected: "<85%",
		Enabled:  true,
	}
	_ = ins.db.SaveDesiredRule(&diskRule)
	rules = append(rules, diskRule)

	// 2. Running Docker containers
	dockerOut, _, code, _ := runner.Exec(`docker ps --format '{{.Names}}' 2>/dev/null`)
	if code == 0 && dockerOut != "" {
		for _, name := range strings.Split(strings.TrimSpace(dockerOut), "\n") {
			name = strings.TrimSpace(name)
			if name != "" {
				r := store.DesiredRule{
					HostID:   hostID,
					Kind:     "container",
					Target:   name,
					Expected: "running",
					Enabled:  true,
				}
				_ = ins.db.SaveDesiredRule(&r)
				rules = append(rules, r)
			}
		}
	}

	// 3. Known DB services
	for _, svc := range []string{"mysql", "mariadb", "postgresql", "redis-server", "nginx"} {
		out, _, _, _ := runner.Exec(fmt.Sprintf(`systemctl is-active %s 2>/dev/null`, svc))
		if strings.TrimSpace(out) == "active" {
			r := store.DesiredRule{
				HostID:   hostID,
				Kind:     "service",
				Target:   svc,
				Expected: "active",
				Enabled:  true,
			}
			_ = ins.db.SaveDesiredRule(&r)
			rules = append(rules, r)
		}
	}

	// 4. Crontab
	crontabOut, _, _, _ := runner.Exec(`crontab -l 2>/dev/null`)
	for _, line := range strings.Split(strings.TrimSpace(crontabOut), "\n") {
		line = strings.TrimSpace(line)
		if line != "" && !strings.HasPrefix(line, "#") {
			// Extract command part
			fields := strings.Fields(line)
			if len(fields) >= 6 {
				cmd := strings.Join(fields[5:], " ")
				// shorten target
				target := cmd
				if len(target) > 40 {
					target = target[:40]
				}
				r := store.DesiredRule{
					HostID:   hostID,
					Kind:     "cron",
					Target:   target,
					Expected: "configured",
					Enabled:  true,
				}
				_ = ins.db.SaveDesiredRule(&r)
				rules = append(rules, r)
			}
		}
	}

	return rules, nil
}

// DockerActions
func (ins *Inspector) DockerAction(hostID int64, containerName, action string) (string, error) {
	if action != "start" && action != "stop" && action != "restart" {
		return "", fmt.Errorf("unsupported action: %s", action)
	}

	// Basic safety check on container name
	if matched, _ := regexp.MatchString(`^[a-zA-Z0-9_\.\-]+$`, containerName); !matched {
		return "", fmt.Errorf("invalid container name: %s", containerName)
	}

	host, err := ins.db.GetHost(hostID)
	if err != nil || host == nil {
		return "", fmt.Errorf("host not found")
	}

	runner, err := ins.GetRunnerForHost(host)
	if err != nil {
		return "", err
	}
	defer runner.Close()

	cmd := fmt.Sprintf("docker %s %s", action, containerName)
	stdout, stderr, exitCode, err := runner.Exec(cmd)
	if err != nil || exitCode != 0 {
		return "", fmt.Errorf("docker %s failed (exit %d): %s %s", action, exitCode, stdout, stderr)
	}

	return stdout, nil
}
