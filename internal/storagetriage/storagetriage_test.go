package storagetriage

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"pantau/internal/store"
)

func TestFormatBytes(t *testing.T) {
	tests := []struct {
		bytes int64
		want  string
	}{
		{0, "0 B"},
		{500, "500 B"},
		{1024, "1.0 KB"},
		{1536, "1.5 KB"},
		{1048576, "1.0 MB"},
		{1073741824, "1.0 GB"},
		{5368709120, "5.0 GB"},
	}

	for _, tt := range tests {
		got := FormatBytes(tt.bytes)
		if got != tt.want {
			t.Errorf("FormatBytes(%d) = %q, want %q", tt.bytes, got, tt.want)
		}
	}
}

func TestParseHumanBytes(t *testing.T) {
	tests := []struct {
		input string
		want  int64
	}{
		{"1.0G", 1073741824},
		{"500M", 524288000},
		{"250KB", 256000},
		{"Archived journals take up 1.5G", 1610612736},
		{"0", 0},
		{"invalid", 0},
	}

	for _, tt := range tests {
		got := ParseHumanBytes(tt.input)
		if got != tt.want {
			t.Errorf("ParseHumanBytes(%q) = %d, want %d", tt.input, got, tt.want)
		}
	}
}

func TestParseTriageOutput(t *testing.T) {
	sampleOutput := `===SUDO_STATUS===
sudo -n 
1000

===PARTITION===
/dev/root   104857600 83886080 20971520      80% /

===SPACE_HOGS===
52428800	/var
20971520	/home
10485760	/usr
83886080	/

===TOP_FILES===
1073741824 /var/log/app.log
524288000 /var/tmp/backup.tar

===RUNAWAY_FILES===
1073741824 /var/log/app.log

===GHOST_FILES===
nginx 4512 3221225472 /var/log/nginx/access.log (deleted)

===RECLAIM_JOURNAL===
Archived and active journals take up 1.2G in the file system.

===RECLAIM_PKGCACHE===
apt
104857600

===RECLAIM_ROTATED_LOGS===
52428800

===RECLAIM_COREDUMP===
10485760

===RECLAIM_DOCKER===
active
500MB
1.5GB
`

	res := ParseTriageOutput(sampleOutput)

	if !res.HasSudo {
		t.Errorf("expected HasSudo to be true")
	}
	if res.IsRoot {
		t.Errorf("expected IsRoot to be false for UID 1000")
	}

	// Partition
	if res.Partition.MountPoint != "/" {
		t.Errorf("mount point = %s, want /", res.Partition.MountPoint)
	}
	if res.Partition.UsedPercent != 80.0 {
		t.Errorf("used percent = %f, want 80.0", res.Partition.UsedPercent)
	}

	// Space Hogs
	if len(res.SpaceHogs) != 3 {
		t.Fatalf("expected 3 space hogs (excluding root), got %d", len(res.SpaceHogs))
	}
	if res.SpaceHogs[0].Path != "/var" || res.SpaceHogs[0].SizeBytes != 52428800*1024 {
		t.Errorf("first space hog unexpected: %+v", res.SpaceHogs[0])
	}

	// Top Files
	if len(res.TopFiles) != 2 {
		t.Fatalf("expected 2 top files, got %d", len(res.TopFiles))
	}
	if res.TopFiles[0].Path != "/var/log/app.log" || res.TopFiles[0].SizeBytes != 1073741824 {
		t.Errorf("first top file unexpected: %+v", res.TopFiles[0])
	}

	// Runaway Files
	if len(res.RunawayFiles) != 1 {
		t.Fatalf("expected 1 runaway file, got %d", len(res.RunawayFiles))
	}

	// Ghost Files
	if len(res.GhostFiles) != 1 {
		t.Fatalf("expected 1 ghost file, got %d", len(res.GhostFiles))
	}
	if res.GhostFiles[0].Command != "nginx" || res.GhostFiles[0].PID != "4512" || res.GhostFiles[0].SizeBytes != 3221225472 {
		t.Errorf("ghost file unexpected: %+v", res.GhostFiles[0])
	}

	// Reclaimables
	if len(res.Reclaimables) < 4 {
		t.Fatalf("expected at least 4 reclaimable items, got %d", len(res.Reclaimables))
	}

	var hasJournal, hasPkg, hasRot, hasDocker bool
	for _, rec := range res.Reclaimables {
		switch rec.Key {
		case "journal":
			hasJournal = true
		case "pkgcache":
			hasPkg = true
		case "rotated_logs":
			hasRot = true
		case "docker":
			hasDocker = true
		}
	}

	if !hasJournal || !hasPkg || !hasRot || !hasDocker {
		t.Errorf("missing expected reclaimable: journal=%v pkg=%v rot=%v docker=%v",
			hasJournal, hasPkg, hasRot, hasDocker)
	}

	if res.TotalReclaimBytes <= 0 {
		t.Errorf("expected TotalReclaimBytes > 0, got %d", res.TotalReclaimBytes)
	}
}

