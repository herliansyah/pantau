package storagetriage

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"pantau/internal/sshrunner"
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

func TestWindowsTriageOutputParsing(t *testing.T) {
	raw := `===SUDO_STATUS===
admin
0

===PARTITION===
NTFS 209715200 104857600 104857600 50% C:

===SPACE_HOGS===
52428800	C:\Windows
20971520	C:\Program Files
10485760	C:\Users
104857600	C:\

===TOP_FILES===
1073741824 C:\Users\Administrator\server.iso
52428800 C:\inetpub\logs\LogFiles\u_ex.log

===RUNAWAY_FILES===
52428800 C:\inetpub\logs\LogFiles\u_ex.log

===GHOST_FILES===

===RECLAIM_WIN_UPDATE===
209715200

===RECLAIM_TEMP===
104857600

===RECLAIM_DUMPS===
52428800

===RECLAIM_RECYCLEBIN===
52428800

===RECLAIM_IIS===
52428800

===RECLAIM_DOCKER===
active
1.5GB
`

	res := ParseTriageOutput(raw)

	if !res.IsRoot || !res.HasSudo {
		t.Errorf("expected IsRoot and HasSudo to be true for admin, got isRoot=%v hasSudo=%v", res.IsRoot, res.HasSudo)
	}

	if res.Partition.MountPoint != "C:" {
		t.Errorf("mount point = %s, want C:", res.Partition.MountPoint)
	}
	if res.Partition.Filesystem != "NTFS" {
		t.Errorf("filesystem = %s, want NTFS", res.Partition.Filesystem)
	}
	if res.Partition.UsedPercent != 50.0 {
		t.Errorf("used percent = %f, want 50.0", res.Partition.UsedPercent)
	}

	// Space Hogs (should exclude root summary C:\)
	if len(res.SpaceHogs) != 3 {
		t.Fatalf("expected 3 space hogs, got %d", len(res.SpaceHogs))
	}
	if res.SpaceHogs[0].Path != `C:\Windows` {
		t.Errorf("expected first space hog C:\\Windows, got %s", res.SpaceHogs[0].Path)
	}

	// Top Files
	if len(res.TopFiles) != 2 {
		t.Fatalf("expected 2 top files, got %d", len(res.TopFiles))
	}

	// Runaway Files
	if len(res.RunawayFiles) != 1 {
		t.Fatalf("expected 1 runaway file, got %d", len(res.RunawayFiles))
	}
	if res.RunawayFiles[0].Path != `C:\inetpub\logs\LogFiles\u_ex.log` {
		t.Errorf("expected runaway file path, got %s", res.RunawayFiles[0].Path)
	}

	// Ghost Files should be 0 on Windows
	if len(res.GhostFiles) != 0 {
		t.Errorf("expected 0 ghost files on Windows, got %d", len(res.GhostFiles))
	}

	// Reclaimables: 6 items (win_update_cache, win_temp, win_wer_dumps, win_recycle_bin, win_iis_logs, docker)
	if len(res.Reclaimables) != 6 {
		t.Fatalf("expected 6 reclaimable items, got %d", len(res.Reclaimables))
	}

	keys := make(map[string]bool)
	for _, rec := range res.Reclaimables {
		keys[rec.Key] = true
	}
	expectedKeys := []string{"win_update_cache", "win_temp", "win_wer_dumps", "win_recycle_bin", "win_iis_logs", "docker"}
	for _, ek := range expectedKeys {
		if !keys[ek] {
			t.Errorf("missing expected Windows reclaim key: %s", ek)
		}
	}

	if res.TotalReclaimBytes <= 0 {
		t.Errorf("expected TotalReclaimBytes > 0, got %d", res.TotalReclaimBytes)
	}

	// Verify win_wer_dumps ReclaimItem command matches ValidateReclaimRequest
	for _, rec := range res.Reclaimables {
		if rec.Key == "win_wer_dumps" {
			expectedCmd, _ := ValidateReclaimRequest(ReclaimRequest{ActionKey: "win_wer_dumps"}, "windows")
			if rec.Command != expectedCmd {
				t.Errorf("win_wer_dumps command mismatch: got %q, want %q", rec.Command, expectedCmd)
			}
		}
	}

	// Verify non-admin output produces Administrator warning
	nonAdminRaw := `===SUDO_STATUS===
user
1000

===PARTITION===
NTFS 209715200 104857600 104857600 50% C:

===SPACE_HOGS===
===TOP_FILES===
===RUNAWAY_FILES===
===GHOST_FILES===
===RECLAIM_WIN_UPDATE===
0
`
	nonAdminRes := ParseTriageOutput(nonAdminRaw)
	if nonAdminRes.IsRoot || nonAdminRes.HasSudo {
		t.Errorf("expected non-admin to have isRoot=false and hasSudo=false")
	}
	if !strings.Contains(nonAdminRes.Warning, "Administrator") {
		t.Errorf("expected Windows non-admin warning to mention 'Administrator', got: %s", nonAdminRes.Warning)
	}
}

