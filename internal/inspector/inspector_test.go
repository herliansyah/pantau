package inspector

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"pantau/internal/sshrunner"
	"pantau/internal/store"
)

func TestParseFree_ModernWithSwap(t *testing.T) {
	out := `               total        used        free      shared  buff/cache   available
Mem:     16573849600  4823449600  8923449600   523449600  2826950400 11226950400
Swap:     2147479552   536870912  1610608640`

	ramUsed, ramTot, swpUsed, swpTot := parseFree(out)
	if ramTot != 16573849600 {
		t.Fatalf("expected ramTot 16573849600, got %d", ramTot)
	}
	if ramUsed != 4823449600 {
		t.Fatalf("expected ramUsed 4823449600, got %d", ramUsed)
	}
	if swpTot != 2147479552 {
		t.Fatalf("expected swpTot 2147479552, got %d", swpTot)
	}
	if swpUsed != 536870912 {
		t.Fatalf("expected swpUsed 536870912, got %d", swpUsed)
	}
}

func TestParseFree_WithoutSwap(t *testing.T) {
	out := `Mem: 16777216000 8388608000 8388608000`
	ramUsed, ramTot, swpUsed, swpTot := parseFree(out)
	if ramTot != 16777216000 || ramUsed != 8388608000 {
		t.Fatalf("unexpected ram values: used=%d, tot=%d", ramUsed, ramTot)
	}
	if swpTot != 0 || swpUsed != 0 {
		t.Fatalf("expected 0 swap, got used=%d, tot=%d", swpUsed, swpTot)
	}
}

func TestParseFree_LegacyBuffersCache(t *testing.T) {
	out := `             total       used       free     shared    buffers     cached
Mem:       1019624     648160     371464          0      68280     265432
-/+ buffers/cache:     314448     705176
Swap:      2097144          0    2097144`

	ramUsed, ramTot, swpUsed, swpTot := parseFree(out)
	if ramTot != 1019624 {
		t.Fatalf("expected ramTot 1019624, got %d", ramTot)
	}
	if ramUsed != 314448 {
		t.Fatalf("expected ramUsed 314448, got %d", ramUsed)
	}
	if swpTot != 2097144 || swpUsed != 0 {
		t.Fatalf("expected swap: used=0, tot=2097144, got used=%d, tot=%d", swpUsed, swpTot)
	}
}

func TestEvaluateRule_DiskHungNFSTimeout(t *testing.T) {
	mockRunner := sshrunner.NewMockRunner()
	mockRunner.DefaultExec = func(cmd string) (string, string, int, error) {
		if strings.Contains(cmd, "df ") {
			return "", "", 124, nil // Exit code 124 represents timeout (e.g. hung NFS)
		}
		return "", "", 0, nil
	}

	ins := &Inspector{}
	host := &store.Host{ID: 1, Host: "10.0.0.1"}
	rule := &store.DesiredRule{
		Kind:     "disk",
		Target:   "/mnt/nfs_backup",
		Expected: "<80%",
	}

	current, status, _, rootCause := ins.evaluateRule(host, mockRunner, rule)
	if status != "drift" {
		t.Fatalf("expected status 'drift', got %q", status)
	}
	if current != "unresponsive" {
		t.Fatalf("expected current 'unresponsive', got %q", current)
	}
	if !strings.Contains(rootCause, "timed out (possible hung NFS") {
		t.Fatalf("expected hung NFS root cause, got %q", rootCause)
	}
}

func TestInspectHost_InFlightGuard(t *testing.T) {
	ins := &Inspector{}
	ins.inFlight.Store(int64(42), struct{}{})

	err := ins.InspectHost(42)
	if !errors.Is(err, ErrAlreadyInspecting) {
		t.Fatalf("expected ErrAlreadyInspecting, got %v", err)
	}
}

func TestInspectHost_TimeoutAbortsRunner(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "pantau-ins-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	db, err := store.Open(filepath.Join(tmpDir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	hostID, err := db.CreateHost(&store.Host{
		Name: "Test Server",
		Host: "1.2.3.4",
		Port: 22,
		User: "root",
	})
	if err != nil {
		t.Fatal(err)
	}

	mockRunner := sshrunner.NewMockRunner()
	// Simulate a hanging command that blocks until runner is closed
	mockRunner.DefaultExec = func(cmd string) (string, string, int, error) {
		for i := 0; i < 20; i++ {
			time.Sleep(10 * time.Millisecond)
			if mockRunner.Closed {
				return "", "", -1, errors.New("use of closed network connection")
			}
		}
		return "", "", 0, nil
	}

	factory := func(host string, port int, user, key string) (sshrunner.Runner, error) {
		return mockRunner, nil
	}

	ins := New(db, factory, nil)

	// Run inspection with tiny timeout of 30ms
	err = ins.InspectHostWithTimeout(hostID, 30*time.Millisecond)
	if err == nil {
		t.Fatal("expected inspection to fail with timeout error, got nil")
	}

	// Verify runner was closed
	if !mockRunner.Closed {
		t.Fatal("expected mockRunner.Closed to be true after timeout")
	}

	// Verify host status updated to down or degraded with timeout noted
	h, err := db.GetHost(hostID)
	if err != nil {
		t.Fatal(err)
	}
	if h.Status != "down" && h.Status != "degraded" {
		t.Fatalf("expected host status 'down' or 'degraded', got %q", h.Status)
	}
}