func TestSecurityBoundaryTruncate(t *testing.T) {
	svc := &Service{}

	badPaths := []string{
		"/",
		"/etc/passwd",
		"/etc/shadow",
		"/bin/bash",
		"/usr/bin/python",
		"/boot/vmlinuz",
		"/sys/firmware",
		"/proc/cpuinfo",
		"/var/lib/mysql/surat/pesan.ibd",
		"/var/lib/mysql/ibdata1",
		"/var/lib/postgresql/15/main/base/16384/2673",
		"/var/lib/redis/dump.rdb",
		"/opt/app/database.sqlite3",
		"/var/lib/docker/volumes/my_vol/_data/state.db",
		"/var/log/../../etc/passwd",
		"/var/tmp/../../etc/shadow",
		"relative/path.log",
		"",
	}

	for _, p := range badPaths {
		_, err := svc.ExecuteReclaim(context.Background(), 1, ReclaimRequest{
			ActionKey:  "truncate",
			TargetPath: p,
		})
		if err == nil {
			t.Errorf("expected error for path %q, got nil", p)
		}
	}
}

func TestUnitValidationReload(t *testing.T) {
	svc := &Service{}

	badUnits := []string{
		"nginx; rm -rf /",
		"nginx && reboot",
		"bad unit name with spaces",
		"",
	}

	for _, u := range badUnits {
		_, err := svc.ExecuteReclaim(context.Background(), 1, ReclaimRequest{
			ActionKey:  "reload_service",
			TargetPath: u,
		})
		if err == nil {
			t.Errorf("expected error for unit %q, got nil", u)
		}
	}
}

func TestShellQuote(t *testing.T) {
	input := "/var/log/my test's file.log"
	got := ShellQuote(input)
	if !strings.HasPrefix(got, "'") || !strings.HasSuffix(got, "'") {
		t.Errorf("ShellQuote(%q) = %q, expected quoted string", input, got)
	}
}

func TestDatabaseRunawayExclusion(t *testing.T) {
	// 1. Verify IsDatabaseOrProtectedFile
	dbFiles := []string{
		"/var/lib/mysql/surat/pesan.ibd",
		"/var/lib/mysql/ibdata1",
		"/var/lib/mariadb/db.ibd",
		"/var/lib/postgresql/data/base/1/123",
		"/var/lib/redis/dump.rdb",
		"/var/lib/docker/volumes/app_data/_data/app.db",
		"/home/user/app.sqlite3",
	}
	for _, f := range dbFiles {
		if !IsDatabaseOrProtectedFile(f) {
			t.Errorf("expected IsDatabaseOrProtectedFile(%q) to be true", f)
		}
		if IsLogOrDisposableFile(f) {
			t.Errorf("expected IsLogOrDisposableFile(%q) to be false", f)
		}
	}

	// 2. Verify ParseTriageOutput filters out database files from RunawayFiles
	raw := `===RUNAWAY_FILES===
8589934592 /var/lib/mysql/surat/pesan.ibd
1073741824 /var/log/nginx/error.log
`
	res := ParseTriageOutput(raw)
	if len(res.RunawayFiles) != 1 {
		t.Fatalf("expected 1 runaway file, got %d", len(res.RunawayFiles))
	}
	if res.RunawayFiles[0].Path != "/var/log/nginx/error.log" {
		t.Errorf("expected runaway file to be /var/log/nginx/error.log, got %s", res.RunawayFiles[0].Path)
	}
}

func TestCancelTriage(t *testing.T) {
	svc := &Service{}
	// When not in flight
	if svc.CancelTriage(999) {
		t.Errorf("expected CancelTriage(999) to return false when nothing in flight")
	}

	// Store dummy cancel func
	called := false
	var dummyCancel context.CancelFunc = func() {
		called = true
	}
	svc.inFlight.Store(int64(42), dummyCancel)

	if !svc.CancelTriage(42) {
		t.Errorf("expected CancelTriage(42) to return true")
	}
	if !called {
		t.Errorf("expected dummyCancel to be invoked")
	}
}