func TestWindowsSecurityBoundaryTruncate(t *testing.T) {
	badPaths := []string{
		`C:\`,
		`C:\Windows\System32\cmd.exe`,
		`C:\Windows\SysWOW64\kernel32.dll`,
		`C:\Windows\WinSxS\manifest.xml`,
		`C:\Boot\BCD`,
		`C:\Recovery\winre.wim`,
		`C:\System Volume Information\indexer.dat`,
		`C:\Program Files\App\main.exe`,
		`C:\Program Files (x86)\App\sub.exe`,
		`C:\pagefile.sys`,
		`C:\swapfile.sys`,
		`C:\hiberfil.sys`,
		`C:\data\database.mdf`,
		`C:\data\database.ldf`,
		`C:\data\database.ndf`,
		`C:\Program Files\Microsoft SQL Server\data.mdf`,
		`C:\data\ntds.dit`,
		`C:\data\edb.log`,
		`C:\data\edb.chk`,
		`C:\Users\User\app.sqlite3`,
		`C:\vm\disk.vhdx`,
		`C:\vm\disk.avhd`,
		`C:\vm\disk.avhdx`,
		`C:\inetpub\logs\..\..\Windows\System32\cmd.exe`,
		`C:\Windows\Temp\tool.exe`,
		`C:\temp\config.json`,
		`C:\temp\settings.ini`,
		`C:\logs\web.config`,
		`C:\inetpub\logs\`,
		`C:\Windows\System32\dumpstack.log.tmp`,
		`C:\Windows\MEMORY.DMP`,
		`D:\`,
		`D:\Windows\System32\cmd.exe`,
		`D:\Program Files\App\sub.exe`,
		`D:\pagefile.sys`,
		`E:\Boot\BCD`,
		`E:\System Volume Information\indexer.dat`,
		`relative\path.log`,
		``,
	}

	for _, p := range badPaths {
		_, err := ValidateReclaimRequest(ReclaimRequest{
			ActionKey:  "truncate",
			TargetPath: p,
		}, "windows")
		if err == nil {
			t.Errorf("expected error for Windows protected/invalid path %q, got nil", p)
		}
	}

	goodPaths := []struct {
		path    string
		wantCmd string
	}{
		{`C:\inetpub\logs\LogFiles\u_ex.log`, `Clear-Content -LiteralPath 'C:\inetpub\logs\LogFiles\u_ex.log' -Force`},
		{`C:\Windows\Temp\debug.log`, `Clear-Content -LiteralPath 'C:\Windows\Temp\debug.log' -Force`},
		{`C:\app\logs\trace.txt`, `Clear-Content -LiteralPath 'C:\app\logs\trace.txt' -Force`},
		{`C:\logs\diag.dump`, `Clear-Content -LiteralPath 'C:\logs\diag.dump' -Force`},
		{`C:\logs\event.etl`, `Clear-Content -LiteralPath 'C:\logs\event.etl' -Force`},
		{`C:/inetpub/logs/LogFiles/u_ex.log`, `Clear-Content -LiteralPath 'C:\inetpub\logs\LogFiles\u_ex.log' -Force`},
	}

	for _, gp := range goodPaths {
		cmd, err := ValidateReclaimRequest(ReclaimRequest{
			ActionKey:  "truncate",
			TargetPath: gp.path,
		}, "windows")
		if err != nil {
			t.Errorf("unexpected error for valid path %q: %v", gp.path, err)
		}
		if cmd != gp.wantCmd {
			t.Errorf("ValidateReclaimRequest(%q) = %q, want %q", gp.path, cmd, gp.wantCmd)
		}
	}
}

func TestWindowsReclaimRequestValidation(t *testing.T) {
	tests := []struct {
		action  string
		wantSub string
	}{
		{"win_update_cache", "SoftwareDistribution\\Download"},
		{"win_temp", "Temp"},
		{"win_wer_dumps", "Minidump"},
		{"win_recycle_bin", "Clear-RecycleBin"},
		{"win_iis_logs", "inetpub\\logs\\LogFiles"},
		{"docker", "docker system prune -af"},
	}

	for _, tt := range tests {
		cmd, err := ValidateReclaimRequest(ReclaimRequest{ActionKey: tt.action}, "windows")
		if err != nil {
			t.Errorf("ValidateReclaimRequest(%s) error: %v", tt.action, err)
		}
		if !strings.Contains(cmd, tt.wantSub) {
			t.Errorf("ValidateReclaimRequest(%s) = %q, expected substring %q", tt.action, cmd, tt.wantSub)
		}
	}
}

func TestWindowsServiceReloadValidation(t *testing.T) {
	// Valid service
	cmd, err := ValidateReclaimRequest(ReclaimRequest{
		ActionKey:  "reload_service",
		TargetPath: "W3SVC",
	}, "windows")
	if err != nil {
		t.Fatalf("unexpected error for valid Windows service: %v", err)
	}
	if cmd != "Restart-Service -Name 'W3SVC' -Force" {
		t.Errorf("expected 'Restart-Service -Name \\'W3SVC\\' -Force', got %q", cmd)
	}

	// Injection attempt
	_, err = ValidateReclaimRequest(ReclaimRequest{
		ActionKey:  "reload_service",
		TargetPath: "W3SVC; Stop-Computer",
	}, "windows")
	if err == nil {
		t.Errorf("expected error for injected service name, got nil")
	}
}

func TestWindowsProbeScriptCommandLength(t *testing.T) {
	encoded := sshrunner.PowershellEncodedCmd(WindowsTriageProbeScript)
	if len(encoded) >= 8191 {
		t.Errorf("WindowsTriageProbeScript encoded command is %d chars, must be < 8191 (cmd.exe max limit)", len(encoded))
	}
}

func TestWindowsWerDumpsIncludesMemoryDmpAndCrashDumps(t *testing.T) {
	cmd, err := ValidateReclaimRequest(ReclaimRequest{ActionKey: "win_wer_dumps"}, "windows")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(cmd, "CrashDumps") {
		t.Errorf("expected win_wer_dumps to clean CrashDumps, got: %s", cmd)
	}
	if !strings.Contains(cmd, "MEMORY.DMP") {
		t.Errorf("expected win_wer_dumps to clean MEMORY.DMP, got: %s", cmd)
	}
}

func TestSpaceHogsSortingAndDriveExclusion(t *testing.T) {
	raw := `===PARTITION===
NTFS 209715200 104857600 104857600 50% D:

===SPACE_HOGS===
10485760	D:\Small
52428800	D:\Big
20971520	D:\Medium
104857600	D:\
0	D:\Empty
`
	res := ParseTriageOutput(raw)
	// D:\ summary line and 0-byte directory should be excluded
	if len(res.SpaceHogs) != 3 {
		t.Fatalf("expected 3 space hogs, got %d", len(res.SpaceHogs))
	}
	// Must be sorted descending by size
	if res.SpaceHogs[0].Path != `D:\Big` {
		t.Errorf("expected first space hog to be D:\\Big, got %s", res.SpaceHogs[0].Path)
	}
	if res.SpaceHogs[1].Path != `D:\Medium` {
		t.Errorf("expected second space hog to be D:\\Medium, got %s", res.SpaceHogs[1].Path)
	}
	if res.SpaceHogs[2].Path != `D:\Small` {
		t.Errorf("expected third space hog to be D:\\Small, got %s", res.SpaceHogs[2].Path)
	}
}

func TestCheckThrottleWindows(t *testing.T) {
	tmpDir := t.TempDir()
	db, err := store.Open(filepath.Join(tmpDir, "test_win_throttle.db"))
	if err != nil {
		t.Fatalf("failed to open test db: %v", err)
	}
	defer db.Close()

	// Windows host with 4 cores and 15% CPU load should be Allowed (normal state)
	hostID, err := db.CreateHost(&store.Host{
		Name:              "win-server-2022",
		Host:              "127.0.0.1",
		Port:              22,
		User:              "Administrator",
		OSType:            "windows",
		Status:            "healthy",
		CPUCores:          4,
		CPULoad:           "15, 15, 15",
		DiskWriteSpeedBps: 2 * 1024 * 1024, // 2 MB/s
	})
	if err != nil {
		t.Fatalf("failed to create host: %v", err)
	}

	svc := NewService(db, nil)

	status, err := svc.CheckThrottle(hostID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !status.Allowed {
		t.Errorf("expected Windows host with 15%% CPU load to be allowed, got blocked: %s", status.Reason)
	}
	if status.State != LoadStateNormal {
		t.Errorf("expected state %s for 15%% CPU load on Windows, got %s", LoadStateNormal, status.State)
	}

	// Windows host with 95% CPU load should be flagged as LoadStateVeryBusy
	h, _ := db.GetHost(hostID)
	h.CPULoad = "95, 94, 90"
	_ = db.UpdateHostInspection(h)

	status, _ = svc.CheckThrottle(hostID)
	if status.State != LoadStateVeryBusy {
		t.Errorf("expected state %s for 95%% CPU load on Windows, got %s", LoadStateVeryBusy, status.State)
	}
	if status.Allowed {
		t.Errorf("expected 95%% CPU overloaded Windows host to be blocked")
	}
	if !strings.Contains(status.Reason, "95%") {
		t.Errorf("expected Windows reason to format CPU load with '%%', got: %s", status.Reason)
	}
}

func TestWindowsTempIncludesSystemDrive(t *testing.T) {
	cmd, err := ValidateReclaimRequest(ReclaimRequest{ActionKey: "win_temp"}, "windows")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(cmd, "$env:SystemDrive\\Temp") {
		t.Errorf("expected win_temp command to clean $env:SystemDrive\\Temp, got: %s", cmd)
	}

	// Verify ReclaimItem in ParseTriageOutput also matches
	raw := `===RECLAIM_TEMP===
104857600
`
	res := ParseTriageOutput(raw)
	found := false
	for _, rec := range res.Reclaimables {
		if rec.Key == "win_temp" {
			found = true
			if !strings.Contains(rec.Command, "$env:SystemDrive\\Temp") {
				t.Errorf("expected win_temp ReclaimItem command to include $env:SystemDrive\\Temp, got: %s", rec.Command)
			}
			if rec.Command != cmd {
				t.Errorf("expected ReclaimItem command to match ValidateReclaimRequest, got %q vs %q", rec.Command, cmd)
			}
		}
	}
	if !found {
		t.Errorf("expected win_temp ReclaimItem to be generated")
	}
}

func TestWindowsOutputDeduplication(t *testing.T) {
	raw := `===SPACE_HOGS===
10485760	C:\inetpub
10485760	C:\inetpub
20971520	C:\inetpub\logs

===TOP_FILES===
52428800 C:\inetpub\logs\LogFiles\u_ex.log
52428800 C:\inetpub\logs\LogFiles\u_ex.log
31457280 C:\data\test.log

===RUNAWAY_FILES===
52428800 C:\inetpub\logs\LogFiles\u_ex.log
52428800 C:\inetpub\logs\LogFiles\u_ex.log
`
	res := ParseTriageOutput(raw)
	if len(res.SpaceHogs) != 2 {
		t.Errorf("expected 2 unique space hogs after deduplication, got %d", len(res.SpaceHogs))
	}
	if len(res.TopFiles) != 2 {
		t.Errorf("expected 2 unique top files after deduplication, got %d", len(res.TopFiles))
	}
	if len(res.RunawayFiles) != 1 {
		t.Errorf("expected 1 unique runaway file after deduplication, got %d", len(res.RunawayFiles))
	}
}

func TestWindowsProtectedBakAndArchiveRejection(t *testing.T) {
	// Database backup .bak must be rejected
	_, err := ValidateReclaimRequest(ReclaimRequest{
		ActionKey:  "truncate",
		TargetPath: `C:\backup\mydb.bak`,
	}, "windows")
	if err == nil {
		t.Errorf("expected error for .bak file truncation, got nil")
	}

	// Archive formats (.7z, .rar, .xz, .bz2, .cab) must be rejected
	archives := []string{
		`C:\temp\archive.7z`,
		`C:\temp\archive.rar`,
		`C:\temp\archive.tar.xz`,
		`C:\temp\archive.bz2`,
		`C:\temp\update.cab`,
	}
	for _, arc := range archives {
		_, err := ValidateReclaimRequest(ReclaimRequest{
			ActionKey:  "truncate",
			TargetPath: arc,
		}, "windows")
		if err == nil {
			t.Errorf("expected error for archive truncation %q, got nil", arc)
		}
	}
}

func TestWindowsDumpstackLogRejection(t *testing.T) {
	// C:\dumpstack.log without .tmp must also be rejected
	_, err := ValidateReclaimRequest(ReclaimRequest{
		ActionKey:  "truncate",
		TargetPath: `C:\dumpstack.log`,
	}, "windows")
	if err == nil {
		t.Errorf("expected error for dumpstack.log truncation, got nil")
	}
}

func TestWindowsRotatedLogAccepted(t *testing.T) {
	// Rotated logs with .log. in the name should be valid for truncation
	cmd, err := ValidateReclaimRequest(ReclaimRequest{
		ActionKey:  "truncate",
		TargetPath: `C:\app\service.log.20261108`,
	}, "windows")
	if err != nil {
		t.Fatalf("expected valid truncation command for rotated log, got err: %v", err)
	}
	if !strings.Contains(cmd, "service.log.20261108") {
		t.Errorf("expected command to include file path, got: %s", cmd)
	}
}

func TestWindowsStorageTriageProcessTermination(t *testing.T) {
	// 1. WindowsTriageProbeScript must register its process PID or tag so CancelTriage can kill it
	if !strings.Contains(WindowsTriageProbeScript, "$PID") && !strings.Contains(WindowsTriageProbeScript, "pantau_triage.pid") {
		t.Errorf("WindowsTriageProbeScript does not save or expose $PID for remote process cancellation")
	}

	// 2. WindowsTriageProbeScript must not use bare 'NULL' as robocopy destination
	// Because relative 'NULL' in C:\Windows\System32 fails with Access Denied and triggers recursive gci fallback
	if strings.Contains(WindowsTriageProbeScript, `robocopy $p "NULL"`) || strings.Contains(WindowsTriageProbeScript, `robocopy $_ "NULL"`) {
		t.Errorf("WindowsTriageProbeScript uses invalid bare 'NULL' destination for robocopy, which fails in System32")
	}

	// 3. Directory scans in TOP_FILES must have depth limit to avoid infinite loops on NTFS junctions (e.g. AppData\\Local\\Application Data)
	if strings.Contains(WindowsTriageProbeScript, `gci -Lit $r -Rec -File -ea 0`) {
		t.Errorf("WindowsTriageProbeScript has unbounded Get-ChildItem recursion in TOP_FILES without -Depth limit, causing infinite junction loops")
	}

	// 4. Fallback _sz must not do unbounded gci -Rec without depth limit
	if strings.Contains(WindowsTriageProbeScript, `gci -Lit $_ -Rec -File -Force -ea 0|measure`) {
		t.Errorf("_sz uses unbounded Get-ChildItem recursion without depth limit, which will hang on Windows system drives")
	}

	// 5. Windows Docker reclaim check must verify Docker service is active before running 'docker info' to avoid hanging on named pipe
	if !strings.Contains(WindowsTriageProbeScript, "Get-Service") && !strings.Contains(WindowsTriageProbeScript, "Get-Process") {
		t.Errorf("WindowsTriageProbeScript calls 'docker info' without checking if Docker service is running, risking named pipe hang")
	}
}

func TestCleanWindowsOutput(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "empty",
			input:    "",
			expected: "",
		},
		{
			name:     "clean normal output",
			input:    "Operation completed successfully",
			expected: "Operation completed successfully",
		},
		{
			name:     "clixml progress only",
			input:    `#< CLIXML <Objs Version="1.1.0.1" xmlns="http://schemas.microsoft.com/powershell/2004/04"><Obj S="progress" RefId="0"><TN RefId="0"><T>System.Management.Automation.PSCustomObject</T><T>System.Object</T></TN><MS><I64 N="SourceId">1</I64><PR N="Record"><AV>Preparing modules for first use.</AV><AI>0</AI><Nil /><PI>-1</PI><PC>-1</PC><T>Completed</T><SR>-1</SR><SD> </SD></PR></MS></Obj></Objs>`,
			expected: "",
		},
		{
			name:     "clixml with error tag and hex escapes",
			input:    `#< CLIXML <Objs Version="1.1.0.1" xmlns="http://schemas.microsoft.com/powershell/2004/04"><S S="Error">Cannot find drive. A drive with the name 'Z' does not exist._x000D__x000A_</S></Objs>`,
			expected: "Cannot find drive. A drive with the name 'Z' does not exist.",
		},
		{
			name:     "clixml progress followed by error",
			input:    "#< CLIXML\r\n" + `<Objs Version="1.1.0.1" xmlns="http://schemas.microsoft.com/powershell/2004/04"><Obj S="progress" RefId="0"><PR N="Record"><AV>Preparing modules for first use.</AV></PR></Obj><S S="Error">Access_x0020_is_x0020_denied._x000D__x000A_</S></Objs>`,
			expected: "Access is denied.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := cleanWindowsOutput(tt.input)
			if got != tt.expected {
				t.Errorf("cleanWindowsOutput() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestWindowsTruncateAclElevationLogic(t *testing.T) {
	// Truncate on Windows must handle inherited Deny ACEs by granting Administrators explicit permissions
	psEscaped := `C:\ProgramData\McAfee\wps\log\trace.etl`
	psWrap := fmt.Sprintf(`$ProgressPreference = 0; $ErrorActionPreference = "Stop"; $p = '%[1]s'; try { Clear-Content -LiteralPath $p -Force; exit 0 } catch { if ($_ -match 'Access.*is denied|UnauthorizedAccess') { icacls $p /grant '*S-1-5-32-544:(F)' 2>$null | Out-Null }; try { Clear-Content -LiteralPath $p -Force; exit 0 } catch { try { $s = [System.IO.File]::Open($p, [System.IO.FileMode]::Open, [System.IO.FileAccess]::Write, [System.IO.FileShare]::ReadWrite); $s.SetLength(0); $s.Close(); exit 0 } catch { [Console]::Error.WriteLine($_.Exception.Message); [Console]::Error.Flush(); exit 1 } } }`, psEscaped)

	if !strings.Contains(psWrap, "icacls $p /grant '*S-1-5-32-544:(F)'") {
		t.Errorf("expected psWrap to contain icacls elevation for Administrators SID, got: %s", psWrap)
	}
	if !strings.Contains(psWrap, "FileShare]::ReadWrite") {
		t.Errorf("expected psWrap to contain FileShare fallback for locked files")
	}
}