func TestInspectHost_AutoResolveAlertsOnHealthy(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "pantau-ins-autoresolve-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	db, err := store.Open(filepath.Join(tmpDir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	hostID, err := db.CreateHost(&store.Host{
		Name: "Server Healthy",
		Host: "1.2.3.4",
		Port: 22,
		User: "root",
	})
	if err != nil {
		t.Fatal(err)
	}

	// Create an active alert for this host
	_, err = db.CreateAlert(&store.Alert{
		HostID:  hostID,
		Level:   "critical",
		Message: "Previous drift alert",
	})
	if err != nil {
		t.Fatal(err)
	}

	alerts, _ := db.ListActiveAlerts()
	if len(alerts) != 1 {
		t.Fatalf("expected 1 active alert before inspection, got %d", len(alerts))
	}

	mockRunner := sshrunner.NewMockRunner()
	mockRunner.DefaultExec = func(cmd string) (string, string, int, error) {
		// Mock SystemMetricsBatchCmd
		if strings.Contains(cmd, "uname -r") {
			parts := []string{
				"5.15.0-generic", "---",
				"NAME=\"Ubuntu\"\nVERSION=\"22.04 LTS\"", "---",
				"up 10 days, 1 user, load average: 0.10, 0.20, 0.15", "---",
				"Mem: 8000000000 4000000000 4000000000", "---",
				"/dev/sda1 100000 50000 50000 50% /", "---",
				"0", "---",
				"1000 1000", "---",
				"time=12.5", "---",
				"1.1.1.1", "---",
				"127.0.0.1:80 127.0.0.1:12345", "---",
				"LISTEN 127.0.0.1:80", "---",
				"0", "---",
				"4", "---",
				"01/01/2022", "---",
				"KVM", "---",
				"1640995200",
			}
			return strings.Join(parts, "\n"), "", 0, nil
		}
		return "", "", 0, nil
	}

	factory := func(host string, port int, user, key string) (sshrunner.Runner, error) {
		return mockRunner, nil
	}

	ins := New(db, factory, nil)
	err = ins.InspectHost(hostID)
	if err != nil {
		t.Fatalf("unexpected inspection error: %v", err)
	}

	// Verify host is healthy and active alerts were auto-resolved
	h, _ := db.GetHost(hostID)
	if h.Status != "healthy" {
		t.Fatalf("expected host status 'healthy', got %q", h.Status)
	}

	activeAlerts, _ := db.ListActiveAlerts()
	if len(activeAlerts) != 0 {
		t.Fatalf("expected active alerts to be auto-resolved, but found %d", len(activeAlerts))
	}
}

func TestLifecycleScore_CommissionDateOverride(t *testing.T) {
	// Scenario: Physical server with old BIOS (May 2017 -> ~9 yrs old)
	// Without override, Productive Lifespan penalty is -25 (score <= 75).
	scoreNoOverride, notesNoOverride, breakdownNoOverride := calculateLifecycleScore(
		"Ubuntu 22.04 LTS", "0.10, 0.20", 4,
		2000000000, 8000000000, 20000000000, 100000000000,
		0, "05/10/2017", "Dell Inc.", 0, "",
	)
	if scoreNoOverride > 75 {
		t.Fatalf("expected score deduction for 2017 BIOS without override, got %d", scoreNoOverride)
	}
	if !strings.Contains(breakdownNoOverride, "Exceeds 8-yr critical lifespan") {
		t.Fatalf("expected critical lifespan warning without override, got %s", breakdownNoOverride)
	}

	// With commission date override (e.g. commissioned 6 months ago):
	recentDate := time.Now().AddDate(0, -6, 0).Format("2006-01-02")
	scoreWithOverride, _, breakdownWithOverride := calculateLifecycleScore(
		"Ubuntu 22.04 LTS", "0.10, 0.20", 4,
		2000000000, 8000000000, 20000000000, 100000000000,
		0, "05/10/2017", "Dell Inc.", 0, recentDate,
	)
	if scoreWithOverride != 100 {
		t.Fatalf("expected 100 score with recent commission date override, got %d", scoreWithOverride)
	}
	if !strings.Contains(breakdownWithOverride, "Commissioned: "+recentDate) {
		t.Fatalf("expected breakdown to mention Commissioned date, got: %s", breakdownWithOverride)
	}
	if !strings.Contains(breakdownWithOverride, "Motherboard BIOS: May 2017") {
		t.Fatalf("expected breakdown to retain Motherboard BIOS audit note, got: %s", breakdownWithOverride)
	}

	// Commission date in future must be ignored/fallback to BIOS
	futureDate := time.Now().AddDate(1, 0, 0).Format("2006-01-02")
	scoreFuture, _, _ := calculateLifecycleScore(
		"Ubuntu 22.04 LTS", "0.10, 0.20", 4,
		2000000000, 8000000000, 20000000000, 100000000000,
		0, "05/10/2017", "Dell Inc.", 0, futureDate,
	)
	if scoreFuture == 100 {
		t.Fatalf("future commission date must not override old BIOS score, got %d", scoreFuture)
	}

	// Commission date before BIOS date must be ignored/fallback to BIOS
	scoreBeforeBIOS, _, _ := calculateLifecycleScore(
		"Ubuntu 22.04 LTS", "0.10, 0.20", 4,
		2000000000, 8000000000, 20000000000, 100000000000,
		0, "05/10/2017", "Dell Inc.", 0, "2015-01-01",
	)
	if scoreBeforeBIOS == 100 {
		t.Fatalf("commission date before BIOS date must not override old BIOS score, got %d", scoreBeforeBIOS)
	}
	_ = notesNoOverride
}

