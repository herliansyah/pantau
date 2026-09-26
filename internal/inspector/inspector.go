package inspector

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"pantau/internal/sshrunner"
	"pantau/internal/store"
)

var ErrAlreadyInspecting = errors.New("inspection already in progress for this host")

type Notifier interface {
	SendAlert(host *store.Host, rule *store.DesiredRule, summary, rootCause string)
}

type Inspector struct {
	db       *store.DB
	factory  sshrunner.RunnerFactory
	notifier Notifier
	inFlight sync.Map
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

// IsInspecting reports whether an inspection is currently in-flight for the given host.
func (ins *Inspector) IsInspecting(hostID int64) bool {
	_, loaded := ins.inFlight.Load(hostID)
	return loaded
}

// InFlightCount returns the number of hosts currently being inspected.
func (ins *Inspector) InFlightCount() int {
	count := 0
	ins.inFlight.Range(func(key, value any) bool {
		count++
		return true
	})
	return count
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
	return ins.InspectHostWithTimeout(hostID, 45*time.Second)
}

// InspectHostWithTimeout executes an inspection with a bounded timeout budget to prevent hanging on unreachable hosts.
func (ins *Inspector) InspectHostWithTimeout(hostID int64, timeout time.Duration) error {
	if _, loaded := ins.inFlight.LoadOrStore(hostID, struct{}{}); loaded {
		// ponytail: Concurrency guard / singleflight prevents overlapping inspections and SSH process stampedes.
		return ErrAlreadyInspecting
	}
	defer ins.inFlight.Delete(hostID)

	if timeout <= 0 {
		timeout = 45 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	startTime := time.Now()

	host, err := ins.db.GetHost(hostID)
	if err != nil {
		return err
	}
	if host == nil {
		return fmt.Errorf("host %d not found", hostID)
	}

	runner, err := ins.GetRunnerForHost(host)
	if err != nil {
		duration := time.Since(startTime).Milliseconds()
		host.Status = "down"
		host.LastDurationMs = duration
		host.LifecycleNotes = fmt.Sprintf("SSH connection failed: %v", err)
		_ = ins.db.UpdateHostInspection(host)
		_ = ins.db.RecordInspectionRun(&store.InspectionRun{
			HostID:     host.ID,
			StartedAt:  startTime,
			DurationMs: duration,
			Status:     "down",
			Summary:    "SSH connection failed",
			Details:    err.Error(),
		})
		return err
	}
	defer runner.Close()

	// Ensure runner connection is immediately aborted if context timeout expires, unblocking any hung commands.
	doneChan := make(chan struct{})
	defer close(doneChan)
	go func() {
		select {
		case <-doneChan:
		case <-ctx.Done():
			_ = runner.Close()
		}
	}()

	// 1. Inspect System Metrics
	if err := ins.scrapeSystemMetrics(host, runner); err != nil || ctx.Err() != nil {
		if err == nil {
			err = ctx.Err()
		}
		duration := time.Since(startTime).Milliseconds()
		status := "degraded"
		summary := "Failed to scrape system metrics"
		details := err.Error()
		if ctx.Err() == context.DeadlineExceeded {
			status = "down"
			summary = fmt.Sprintf("Inspection timed out after %v", timeout)
			details = "Inspection cancelled: context deadline exceeded"
		}
		host.Status = status
		host.LastDurationMs = duration
		_ = ins.db.UpdateHostInspection(host)
		_ = ins.db.RecordInspectionRun(&store.InspectionRun{
			HostID:     host.ID,
			StartedAt:  startTime,
			DurationMs: duration,
			Status:     status,
			Summary:    summary,
			Details:    details,
		})
		return err
	}

	// 2. Evaluate Desired State Rules & Detect Drift
	driftFound, driftSummaries, ruleCount, err := ins.evaluateDesiredRules(host, runner)
	if err != nil || ctx.Err() != nil {
		if err == nil {
			err = ctx.Err()
		}
		duration := time.Since(startTime).Milliseconds()
		status := "error"
		summary := "Error evaluating desired state rules"
		details := err.Error()
		if ctx.Err() == context.DeadlineExceeded {
			status = "down"
			summary = fmt.Sprintf("Inspection timed out after %v", timeout)
			details = "Inspection cancelled: context deadline exceeded"
		}
		host.Status = "degraded"
		if status == "down" {
			host.Status = "down"
		}
		host.LastDurationMs = duration
		_ = ins.db.UpdateHostInspection(host)
		_ = ins.db.RecordInspectionRun(&store.InspectionRun{
			HostID:     host.ID,
			StartedAt:  startTime,
			DurationMs: duration,
			Status:     status,
			Summary:    summary,
			Details:    details,
		})
		return err
	}

	duration := time.Since(startTime).Milliseconds()
	host.LastDurationMs = duration

	runStatus := "ok"
	summary := fmt.Sprintf("Healthy (%d rules checked)", ruleCount)
	details := ""
	if driftFound {
		host.Status = "degraded"
		runStatus = "drift"
		summary = fmt.Sprintf("Drift detected in %d rule(s)", len(driftSummaries))
		details = strings.Join(driftSummaries, "\n")
	} else {
		host.Status = "healthy"
		// ponytail: Auto-resolve/acknowledge active alerts when host recovers to fully healthy/desired state.
		_ = ins.db.AcknowledgeHostAlerts(host.ID)
	}

	_ = ins.db.RecordInspectionRun(&store.InspectionRun{
		HostID:     host.ID,
		StartedAt:  startTime,
		DurationMs: duration,
		Status:     runStatus,
		Summary:    summary,
		Details:    details,
	})

	return ins.db.UpdateHostInspection(host)
}

// SystemMetricsBatchCmd is the single-shot batch command to gather system, network, and security metrics.
// ponytail: Universal polyglot batching keeps agentless overhead near zero on modern & legacy Linux.
// ponytail: Uses df -lPk with timeout fallback to guard against hung NFS / network storage.
const SystemMetricsBatchCmd = `uname -r; echo "---"; cat /etc/os-release 2>/dev/null || cat /usr/lib/os-release 2>/dev/null || cat /etc/redhat-release 2>/dev/null || cat /etc/centos-release 2>/dev/null || cat /etc/issue 2>/dev/null; echo "---"; uptime; echo "---"; free -b 2>/dev/null; echo "---"; (timeout -k 2s 5s df -lPk / 2>/dev/null || df -lPk / 2>/dev/null); echo "---"; (dmesg 2>/dev/null || cat /var/log/dmesg 2>/dev/null) | grep -iE 'I/O error|EXT4-fs error|BTRFS error' | wc -l; echo "---"; cat /proc/net/dev 2>/dev/null | grep -vE 'lo|Inter-|face' | awk '{rx+=$2; tx+=$10} END {print rx, tx}'; echo "---"; (ping -c 1 -W 2 1.1.1.1 2>/dev/null | grep -oE 'time=[0-9.]+' | cut -d= -f2) || echo "OFFLINE"; echo "---"; curl -s --connect-timeout 2 -m 4 https://icanhazip.com 2>/dev/null || curl -s --connect-timeout 2 -m 4 https://ifconfig.me 2>/dev/null || echo ""; echo "---"; (ss -nt state established 2>/dev/null || ss -nt 2>/dev/null || netstat -nt 2>/dev/null) | awk '!/Recv-Q|Proto|Active/ {if (NF>=5) print $4, $5; else if (NF>=4) print $3, $4}' | head -n 50; echo "---"; (ss -tlpn 2>/dev/null || netstat -tlpn 2>/dev/null) | awk '!/State|Proto|Active/ {print $1, $4, $6}' | head -n 30; echo "---"; (grep -i "Failed password" /var/log/auth.log 2>/dev/null || grep -i "Failed password" /var/log/secure 2>/dev/null || true) | wc -l; echo "---"; (nproc 2>/dev/null || grep -c ^processor /proc/cpuinfo 2>/dev/null || echo 1); echo "---"; (cat /sys/class/dmi/id/bios_date 2>/dev/null || echo ""); echo "---"; (cat /sys/class/dmi/id/sys_vendor 2>/dev/null || cat /sys/class/dmi/id/product_name 2>/dev/null || echo ""); echo "---"; (stat -c %Y /etc/machine-id 2>/dev/null || stat -c %Y /etc/ssh/ssh_host_rsa_key 2>/dev/null || stat -c %Y /var/log 2>/dev/null || echo 0)`

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
		h.RAMUsedBytes, h.RAMTotalBytes, h.SwapUsedBytes, h.SwapTotalBytes = parseFree(sections[3])
	}
	if len(sections) >= 5 {
		h.DiskUsedBytes, h.DiskTotalBytes = parseDf(sections[4])
	}

	ioErrors := 0
	if len(sections) >= 6 {
		ioErrors, _ = strconv.Atoi(strings.TrimSpace(sections[5]))
	}

	cpuCores := 1
	if len(sections) >= 13 {
		if c, err := strconv.Atoi(strings.TrimSpace(sections[12])); err == nil && c > 0 {
			cpuCores = c
		}
	}
	h.CPUCores = cpuCores

	biosDateStr := ""
	if len(sections) >= 14 {
		biosDateStr = strings.TrimSpace(sections[13])
	}
	vendorStr := ""
	if len(sections) >= 15 {
		vendorStr = strings.TrimSpace(sections[14])
	}
	h.HardwareModel = vendorStr
	h.BIOSDate = biosDateStr
	osInstallEpoch := int64(0)
	if len(sections) >= 16 {
		osInstallEpoch, _ = strconv.ParseInt(strings.TrimSpace(sections[15]), 10, 64)
	}
	h.OSInstallEpoch = osInstallEpoch

	// Calculate Lifecycle Score (0-100) and transparent breakdown
	score, notes, breakdownJSON := calculateLifecycleScore(h.OSInfo, h.CPULoad, cpuCores, h.RAMUsedBytes, h.RAMTotalBytes, h.DiskUsedBytes, h.DiskTotalBytes, ioErrors, biosDateStr, vendorStr, osInstallEpoch, h.CommissionDate)
	h.LifecycleScore = score
	h.LifecycleNotes = notes
	h.LifecycleBreakdown = breakdownJSON

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
	// Fallback for non-systemd / legacy distributions (CentOS 6, Debian 7) without standard key=value os-release
	for _, line := range strings.Split(osRelease, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.Contains(line, "release") || strings.Contains(line, "Linux") || strings.Contains(line, "Debian") || strings.Contains(line, "Ubuntu") {
			return line
		}
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

func parseFree(output string) (ramUsed, ramTotal, swapUsed, swapTotal int64) {
	lines := strings.Split(output, "\n")
	var memTot, memUsd int64
	var swpTot, swpUsd int64
	var bufCacheUsed int64
	hasBufCache := false

	for _, l := range lines {
		l = strings.TrimSpace(l)
		if strings.HasPrefix(l, "Mem:") {
			fields := strings.Fields(l)
			if len(fields) >= 3 {
				memTot, _ = strconv.ParseInt(fields[1], 10, 64)
				memUsd, _ = strconv.ParseInt(fields[2], 10, 64)
			}
		} else if strings.HasPrefix(l, "Swap:") {
			fields := strings.Fields(l)
			if len(fields) >= 3 {
				swpTot, _ = strconv.ParseInt(fields[1], 10, 64)
				swpUsd, _ = strconv.ParseInt(fields[2], 10, 64)
			}
		} else if strings.Contains(l, "buffers/cache:") {
			// Legacy free format on pre-3.14 kernels (CentOS 6): "-/+ buffers/cache: used free"
			fields := strings.Fields(l)
			for i, f := range fields {
				if (f == "buffers/cache:" || strings.HasSuffix(f, "buffers/cache:")) && i+1 < len(fields) {
					if u, err := strconv.ParseInt(fields[i+1], 10, 64); err == nil {
						bufCacheUsed = u
						hasBufCache = true
						break
					}
				}
			}
		}
	}

	finalRamUsed := memUsd
	if hasBufCache && bufCacheUsed > 0 {
		finalRamUsed = bufCacheUsed
	}
	return finalRamUsed, memTot, swpUsd, swpTot
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

type LifecycleFactor struct {
	Name           string `json:"name"`
	Status         string `json:"status"` // "pass", "fail"
	ScoreDeduction int    `json:"score_deduction"`
	Detail         string `json:"detail"`
}

func ParseBIOSDate(s string) (time.Time, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, false
	}
	layouts := []string{"01/02/2006", "2006-01-02", "01/02/06", "2006/01/02", "02-01-2006"}
	for _, l := range layouts {
		if t, err := time.Parse(l, s); err == nil && t.Year() > 1990 && t.Year() <= time.Now().Year()+1 {
			return t, true
		}
	}
	return time.Time{}, false
}

func ParseCommissionDate(s string) (time.Time, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, false
	}
	t, err := time.Parse("2006-01-02", s)
	if err != nil || t.Year() < 1990 {
		return time.Time{}, false
	}
	return t, true
}

func IsFutureDate(t time.Time) bool {
	todayLocal := time.Now().Format("2006-01-02")
	todayUTC := time.Now().UTC().Format("2006-01-02")
	dateStr := t.Format("2006-01-02")
	return dateStr > todayLocal && dateStr > todayUTC
}

func IsVirtualSystem(vendor string) bool {
	v := strings.ToLower(vendor)
	virtualSignatures := []string{
		"qemu", "kvm", "vmware", "virtualbox", "xen",
		"microsoft corporation", "hyper-v",
		"amazon ec2", "google", "digitalocean", "hetzner",
		"vultr", "linode", "openstack", "proxmox", "bhyve",
	}
	for _, sig := range virtualSignatures {
		if strings.Contains(v, sig) {
			return true
		}
	}
	return false
}

func calculateLifecycleScore(osInfo, loadStr string, cpuCores int, ramUsed, ramTotal, diskUsed, diskTotal int64, ioErrors int, biosDateStr, vendorStr string, osInstallEpoch int64, commissionDateStr string) (int, string, string) {
	if cpuCores <= 0 {
		cpuCores = 1
	}
	score := 100
	var notes []string
	var factors []LifecycleFactor

	// 1. Check EOL OS signatures (-30 points)
	eolPatterns := []string{
		"Ubuntu 14.04", "Ubuntu 16.04", "Ubuntu 18.04",
		"Debian 7", "Debian 8", "Debian 9",
		"CentOS 6", "CentOS release 6", "CentOS Linux 6",
		"CentOS Linux 7", "CentOS release 7",
		"CentOS Linux 8", "CentOS release 8",
		"Red Hat Enterprise Linux Server release 6",
		"Red Hat Enterprise Linux Server release 7",
	}
	osEOL := false
	matchedEOL := ""
	for _, pat := range eolPatterns {
		if strings.Contains(osInfo, pat) {
			osEOL = true
			matchedEOL = pat
			break
		}
	}
	if osEOL {
		score -= 30
		notes = append(notes, "OS reached End-Of-Life ("+matchedEOL+")")
		factors = append(factors, LifecycleFactor{
			Name:           "OS Support",
			Status:         "fail",
			ScoreDeduction: -30,
			Detail:         fmt.Sprintf("OS reached End-Of-Life (%s)", matchedEOL),
		})
	} else {
		factors = append(factors, LifecycleFactor{
			Name:           "OS Support",
			Status:         "pass",
			ScoreDeduction: 0,
			Detail:         "Operating system is supported",
		})
	}

	// 2. Productive Lifespan / Hardware Age check (MTBF degradation zone)
	var ageYears float64 = -1
	var ageDetail string
	isVirt := IsVirtualSystem(vendorStr)

	var bt time.Time
	hasBT := false
	if !isVirt {
		if t, ok := ParseBIOSDate(biosDateStr); ok {
			bt = t
			hasBT = true
		}
	}

	var commTime time.Time
	hasValidComm := false
	commIgnoredReason := ""
	if strings.TrimSpace(commissionDateStr) != "" {
		if ct, ok := ParseCommissionDate(commissionDateStr); ok {
			if IsFutureDate(ct) {
				commIgnoredReason = fmt.Sprintf("Commission date %s ignored: future date", ct.Format("2006-01-02"))
			} else if hasBT && ct.Before(bt) {
				commIgnoredReason = fmt.Sprintf("Commission date %s ignored: earlier than BIOS (%s)", ct.Format("2006-01-02"), bt.Format("Jan 2006"))
			} else {
				commTime = ct
				hasValidComm = true
			}
		} else {
			commIgnoredReason = "Commission date format invalid (expected YYYY-MM-DD)"
		}
	}

	if hasValidComm {
		ageYears = time.Since(commTime).Hours() / (24 * 365.25)
		ageDetail = fmt.Sprintf("Commissioned: %s", commTime.Format("2006-01-02"))
		if hasBT {
			ageDetail += fmt.Sprintf(" • Motherboard BIOS: %s", bt.Format("Jan 2006"))
		}
	} else if !isVirt && hasBT {
		ageYears = time.Since(bt).Hours() / (24 * 365.25)
		ageDetail = fmt.Sprintf("Physical hardware BIOS: %s (~%.1f yrs)", bt.Format("Jan 2006"), ageYears)
		if commIgnoredReason != "" {
			ageDetail += fmt.Sprintf(" [%s]", commIgnoredReason)
		}
	}

	// If virtual or physical BIOS date missing/invalid (and no commission date), fallback to OS deployment age
	if ageYears < 0 && osInstallEpoch > 0 {
		installTime := time.Unix(osInstallEpoch, 0)
		if time.Since(installTime) > 0 {
			ageYears = time.Since(installTime).Hours() / (24 * 365.25)
			if isVirt {
				ageDetail = fmt.Sprintf("Virtual instance deployment age: ~%.1f yrs", ageYears)
			} else {
				ageDetail = fmt.Sprintf("OS installation deployment age: ~%.1f yrs", ageYears)
			}
			if commIgnoredReason != "" {
				ageDetail += fmt.Sprintf(" [%s]", commIgnoredReason)
			}
		}
	}

	if ageYears >= 8.0 {
		score -= 25
		msg := fmt.Sprintf("Exceeds 8-yr critical lifespan (%.1f yrs) — urgent hardware refresh recommended", ageYears)
		notes = append(notes, msg)
		factors = append(factors, LifecycleFactor{
			Name:           "Productive Lifespan",
			Status:         "fail",
			ScoreDeduction: -25,
			Detail:         msg + " (" + ageDetail + ")",
		})
	} else if ageYears >= 5.0 {
		score -= 15
		msg := fmt.Sprintf("Exceeds 5-yr productive lifecycle / MTBF risk zone (%.1f yrs)", ageYears)
		notes = append(notes, msg)
		factors = append(factors, LifecycleFactor{
			Name:           "Productive Lifespan",
			Status:         "warn",
			ScoreDeduction: -15,
			Detail:         msg + " (" + ageDetail + ")",
		})
	} else if ageYears >= 0 {
		factors = append(factors, LifecycleFactor{
			Name:           "Productive Lifespan",
			Status:         "pass",
			ScoreDeduction: 0,
			Detail:         fmt.Sprintf("Within normal productive lifespan (%.1f yrs; %s)", ageYears, ageDetail),
		})
	} else {
		factors = append(factors, LifecycleFactor{
			Name:           "Productive Lifespan",
			Status:         "pass",
			ScoreDeduction: 0,
			Detail:         "Hardware age telemetry unavailable",
		})
	}

	// 2. Memory pressure check (>92% = -20 points)
	if ramTotal > 0 {
		ramPct := float64(ramUsed) / float64(ramTotal) * 100
		if ramPct > 92.0 {
			score -= 20
			msg := fmt.Sprintf("Critical memory pressure (%.1f%% RAM utilized)", ramPct)
			notes = append(notes, msg)
			factors = append(factors, LifecycleFactor{
				Name:           "Memory Pressure",
				Status:         "fail",
				ScoreDeduction: -20,
				Detail:         msg,
			})
		} else {
			factors = append(factors, LifecycleFactor{
				Name:           "Memory Pressure",
				Status:         "pass",
				ScoreDeduction: 0,
				Detail:         fmt.Sprintf("Normal memory usage (%.1f%% RAM utilized)", ramPct),
			})
		}
	} else {
		factors = append(factors, LifecycleFactor{
			Name:           "Memory Pressure",
			Status:         "pass",
			ScoreDeduction: 0,
			Detail:         "Memory statistics unavailable",
		})
	}

	// 3. CPU Saturation check (load 1m / vCPU > 2.0 = -20 points)
	loadVal := 0.0
	if loadStr != "" {
		loads := strings.Split(loadStr, ",")
		if len(loads) >= 1 {
			loadVal, _ = strconv.ParseFloat(strings.TrimSpace(loads[0]), 64)
		}
	}
	loadRatio := loadVal / float64(cpuCores)
	if loadRatio > 2.0 {
		score -= 20
		msg := fmt.Sprintf("High CPU saturation (load %.2f on %d vCPU, ratio %.2f)", loadVal, cpuCores, loadRatio)
		notes = append(notes, msg)
		factors = append(factors, LifecycleFactor{
			Name:           "CPU Saturation",
			Status:         "fail",
			ScoreDeduction: -20,
			Detail:         msg,
		})
	} else {
		factors = append(factors, LifecycleFactor{
			Name:           "CPU Saturation",
			Status:         "pass",
			ScoreDeduction: 0,
			Detail:         fmt.Sprintf("CPU load within capacity (load %.2f on %d vCPU)", loadVal, cpuCores),
		})
	}

	// 4. Disk Capacity check (>90% = -20 points)
	if diskTotal > 0 {
		diskPct := float64(diskUsed) / float64(diskTotal) * 100
		if diskPct > 90.0 {
			score -= 20
			msg := fmt.Sprintf("Critical disk saturation (%.1f%% disk utilized)", diskPct)
			notes = append(notes, msg)
			factors = append(factors, LifecycleFactor{
				Name:           "Disk Capacity",
				Status:         "fail",
				ScoreDeduction: -20,
				Detail:         msg,
			})
		} else {
			factors = append(factors, LifecycleFactor{
				Name:           "Disk Capacity",
				Status:         "pass",
				ScoreDeduction: 0,
				Detail:         fmt.Sprintf("Normal disk usage (%.1f%% disk utilized)", diskPct),
			})
		}
	} else {
		factors = append(factors, LifecycleFactor{
			Name:           "Disk Capacity",
			Status:         "pass",
			ScoreDeduction: 0,
			Detail:         "Disk statistics unavailable",
		})
	}

	// 5. Hardware I/O disk errors (-40 points)
	if ioErrors > 0 {
		score -= 40
		msg := fmt.Sprintf("%d disk I/O hardware errors recorded in kernel ring buffer", ioErrors)
		notes = append(notes, msg)
		factors = append(factors, LifecycleFactor{
			Name:           "Hardware I/O",
			Status:         "fail",
			ScoreDeduction: -40,
			Detail:         msg,
		})
	} else {
		factors = append(factors, LifecycleFactor{
			Name:           "Hardware I/O",
			Status:         "pass",
			ScoreDeduction: 0,
			Detail:         "No hardware disk I/O errors detected",
		})
	}

	if score < 0 {
		score = 0
	}
	notesStr := "Healthy — Hardware and OS are in good standing"
	if len(notes) > 0 {
		notesStr = strings.Join(notes, "; ")
	}

	breakdownBytes, _ := json.Marshal(factors)
	return score, notesStr, string(breakdownBytes)
}

func (ins *Inspector) evaluateDesiredRules(h *store.Host, runner sshrunner.Runner) (bool, []string, int, error) {
	rules, err := ins.db.ListDesiredRules(h.ID)
	if err != nil {
		return false, nil, 0, err
	}

	driftFound := false
	var driftSummaries []string
	checkedCount := 0

	for _, rule := range rules {
		if !rule.Enabled {
			continue
		}
		checkedCount++

		current, status, details, rootCause := ins.evaluateRule(h, runner, &rule)
		if status == "drift" {
			driftFound = true
			msg := fmt.Sprintf("%s '%s': %s", rule.Kind, rule.Target, details)
			if rootCause != "" {
				msg += fmt.Sprintf(" (%s)", rootCause)
			}
			driftSummaries = append(driftSummaries, msg)
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

	return driftFound, driftSummaries, checkedCount, nil
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
		// ponytail: Use timeout wrapper to prevent hanging on stale NFS/remote mounts
		dfCmd := fmt.Sprintf(`timeout -k 2s 5s df -Pk %s 2>/dev/null || df -Pk %s 2>/dev/null`, target, target)
		out, _, exitCode, _ := runner.Exec(dfCmd)
		if exitCode == 124 {
			current = "unresponsive"
			status = "drift"
			rootCause = fmt.Sprintf("Storage/mount point '%s' timed out (possible hung NFS or storage deadlock)", target)
			return
		}
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
				// ponytail: Omit periodic heavy recursive du; df provides instant usage without saturating disk I/O.
				rootCause = fmt.Sprintf("Disk usage on %s is %d%%, exceeding threshold <%d%%", target, pct, limit)
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
		// Target is service name (e.g. "mysql", "mysqld", "nginx", "httpd")
		// Polyglot check supporting both systemd and SysVinit (CentOS 6, Debian 7)
		svcCmd := fmt.Sprintf(`sh -c 'if command -v systemctl >/dev/null 2>&1; then systemctl is-active "$1" 2>/dev/null; elif service "$1" status 2>&1 | grep -iq "running"; then echo "active"; else echo "inactive"; fi' -- %s`, rule.Target)
		out, _, _, _ := runner.Exec(svcCmd)
		activeState := strings.TrimSpace(out)
		current = activeState
		if activeState == "active" {
			status = "ok"
		} else {
			status = "drift"
			diagCmd := fmt.Sprintf(`sh -c 'if command -v journalctl >/dev/null 2>&1; then journalctl -u "$1" -n 50 --no-pager 2>&1; elif [ -f "/var/log/$1.log" ]; then tail -n 50 "/var/log/$1.log" 2>&1; elif [ -f "/var/log/$1/error.log" ]; then tail -n 50 "/var/log/$1/error.log" 2>&1; else tail -n 50 /var/log/messages 2>&1 || tail -n 50 /var/log/syslog 2>&1; fi' -- %s`, rule.Target)
			diagOut, _, _, _ := runner.Exec(diagCmd)
			rootCause = fmt.Sprintf("Service %s is %s (expected active)\n--- Service diagnostics ---\n%s", rule.Target, activeState, strings.TrimSpace(diagOut))
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

	// 3. Known DB & Web services (supporting systemd and SysVinit / RedHat naming conventions)
	for _, svc := range []string{"mysql", "mysqld", "mariadb", "postgresql", "redis-server", "redis", "nginx", "httpd", "apache2"} {
		svcCheck := fmt.Sprintf(`sh -c 'if command -v systemctl >/dev/null 2>&1; then systemctl is-active "$1" 2>/dev/null; elif service "$1" status 2>&1 | grep -iq "running"; then echo "active"; else echo "inactive"; fi' -- %s`, svc)
		out, _, _, _ := runner.Exec(svcCheck)
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

func (ins *Inspector) RecalculateHostLifecycle(hostID int64) error {
	host, err := ins.db.GetHost(hostID)
	if err != nil || host == nil {
		return err
	}
	if host.LastInspected == nil && host.OSInfo == "" {
		return nil
	}
	// ponytail: RecalculateHostLifecycle performs offline re-assessment from cached metrics.
	// Substring scan for "Hardware I/O" status is an O(1) heuristic to preserve kernel ring-buffer error state
	// without full JSON unmarshaling overhead. Ceiling: 1-factor check. Upgrade path: unmarshal LifecycleFactor array.
	ioErrors := 0
	if strings.Contains(host.LifecycleBreakdown, "Hardware I/O") && strings.Contains(host.LifecycleBreakdown, `"status":"fail"`) {
		ioErrors = 1
	}
	score, notes, breakdown := calculateLifecycleScore(
		host.OSInfo, host.CPULoad, host.CPUCores,
		host.RAMUsedBytes, host.RAMTotalBytes,
		host.DiskUsedBytes, host.DiskTotalBytes,
		ioErrors, host.BIOSDate, host.HardwareModel, host.OSInstallEpoch,
		host.CommissionDate,
	)
	return ins.db.UpdateHostLifecycle(host.ID, score, notes, breakdown)
}

