package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/pkg/sftp"

	"pantau/internal/inspector"
	"pantau/internal/notify"
	"pantau/internal/sshrunner"
	"pantau/internal/store"
	"pantau/internal/web"
)

type testEnv struct {
	db         *store.DB
	mockRunner *sshrunner.MockRunner
	inspector  *inspector.Inspector
	dispatcher *notify.Dispatcher
	server     *web.Server
	httpServer *httptest.Server
	dbPath     string
}

func setupTestEnv(t *testing.T) *testEnv {
	tmpFile, err := os.CreateTemp("", "pantau_test_*.db")
	if err != nil {
		t.Fatalf("create temp db: %v", err)
	}
	tmpPath := tmpFile.Name()
	tmpFile.Close()

	db, err := store.Open(tmpPath)
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}

	mockRunner := sshrunner.NewMockRunner()
	factory := func(host string, port int, user, key string) (sshrunner.Runner, error) {
		return mockRunner, nil
	}

	dispatcher := notify.New(db)
	ins := inspector.New(db, factory, dispatcher)
	srv := web.NewServer(db, ins, dispatcher)
	httpSrv := httptest.NewServer(srv)

	t.Cleanup(func() {
		httpSrv.Close()
		db.Close()
		os.Remove(tmpPath)
	})

	return &testEnv{
		db:         db,
		mockRunner: mockRunner,
		inspector:  ins,
		dispatcher: dispatcher,
		server:     srv,
		httpServer: httpSrv,
		dbPath:     tmpPath,
	}
}

func loginClient(t *testing.T, env *testEnv) *http.Client {
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}
	loginBody, _ := json.Marshal(map[string]string{"password": "admin"})
	resp, err := client.Post(env.httpServer.URL+"/api/login", "application/json", bytes.NewReader(loginBody))
	if err != nil {
		t.Fatalf("login failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("login status: %d", resp.StatusCode)
	}
	return client
}

// Ticket 01: Host Management & SSH Connection Verification
func TestTicket01_HostManagementAndSSH(t *testing.T) {
	env := setupTestEnv(t)
	client := loginClient(t, env)

	// Add host
	hostPayload := map[string]interface{}{
		"name": "Production VPS",
		"host": "192.168.1.50",
		"port": 22,
		"user": "root",
	}
	body, _ := json.Marshal(hostPayload)
	resp, err := client.Post(env.httpServer.URL+"/api/hosts", "application/json", bytes.NewReader(body))
	if err != nil || resp.StatusCode != http.StatusCreated {
		t.Fatalf("create host failed: %v, status: %d", err, resp.StatusCode)
	}

	var created store.Host
	_ = json.NewDecoder(resp.Body).Decode(&created)
	resp.Body.Close()

	if created.ID == 0 || created.Name != "Production VPS" {
		t.Fatalf("unexpected host: %+v", created)
	}

	// Test connection using mock runner
	env.mockRunner.Handlers["uname -srm; uptime"] = func() (string, string, int, error) {
		return "Linux 5.15.0-89-generic x86_64\n 12:00:00 up 10 days, 2:00, 1 user, load average: 0.10, 0.05, 0.01", "", 0, nil
	}

	testResp, err := client.Post(fmt.Sprintf("%s/api/hosts/%d/test", env.httpServer.URL, created.ID), "application/json", nil)
	if err != nil {
		t.Fatalf("test connection: %v", err)
	}
	defer testResp.Body.Close()

	var testResult map[string]interface{}
	_ = json.NewDecoder(testResp.Body).Decode(&testResult)
	if testResult["ok"] != true {
		t.Fatalf("expected test connection ok: true, got %+v", testResult)
	}
}