func TestRecalculateHostLifecycle(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "pantau-recalc-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	db, err := store.Open(filepath.Join(tmpDir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	hostID, err := db.CreateHost(&store.Host{
		Name:          "NOS Physical Server",
		Host:          "192.168.1.50",
		Port:          22,
		User:          "root",
		HardwareModel: "Supermicro",
		BIOSDate:      "01/15/2016", // 10 years old
	})
	if err != nil {
		t.Fatal(err)
	}

	h, _ := db.GetHost(hostID)
	h.OSInfo = "Ubuntu 22.04 LTS"
	h.CPUCores = 8
	h.CPULoad = "0.5, 0.5"
	h.RAMUsedBytes = 4000000000
	h.RAMTotalBytes = 16000000000
	h.DiskUsedBytes = 20000000000
	h.DiskTotalBytes = 200000000000
	past := time.Now().Add(-1 * time.Hour)
	h.LastInspected = &past
	h.LifecycleScore = 75
	h.LifecycleNotes = "Exceeds 8-yr critical lifespan"
	_ = db.UpdateHostInspection(h)

	ins := New(db, nil, nil)

	// Set commission date to 3 months ago and recalculate
	h.CommissionDate = time.Now().AddDate(0, -3, 0).Format("2006-01-02")
	_ = db.UpdateHost(h)

	err = ins.RecalculateHostLifecycle(hostID)
	if err != nil {
		t.Fatalf("recalculate error: %v", err)
	}

	updated, err := db.GetHost(hostID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.LifecycleScore != 100 {
		t.Fatalf("expected updated lifecycle score to be 100 after commission date, got %d", updated.LifecycleScore)
	}
	if !strings.Contains(updated.LifecycleBreakdown, "Commissioned: "+h.CommissionDate) {
		t.Fatalf("expected breakdown to show commission date, got: %s", updated.LifecycleBreakdown)
	}
}

func TestEvaluateRule_Port(t *testing.T) {
	ins := &Inspector{}
	host := &store.Host{ID: 1, Host: "10.0.0.1"}

	// 1. Port listening
	mockOpen := sshrunner.NewMockRunner()
	mockOpen.DefaultExec = func(cmd string) (string, string, int, error) {
		return "LISTEN 0 128 0.0.0.0:80 0.0.0.0:*", "", 0, nil
	}
	rule := &store.DesiredRule{Kind: "port", Target: "80", Expected: "listening"}
	curr, status, _, _ := ins.evaluateRule(host, mockOpen, rule)
	if status != "ok" || curr != "listening" {
		t.Fatalf("expected ok/listening, got %s/%s", status, curr)
	}

	// 2. Port closed (drift)
	mockClosed := sshrunner.NewMockRunner()
	mockClosed.DefaultExec = func(cmd string) (string, string, int, error) {
		return "", "", 1, nil
	}
	curr, status, _, rootCause := ins.evaluateRule(host, mockClosed, rule)
	if status != "drift" || curr != "closed" {
		t.Fatalf("expected drift/closed, got %s/%s", status, curr)
	}
	if !strings.Contains(rootCause, "not listening") {
		t.Fatalf("unexpected root cause: %s", rootCause)
	}
}