func TestReclaimSudoPasswordValidation(t *testing.T) {
	cmd, err := ValidateReclaimRequest(ReclaimRequest{
		ActionKey:    "pkgcache",
		SudoPassword: "secretPassword123",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(cmd, "apt-get clean") || !strings.Contains(cmd, "yum clean all") {
		t.Errorf("expected clean command to contain apt-get and yum, got %q", cmd)
	}
}

func TestCheckThrottle(t *testing.T) {
	tmpDir := t.TempDir()
	db, err := store.Open(filepath.Join(tmpDir, "test.db"))
	if err != nil {
		t.Fatalf("failed to open test db: %v", err)
	}
	defer db.Close()

	hostID, err := db.CreateHost(&store.Host{
		Name:              "test-host",
		Host:              "127.0.0.1",
		Port:              22,
		User:              "root",
		Status:            "healthy",
		CPUCores:          4,
		CPULoad:           "0.50, 0.40, 0.30",
		DiskWriteSpeedBps: 1024 * 1024, // 1 MB/s
	})
	if err != nil {
		t.Fatalf("failed to create host: %v", err)
	}

	svc := NewService(db, nil)

	// 1. Initial check: should be allowed (normal state)
	status, err := svc.CheckThrottle(hostID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !status.Allowed {
		t.Errorf("expected initial check to be allowed")
	}
	if status.State != LoadStateNormal {
		t.Errorf("expected state %s, got %s", LoadStateNormal, status.State)
	}

	// 2. Set recent scan time: should be throttled (remaining > 0)
	svc.SetLastScanTimeForTest(hostID, time.Now())
	status, err = svc.CheckThrottle(hostID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if status.Allowed {
		t.Errorf("expected check right after scan to be not allowed")
	}
	if status.RemainingSeconds <= 0 {
		t.Errorf("expected remaining seconds > 0, got %d", status.RemainingSeconds)
	}
	if !strings.Contains(status.Reason, "Scan cooldown active") {
		t.Errorf("expected English reason, got %q", status.Reason)
	}
	if !strings.Contains(status.ReasonID, "Waktu jeda") {
		t.Errorf("expected Indonesian reason_id, got %q", status.ReasonID)
	}

	// 3. Force bypass should be allowed
	status, err = svc.CheckThrottle(hostID, true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !status.Allowed {
		t.Errorf("expected force check to be allowed")
	}

	// 4. Update host to Busy write speed (20 MB/s)
	h, _ := db.GetHost(hostID)
	h.DiskWriteSpeedBps = 20 * 1024 * 1024
	_ = db.UpdateHostInspection(h)

	status, _ = svc.CheckThrottle(hostID)
	if status.State != LoadStateBusy {
		t.Errorf("expected state %s for 20MB/s write, got %s", LoadStateBusy, status.State)
	}
	if status.CooldownSeconds != CooldownBusySeconds {
		t.Errorf("expected cooldown %d, got %d", CooldownBusySeconds, status.CooldownSeconds)
	}

	// 5. Update host to Very Busy write speed (45 MB/s)
	svc.ResetThrottle(hostID)
	h.DiskWriteSpeedBps = 45 * 1024 * 1024
	_ = db.UpdateHostInspection(h)

	status, _ = svc.CheckThrottle(hostID)
	if status.State != LoadStateVeryBusy {
		t.Errorf("expected state %s for 45MB/s write, got %s", LoadStateVeryBusy, status.State)
	}
	if status.Allowed {
		t.Errorf("expected very busy host to be blocked without force")
	}

	// 6. Very Busy by CPU load (4 cores, load 9.5)
	h.DiskWriteSpeedBps = 1024
	h.CPULoad = "9.50, 8.00, 7.00"
	_ = db.UpdateHostInspection(h)

	status, _ = svc.CheckThrottle(hostID)
	if status.State != LoadStateVeryBusy {
		t.Errorf("expected state %s for CPU overload, got %s", LoadStateVeryBusy, status.State)
	}
	if status.Allowed {
		t.Errorf("expected CPU overloaded host to be blocked without force")
	}
}

func TestSpaceHogPathWithSpaces(t *testing.T) {
	raw := `===SPACE_HOGS===
10485760	/var/data/my backup
`
	res := ParseTriageOutput(raw)
	if len(res.SpaceHogs) != 1 {
		t.Fatalf("expected 1 space hog, got %d", len(res.SpaceHogs))
	}
	if res.SpaceHogs[0].Path != "/var/data/my backup" {
		t.Errorf("expected path '/var/data/my backup', got %q", res.SpaceHogs[0].Path)
	}
}

func TestGhostFileExtractionCleanPath(t *testing.T) {
	raw := `===GHOST_FILES===
nginx 4512 3221225472 /var/log/nginx/access.log (deleted)
`
	res := ParseTriageOutput(raw)
	if len(res.GhostFiles) != 1 {
		t.Fatalf("expected 1 ghost file, got %d", len(res.GhostFiles))
	}
	if res.GhostFiles[0].Path != "/var/log/nginx/access.log" {
		t.Errorf("expected clean path '/var/log/nginx/access.log', got %q", res.GhostFiles[0].Path)
	}
}

func TestDockerPruneCommand(t *testing.T) {
	cmd, err := ValidateReclaimRequest(ReclaimRequest{
		ActionKey: "docker",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cmd != "docker system prune -af" {
		t.Errorf("expected 'docker system prune -af' to prune all unused images and containers, got %q", cmd)
	}
}

func TestLastResultCaching(t *testing.T) {
	svc := NewService(nil, nil)
	if res := svc.GetLastResult(99); res != nil {
		t.Errorf("expected nil for non-existent host, got %v", res)
	}

	mockRes := &TriageResult{
		HostID:    99,
		HostName:  "test-server",
		ScannedAt: "2026-11-03 11:00:00",
	}
	svc.SetLastResultForTest(99, mockRes)

	cached := svc.GetLastResult(99)
	if cached == nil {
		t.Fatalf("expected cached result, got nil")
	}
	if cached.HostName != "test-server" || cached.ScannedAt != "2026-11-03 11:00:00" {
		t.Errorf("cached result data mismatch: %+v", cached)
	}
}