// Ticket 02: Periodic Inspection & System Metrics Dashboard
func TestTicket02_PeriodicInspectionAndLifecycle(t *testing.T) {
	env := setupTestEnv(t)

	hostID, _ := env.db.CreateHost(&store.Host{
		Name: "Ubuntu EOL Server",
		Host: "10.0.0.1",
		Port: 22,
		User: "root",
	})

	// Mock batch metrics output for Ubuntu 18.04 (EOL)
	env.mockRunner.Handlers[`uname -r; echo "---"; cat /etc/os-release 2>/dev/null || cat /usr/lib/os-release 2>/dev/null; echo "---"; uptime; echo "---"; free -b 2>/dev/null; echo "---"; df -Pk / 2>/dev/null; echo "---"; dmesg --level=err,crit 2>/dev/null | grep -iE 'I/O error|EXT4-fs error|BTRFS error' | wc -l`] = func() (string, string, int, error) {
		out := "5.4.0-42-generic\n---\nPRETTY_NAME=\"Ubuntu 18.04.6 LTS\"\nVERSION=\"18.04.6 LTS\"\n---\n 15:00:00 up 45 days, 1:12, load average: 1.20, 0.80, 0.50\n---\nMem: 16777216000 8388608000 8388608000\n---\n/dev/sda1 104857600 41943040 62914560 40% /\n---\n0"
		return out, "", 0, nil
	}

	err := env.inspector.InspectHost(hostID)
	if err != nil {
		t.Fatalf("inspect host failed: %v", err)
	}

	h, _ := env.db.GetHost(hostID)
	if h.Status != "healthy" {
		t.Fatalf("expected healthy status without rules, got %s", h.Status)
	}
	if !strings.Contains(h.OSInfo, "Ubuntu 18.04") {
		t.Fatalf("expected Ubuntu 18.04 in OSInfo, got %s", h.OSInfo)
	}
	// Lifecycle score should be penalized for EOL (100 - 30 = 70)
	if h.LifecycleScore != 70 {
		t.Fatalf("expected LifecycleScore 70 for EOL OS, got %d", h.LifecycleScore)
	}
	if !strings.Contains(h.LifecycleNotes, "End-Of-Life") {
		t.Fatalf("expected EOL note in LifecycleNotes, got %s", h.LifecycleNotes)
	}
}

// Ticket 03 & 04 & 05: Desired State Baseline, Docker Actions & Root Cause Excerpt
func TestTicket03_04_05_DockerDriftAndRootCause(t *testing.T) {
	env := setupTestEnv(t)

	hostID, _ := env.db.CreateHost(&store.Host{
		Name: "Docker Host",
		Host: "10.0.0.2",
		Port: 22,
		User: "root",
	})

	// 1. Generate Baseline
	env.mockRunner.Handlers[`docker ps --format '{{.Names}}' 2>/dev/null`] = func() (string, string, int, error) {
		return "web-app\npostgres-db", "", 0, nil
	}
	env.mockRunner.Handlers[`systemctl is-active`] = func() (string, string, int, error) {
		return "inactive", "", 0, nil
	}
	env.mockRunner.Handlers[`crontab -l 2>/dev/null`] = func() (string, string, int, error) {
		return "0 2 * * * /backup/backup.sh", "", 0, nil
	}

	rules, err := env.inspector.GenerateBaseline(hostID)
	if err != nil {
		t.Fatalf("generate baseline: %v", err)
	}
	if len(rules) < 3 {
		t.Fatalf("expected at least 3 baseline rules (disk + 2 containers + cron), got %d", len(rules))
	}

	// 2. Simulate container crash & inspection
	// Container "web-app" exited with error 137 (OOMKilled)
	env.mockRunner.Handlers[`docker inspect -f '{{.State.Status}}|{{.State.ExitCode}}|{{.State.OOMKilled}}|{{.State.Error}}' web-app 2>/dev/null`] = func() (string, string, int, error) {
		return "exited|137|true|Out of memory killer terminated process", "", 0, nil
	}
	env.mockRunner.Handlers[`docker inspect -f '{{.State.Status}}|{{.State.ExitCode}}|{{.State.OOMKilled}}|{{.State.Error}}' postgres-db 2>/dev/null`] = func() (string, string, int, error) {
		return "running|0|false|", "", 0, nil
	}
	env.mockRunner.Handlers[`docker logs --tail 50 web-app 2>&1`] = func() (string, string, int, error) {
		return "FATAL ERROR: Ineffective mark-compacts near heap limit Allocation failed - JavaScript heap out of memory", "", 0, nil
	}

	env.mockRunner.Handlers[`df -Pk / 2>/dev/null`] = func() (string, string, int, error) {
		return "Filesystem 10485760 1048576 9437184 10% /", "", 0, nil
	}

	// Run inspection
	err = env.inspector.InspectHost(hostID)
	if err != nil {
		t.Fatalf("inspect host: %v", err)
	}

	// Verify Host status is degraded
	h, _ := env.db.GetHost(hostID)
	if h.Status != "degraded" {
		t.Fatalf("expected host status degraded on drift, got %s", h.Status)
	}

	// Verify Incident record with Root Cause Excerpt
	incidents, err := env.db.ListIncidents(hostID, 10)
	if err != nil || len(incidents) == 0 {
		t.Fatalf("expected incident recorded, got %v", incidents)
	}

	var webAppInc *store.Incident
	for _, it := range incidents {
		if it.Target == "web-app" {
			webAppInc = &it
			break
		}
	}
	if webAppInc == nil {
		t.Fatalf("expected incident for web-app, got: %+v", incidents)
	}
	if webAppInc.EventType != "drift_detected" {
		t.Fatalf("expected event drift_detected, got %s", webAppInc.EventType)
	}
	if !strings.Contains(webAppInc.RootCauseExcerpt, "OOMKilled: true") || !strings.Contains(webAppInc.RootCauseExcerpt, "heap out of memory") {
		t.Fatalf("root cause excerpt missing diagnostic details: %s", webAppInc.RootCauseExcerpt)
	}

	// Verify Docker action control
	env.mockRunner.Handlers["docker restart web-app"] = func() (string, string, int, error) {
		return "web-app", "", 0, nil
	}
	out, err := env.inspector.DockerAction(hostID, "web-app", "restart")
	if err != nil || strings.TrimSpace(out) != "web-app" {
		t.Fatalf("docker restart failed: %v, out: %s", err, out)
	}
}