func TestEvaluateRule_Process(t *testing.T) {
	ins := &Inspector{}
	host := &store.Host{ID: 1, Host: "10.0.0.1"}

	// 1. Process running
	mockRunning := sshrunner.NewMockRunner()
	mockRunning.DefaultExec = func(cmd string) (string, string, int, error) {
		return "1234 supervisord", "", 0, nil
	}
	rule := &store.DesiredRule{Kind: "process", Target: "supervisord", Expected: "running"}
	curr, status, _, _ := ins.evaluateRule(host, mockRunning, rule)
	if status != "ok" || curr != "running" {
		t.Fatalf("expected ok/running, got %s/%s", status, curr)
	}

	// 2. Process stopped (drift)
	mockStopped := sshrunner.NewMockRunner()
	mockStopped.DefaultExec = func(cmd string) (string, string, int, error) {
		return "", "", 1, nil
	}
	curr, status, _, rootCause := ins.evaluateRule(host, mockStopped, rule)
	if status != "drift" || curr != "stopped" {
		t.Fatalf("expected drift/stopped, got %s/%s", status, curr)
	}
	if !strings.Contains(rootCause, "not running") {
		t.Fatalf("unexpected root cause: %s", rootCause)
	}
}

func TestEvaluateRule_Cron(t *testing.T) {
	ins := &Inspector{}
	host := &store.Host{ID: 1, Host: "10.0.0.1"}

	rule := &store.DesiredRule{Kind: "cron", Target: "backup.sh", Expected: "configured"}

	// 1. Cron found
	mockFound := sshrunner.NewMockRunner()
	mockFound.DefaultExec = func(cmd string) (string, string, int, error) {
		return "0 2 * * * /opt/scripts/backup.sh >/dev/null 2>&1", "", 0, nil
	}
	curr, status, _, _ := ins.evaluateRule(host, mockFound, rule)
	if status != "ok" || curr != "configured" {
		t.Fatalf("expected ok/configured, got %s/%s", status, curr)
	}

	// 2. Cron missing
	mockMissing := sshrunner.NewMockRunner()
	mockMissing.DefaultExec = func(cmd string) (string, string, int, error) {
		return "# empty crontab", "", 0, nil
	}
	curr, status, _, _ = ins.evaluateRule(host, mockMissing, rule)
	if status != "drift" || curr != "missing" {
		t.Fatalf("expected drift/missing, got %s/%s", status, curr)
	}
}

func TestParseDiskIOMetrics(t *testing.T) {
	// 1. Initial run: sets baseline bytes, speed 0
	h := &store.Host{}
	parseDiskIOMetrics(h, "10485760 20971520")
	if h.DiskReadBytes != 10485760 || h.DiskWriteBytes != 20971520 {
		t.Fatalf("expected initial bytes 10485760/20971520, got %d/%d", h.DiskReadBytes, h.DiskWriteBytes)
	}
	if h.DiskReadSpeedBps != 0 || h.DiskWriteSpeedBps != 0 {
		t.Fatalf("expected initial speed 0, got %d/%d", h.DiskReadSpeedBps, h.DiskWriteSpeedBps)
	}

	// 2. Subsequent run after 5 seconds
	last := time.Now().Add(-5 * time.Second)
	h.LastInspected = &last
	// Delta read: 5,242,880 bytes in 5s = ~1,048,576 Bps
	// Delta write: 10,485,760 bytes in 5s = ~2,097,152 Bps
	parseDiskIOMetrics(h, "15728640 31457280")
	if h.DiskReadBytes != 15728640 || h.DiskWriteBytes != 31457280 {
		t.Fatalf("expected updated bytes, got %d/%d", h.DiskReadBytes, h.DiskWriteBytes)
	}
	if h.DiskReadSpeedBps < 1000000 || h.DiskReadSpeedBps > 1100000 {
		t.Fatalf("expected read speed around 1MB/s, got %d", h.DiskReadSpeedBps)
	}
	if h.DiskWriteSpeedBps < 2000000 || h.DiskWriteSpeedBps > 2200000 {
		t.Fatalf("expected write speed around 2MB/s, got %d", h.DiskWriteSpeedBps)
	}

	// 3. Host reboot / counter reset: counter drops
	last2 := time.Now().Add(-5 * time.Second)
	h.LastInspected = &last2
	parseDiskIOMetrics(h, "1048576 2097152")
	if h.DiskReadBytes != 1048576 || h.DiskWriteBytes != 2097152 {
		t.Fatalf("expected reboot baseline bytes, got %d/%d", h.DiskReadBytes, h.DiskWriteBytes)
	}
	if h.DiskReadSpeedBps != 0 || h.DiskWriteSpeedBps != 0 {
		t.Fatalf("expected speed reset to 0 upon reboot, got %d/%d", h.DiskReadSpeedBps, h.DiskWriteSpeedBps)
	}
}