// Ticket 06: Backup Freshness & DB / Cron Monitoring
func TestTicket06_BackupFreshness(t *testing.T) {
	env := setupTestEnv(t)

	hostID, _ := env.db.CreateHost(&store.Host{
		Name: "Backup Host",
		Host: "10.0.0.3",
		Port: 22,
		User: "root",
	})

	backupPath := "/var/backups/db-latest.sql.gz"
	_ = env.db.SaveDesiredRule(&store.DesiredRule{
		HostID:   hostID,
		Kind:     "backup",
		Target:   backupPath,
		Expected: "fresh_24h",
		Enabled:  true,
	})

	// Case 1: Fresh backup file (size 1048576 bytes, modified 2 hours ago)
	twoHoursAgo := time.Now().Add(-2 * time.Hour).Unix()
	env.mockRunner.Handlers[fmt.Sprintf(`stat -c "%%s %%Y" %s 2>/dev/null || ls -l %s 2>/dev/null`, backupPath, backupPath)] = func() (string, string, int, error) {
		return fmt.Sprintf("1048576 %d", twoHoursAgo), "", 0, nil
	}

	err := env.inspector.InspectHost(hostID)
	if err != nil {
		t.Fatalf("inspection failed: %v", err)
	}

	items, _ := env.db.ListActualItems(hostID)
	var backupItem *store.ActualItem
	for _, it := range items {
		if it.Kind == "backup" {
			backupItem = &it
			break
		}
	}
	if backupItem == nil || backupItem.Status != "ok" {
		t.Fatalf("expected backup status ok, got %+v", backupItem)
	}

	// Case 2: Stale backup file (modified 48 hours ago)
	twoDaysAgo := time.Now().Add(-48 * time.Hour).Unix()
	env.mockRunner.Handlers[fmt.Sprintf(`stat -c "%%s %%Y" %s 2>/dev/null || ls -l %s 2>/dev/null`, backupPath, backupPath)] = func() (string, string, int, error) {
		return fmt.Sprintf("1048576 %d", twoDaysAgo), "", 0, nil
	}

	_ = env.inspector.InspectHost(hostID)
	items, _ = env.db.ListActualItems(hostID)
	for _, it := range items {
		if it.Kind == "backup" {
			backupItem = &it
			break
		}
	}
	if backupItem == nil || backupItem.Status != "drift" {
		t.Fatalf("expected stale backup status drift, got %+v", backupItem)
	}
}

// Ticket 07: Hybrid Alert Dispatcher
func TestTicket07_AlertDispatcher(t *testing.T) {
	env := setupTestEnv(t)

	// Configure fake webhook
	var receivedWebhookPayload map[string]interface{}
	webhookServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&receivedWebhookPayload)
		w.WriteHeader(http.StatusOK)
	}))
	defer webhookServer.Close()

	_ = env.db.SetSetting("webhook_url", webhookServer.URL)

	host := &store.Host{ID: 99, Name: "DB Server", Host: "10.0.0.99"}
	rule := &store.DesiredRule{Kind: "service", Target: "mysql", Expected: "active"}
	env.dispatcher.SendAlert(host, rule, "Drift: service mysql is inactive", "Active: failed (Result: exit-code)")

	// Allow brief async dispatch
	time.Sleep(100 * time.Millisecond)

	if receivedWebhookPayload == nil || receivedWebhookPayload["event"] != "drift_alert" {
		t.Fatalf("webhook payload not received or invalid: %+v", receivedWebhookPayload)
	}
	if receivedWebhookPayload["host_name"] != "DB Server" {
		t.Fatalf("expected host_name DB Server, got %v", receivedWebhookPayload["host_name"])
	}
}

// Ticket 08: Interactive Web Terminal
func TestTicket08_InteractiveWebTerminal(t *testing.T) {
	env := setupTestEnv(t)

	hostID, _ := env.db.CreateHost(&store.Host{
		Name: "Terminal Host",
		Host: "10.0.0.4",
		Port: 22,
		User: "root",
	})

	wsURL := "ws" + strings.TrimPrefix(env.httpServer.URL, "http") + fmt.Sprintf("/ws/terminal?host_id=%d", hostID)
	ws, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial websocket terminal failed: %v", err)
	}
	defer ws.Close()

	// Read initial banner
	_, msg, err := ws.ReadMessage()
	if err != nil {
		t.Fatalf("read ws: %v", err)
	}
	if !strings.Contains(string(msg), "Mock SSH Terminal Connected") {
		t.Fatalf("unexpected terminal message: %s", string(msg))
	}

	// Send command
	err = ws.WriteMessage(websocket.TextMessage, []byte("echo hello\n"))
	if err != nil {
		t.Fatalf("write ws: %v", err)
	}

	// Verify echoed response from mock runner
	_, echoMsg, err := ws.ReadMessage()
	if err != nil {
		t.Fatalf("read echo: %v", err)
	}
	if !strings.Contains(string(echoMsg), "echo hello") {
		t.Fatalf("expected echoed text, got: %s", string(echoMsg))
	}
}

// Ticket 09: Settings & Default Keypair
func TestTicket09_SettingsAndKeygen(t *testing.T) {
	env := setupTestEnv(t)

	pubKey, err := env.db.GetSetting("ssh_public_key")
	if err != nil || !strings.HasPrefix(pubKey, "ssh-ed25519") {
		t.Fatalf("expected valid ed25519 ssh public key, got %s (err: %v)", pubKey, err)
	}
}

// Ticket 09: SFTP File Manager & Code Editor
func TestTicket09_SFTPFileManagerAndEditor(t *testing.T) {
	env := setupTestEnv(t)
	client := loginClient(t, env)

	hostID, _ := env.db.CreateHost(&store.Host{
		Name: "SFTP Host",
		Host: "10.0.0.5",
		Port: 22,
		User: "root",
	})

	// Setup dynamic net.Pipe-based in-process SFTP server per call
	env.mockRunner.SFTPProvider = func() (*sftp.Client, error) {
		sConn, cConn := net.Pipe()

		server, err := sftp.NewServer(sConn)
		if err != nil {
			return nil, err
		}
		go func() {
			_ = server.Serve()
		}()

		return sftp.NewClientPipe(cConn, cConn)
	}

	tmpDir, err := os.MkdirTemp("", "pantau_sftp_test_*")
	if err != nil {
		t.Fatalf("mkdir temp: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	testFilePath := tmpDir + "/test_script.php"
	testContent := "<?php echo 'Hello Pantau'; ?>"

	// 1. Write file via API
	writePayload, _ := json.Marshal(map[string]string{
		"path":    testFilePath,
		"content": testContent,
	})
	resp, err := client.Post(fmt.Sprintf("%s/api/hosts/%d/files/write", env.httpServer.URL, hostID), "application/json", bytes.NewReader(writePayload))
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("sftp write failed: %v, status: %d", err, resp.StatusCode)
	}
	resp.Body.Close()

	// 2. Read file via API
	readResp, err := client.Get(fmt.Sprintf("%s/api/hosts/%d/files/read?path=%s", env.httpServer.URL, hostID, testFilePath))
	if err != nil || readResp.StatusCode != http.StatusOK {
		t.Fatalf("sftp read failed: %v, status: %d", err, readResp.StatusCode)
	}
	var readData map[string]string
	_ = json.NewDecoder(readResp.Body).Decode(&readData)
	readResp.Body.Close()

	if readData["content"] != testContent {
		t.Fatalf("expected content %q, got %q", testContent, readData["content"])
	}

	// 3. List directory via API
	listResp, err := client.Get(fmt.Sprintf("%s/api/hosts/%d/files/list?path=%s", env.httpServer.URL, hostID, tmpDir))
	if err != nil || listResp.StatusCode != http.StatusOK {
		t.Fatalf("sftp list failed: %v, status: %d", err, listResp.StatusCode)
	}
	var listData struct {
		Path  string `json:"path"`
		Files []struct {
			Name string `json:"name"`
		} `json:"files"`
	}
	_ = json.NewDecoder(listResp.Body).Decode(&listData)
	listResp.Body.Close()

	found := false
	for _, f := range listData.Files {
		if f.Name == "test_script.php" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected test_script.php in file list, got %+v", listData.Files)
	}
}
