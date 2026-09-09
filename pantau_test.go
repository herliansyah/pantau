package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/pkg/sftp"

	"pantau/internal/inspector"
	"pantau/internal/notify"
	"pantau/internal/snapshot"
	"pantau/internal/sshrunner"
	"pantau/internal/store"
	"pantau/internal/transfer"
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
	srv.SetTransferManager(transfer.NewManager(db, factory))
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
	env.mockRunner.Handlers[inspector.SystemMetricsBatchCmd] = func() (string, string, int, error) {
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
	if err != nil || (!strings.HasPrefix(pubKey, "ssh-rsa") && !strings.HasPrefix(pubKey, "ssh-ed25519")) {
		t.Fatalf("expected valid universal ssh public key (ssh-rsa or ssh-ed25519), got %s (err: %v)", pubKey, err)
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

	// 4. Check folder size API
	env.mockRunner.Handlers[fmt.Sprintf(`du -sb %q 2>/dev/null | cut -f1`, tmpDir)] = func() (string, string, int, error) {
		return "5242880\n", "", 0, nil
	}
	sizeResp, err := client.Get(fmt.Sprintf("%s/api/hosts/%d/files/size?path=%s", env.httpServer.URL, hostID, tmpDir))
	if err != nil || sizeResp.StatusCode != http.StatusOK {
		t.Fatalf("folder size check failed: %v, status: %d", err, sizeResp.StatusCode)
	}
	var sizeData map[string]int64
	_ = json.NewDecoder(sizeResp.Body).Decode(&sizeData)
	sizeResp.Body.Close()
	if sizeData["size"] != 5242880 {
		t.Fatalf("expected folder size 5242880, got %d", sizeData["size"])
	}

	// 5. Folder streaming download (ZIP when zip available)
	env.mockRunner.Handlers["which zip 2>/dev/null"] = func() (string, string, int, error) {
		return "/usr/bin/zip\n", "", 0, nil
	}
	zipCmd := fmt.Sprintf(`cd %q && zip -r -q - %q`, filepath.Dir(tmpDir), filepath.Base(tmpDir))
	env.mockRunner.Handlers[zipCmd] = func() (string, string, int, error) {
		return "PK_mock_zip_stream_data", "", 0, nil
	}

	dlZipResp, err := client.Get(fmt.Sprintf("%s/api/hosts/%d/files/download?path=%s", env.httpServer.URL, hostID, tmpDir))
	if err != nil || dlZipResp.StatusCode != http.StatusOK {
		t.Fatalf("folder download zip failed: %v, status: %d", err, dlZipResp.StatusCode)
	}
	if ct := dlZipResp.Header.Get("Content-Type"); ct != "application/zip" {
		t.Fatalf("expected application/zip, got %s", ct)
	}
	zipBody, _ := io.ReadAll(dlZipResp.Body)
	dlZipResp.Body.Close()
	if string(zipBody) != "PK_mock_zip_stream_data" {
		t.Fatalf("unexpected zip body: %s", string(zipBody))
	}

	// 6. Folder streaming download (tar.gz fallback when zip unavailable)
	env.mockRunner.Handlers["which zip 2>/dev/null"] = func() (string, string, int, error) {
		return "", "", 1, nil
	}
	tarCmd := fmt.Sprintf(`tar -czf - -C %q %q`, filepath.Dir(tmpDir), filepath.Base(tmpDir))
	env.mockRunner.Handlers[tarCmd] = func() (string, string, int, error) {
		return "tar_gz_mock_stream_data", "", 0, nil
	}

	dlTarResp, err := client.Get(fmt.Sprintf("%s/api/hosts/%d/files/download?path=%s", env.httpServer.URL, hostID, tmpDir))
	if err != nil || dlTarResp.StatusCode != http.StatusOK {
		t.Fatalf("folder download tar failed: %v, status: %d", err, dlTarResp.StatusCode)
	}
	if ct := dlTarResp.Header.Get("Content-Type"); ct != "application/gzip" {
		t.Fatalf("expected application/gzip, got %s", ct)
	}
	tarBody, _ := io.ReadAll(dlTarResp.Body)
	dlTarResp.Body.Close()
	if string(tarBody) != "tar_gz_mock_stream_data" {
		t.Fatalf("unexpected tar body: %s", string(tarBody))
	}
}

// Test Key Provisioning (Auto-Inject SSH Key)
func TestTicket10_KeyProvisioning(t *testing.T) {
	env := setupTestEnv(t)
	client := loginClient(t, env)

	var lastProvisionedHost string
	var lastProvisionedPass string
	var lastProvisionedKey string

	env.server.SetKeyProvisioner(func(host string, port int, user, password, pubKey string) error {
		lastProvisionedHost = host
		lastProvisionedPass = password
		lastProvisionedKey = pubKey
		return nil
	})

	// 1. Add host with one-time password to auto-inject
	payload := map[string]interface{}{
		"name":     "Target With Password",
		"host":     "10.0.0.20",
		"port":     22,
		"user":     "root",
		"password": "initial_root_password_123",
	}
	body, _ := json.Marshal(payload)
	resp, err := client.Post(env.httpServer.URL+"/api/hosts", "application/json", bytes.NewReader(body))
	if err != nil || resp.StatusCode != http.StatusCreated {
		t.Fatalf("create host with auto-inject failed: %v, status: %d", err, resp.StatusCode)
	}
	var created store.Host
	_ = json.NewDecoder(resp.Body).Decode(&created)
	resp.Body.Close()

	if lastProvisionedHost != "10.0.0.20" || lastProvisionedPass != "initial_root_password_123" {
		t.Fatalf("provisioner not called correctly: host=%s, pass=%s", lastProvisionedHost, lastProvisionedPass)
	}
	if !strings.HasPrefix(lastProvisionedKey, "ssh-rsa") && !strings.HasPrefix(lastProvisionedKey, "ssh-ed25519") {
		t.Fatalf("expected universal ssh pubkey in provisioner, got %s", lastProvisionedKey)
	}

	// 2. Separate inject-key endpoint
	env.mockRunner.Handlers["uname -srm"] = func() (string, string, int, error) {
		return "Linux 5.15.0-x86_64", "", 0, nil
	}

	injectBody, _ := json.Marshal(map[string]string{"password": "second_password_456"})
	injectResp, err := client.Post(fmt.Sprintf("%s/api/hosts/%d/inject-key", env.httpServer.URL, created.ID), "application/json", bytes.NewReader(injectBody))
	if err != nil || injectResp.StatusCode != http.StatusOK {
		t.Fatalf("inject-key failed: %v, status: %d", err, injectResp.StatusCode)
	}
	var injectResult map[string]interface{}
	_ = json.NewDecoder(injectResp.Body).Decode(&injectResult)
	injectResp.Body.Close()

	if injectResult["ok"] != true {
		t.Fatalf("expected ok: true, got %+v", injectResult)
	}
	if lastProvisionedPass != "second_password_456" {
		t.Fatalf("expected provisioner password updated, got %s", lastProvisionedPass)
	}
}

// Test Ticket 11: Cross-Host Transfer (Fast Stream & Verified)
func TestTicket11_CrossHostTransfer(t *testing.T) {
	env := setupTestEnv(t)
	client := loginClient(t, env)

	// Create Host 1 (Source) and Host 2 (Dest)
	h1Payload, _ := json.Marshal(map[string]interface{}{
		"name": "Source Server",
		"host": "192.168.1.10",
		"port": 22,
		"user": "root",
	})
	resp1, err := client.Post(env.httpServer.URL+"/api/hosts", "application/json", bytes.NewReader(h1Payload))
	if err != nil || resp1.StatusCode != http.StatusCreated {
		t.Fatalf("create host 1 failed: %v", err)
	}
	var h1 store.Host
	_ = json.NewDecoder(resp1.Body).Decode(&h1)
	resp1.Body.Close()

	h2Payload, _ := json.Marshal(map[string]interface{}{
		"name": "Dest Server",
		"host": "192.168.1.20",
		"port": 22,
		"user": "root",
	})
	resp2, err := client.Post(env.httpServer.URL+"/api/hosts", "application/json", bytes.NewReader(h2Payload))
	if err != nil || resp2.StatusCode != http.StatusCreated {
		t.Fatalf("create host 2 failed: %v", err)
	}
	var h2 store.Host
	_ = json.NewDecoder(resp2.Body).Decode(&h2)
	resp2.Body.Close()

	// 1. Fast stream directory transfer
	dummyPayload := strings.Repeat("A", 4096)
	env.mockRunner.Handlers[`test -d "/var/bigdata" && echo "DIR" || echo "FILE"`] = func() (string, string, int, error) {
		return "DIR\n", "", 0, nil
	}
	env.mockRunner.Handlers[`du -sb "/var/bigdata" 2>/dev/null | cut -f1`] = func() (string, string, int, error) {
		return "4096\n", "", 0, nil
	}
	env.mockRunner.Handlers[`tar -cf - -C "/var" "bigdata"`] = func() (string, string, int, error) {
		return dummyPayload, "", 0, nil
	}

	transferReq, _ := json.Marshal(map[string]interface{}{
		"source_host_id": h1.ID,
		"source_path":   "/var/bigdata",
		"dest_host_id":   h2.ID,
		"dest_path":     "/mnt/backup",
		"mode":          "fast",
	})
	startResp, err := client.Post(env.httpServer.URL+"/api/transfers", "application/json", bytes.NewReader(transferReq))
	if err != nil || startResp.StatusCode != http.StatusAccepted {
		t.Fatalf("start transfer failed: %v, status: %d", err, startResp.StatusCode)
	}
	var startedJob transfer.TransferJob
	_ = json.NewDecoder(startResp.Body).Decode(&startedJob)
	startResp.Body.Close()

	// Poll until completed
	var finishedJob transfer.TransferJob
	for i := 0; i < 20; i++ {
		time.Sleep(100 * time.Millisecond)
		detailResp, err := client.Get(fmt.Sprintf("%s/api/transfers/%d", env.httpServer.URL, startedJob.ID))
		if err != nil {
			continue
		}
		_ = json.NewDecoder(detailResp.Body).Decode(&finishedJob)
		detailResp.Body.Close()

		if finishedJob.Status == "completed" || finishedJob.Status == "failed" {
			break
		}
	}

	if finishedJob.Status != "completed" {
		t.Fatalf("expected job status 'completed', got %q (error: %s)", finishedJob.Status, finishedJob.Error)
	}
	if finishedJob.TransferredBytes != 4096 {
		t.Fatalf("expected 4096 transferred bytes, got %d", finishedJob.TransferredBytes)
	}

	// 2. Verified File Transfer
	fileContent := "config_secret_content_data_12345"
	env.mockRunner.Handlers[`test -d "/etc/pantau.conf" && echo "DIR" || echo "FILE"`] = func() (string, string, int, error) {
		return "FILE\n", "", 0, nil
	}
	env.mockRunner.Handlers[`du -sb "/etc/pantau.conf" 2>/dev/null | cut -f1`] = func() (string, string, int, error) {
		return fmt.Sprintf("%d\n", len(fileContent)), "", 0, nil
	}
	env.mockRunner.Handlers[`cat "/etc/pantau.conf"`] = func() (string, string, int, error) {
		return fileContent, "", 0, nil
	}
	env.mockRunner.Handlers[`sha256sum "/etc/pantau.conf" | cut -d' ' -f1`] = func() (string, string, int, error) {
		return "a1b2c3d4e5f6\n", "", 0, nil
	}
	env.mockRunner.Handlers[`sha256sum "/tmp/pantau.conf" | cut -d' ' -f1`] = func() (string, string, int, error) {
		return "a1b2c3d4e5f6\n", "", 0, nil
	}

	verifiedReq, _ := json.Marshal(map[string]interface{}{
		"source_host_id": h1.ID,
		"source_path":   "/etc/pantau.conf",
		"dest_host_id":   h2.ID,
		"dest_path":     "/tmp/pantau.conf",
		"mode":          "verified",
	})
	vResp, err := client.Post(env.httpServer.URL+"/api/transfers", "application/json", bytes.NewReader(verifiedReq))
	if err != nil || vResp.StatusCode != http.StatusAccepted {
		t.Fatalf("start verified transfer failed: %v", err)
	}
	var vJob transfer.TransferJob
	_ = json.NewDecoder(vResp.Body).Decode(&vJob)
	vResp.Body.Close()

	for i := 0; i < 20; i++ {
		time.Sleep(100 * time.Millisecond)
		detailResp, _ := client.Get(fmt.Sprintf("%s/api/transfers/%d", env.httpServer.URL, vJob.ID))
		_ = json.NewDecoder(detailResp.Body).Decode(&finishedJob)
		detailResp.Body.Close()

		if finishedJob.Status == "completed" || finishedJob.Status == "failed" {
			break
		}
	}

	if finishedJob.Status != "completed" {
		t.Fatalf("expected verified job status 'completed', got %q (error: %s)", finishedJob.Status, finishedJob.Error)
	}

	// 3. Cancel transfer endpoint check
	cancelResp, err := client.Post(fmt.Sprintf("%s/api/transfers/%d/cancel", env.httpServer.URL, finishedJob.ID), "application/json", nil)
	if err != nil || cancelResp.StatusCode != http.StatusOK {
		t.Fatalf("cancel transfer request failed: %v", err)
	}
	cancelResp.Body.Close()
}

// Test Ticket 12: Network & Security Observability (Bandwidth, Internet Egress, Sockets, Exposure)
func TestTicket12_NetworkAndSecurityObservability(t *testing.T) {
	env := setupTestEnv(t)
	client := loginClient(t, env)

	hostID, _ := env.db.CreateHost(&store.Host{
		Name: "Production Web & DB",
		Host: "192.168.1.100",
		Port: 22,
		User: "root",
	})

	// Mock batch metrics output with all 12 sections
	env.mockRunner.Handlers[inspector.SystemMetricsBatchCmd] = func() (string, string, int, error) {
		sections := []string{
			"6.1.0-21-amd64",                                                        // 0: uname -r
			"PRETTY_NAME=\"Debian GNU/Linux 12 (bookworm)\"\nVERSION=\"12\"",          // 1: os-release
			" 16:30:00 up 10 days, 2:00, load average: 0.25, 0.35, 0.40",            // 2: uptime
			"Mem: 8388608000 4194304000 4194304000",                                  // 3: free
			"/dev/sda1 52428800 20971520 31457280 40% /",                             // 4: df
			"0",                                                                      // 5: dmesg errors
			"10485760 20971520",                                                      // 6: /proc/net/dev (rx tx bytes)
			"12.8",                                                                   // 7: ping 1.1.1.1 time=12.8ms
			"103.145.22.8\n",                                                         // 8: public IP
			"192.168.1.100:80 203.0.113.1:54321\n192.168.1.100:80 203.0.113.1:54322\n192.168.1.100:443 198.51.100.5:41234", // 9: established sockets
			"LISTEN 0.0.0.0:80 users:((\"nginx\",pid=100,fd=3))\nLISTEN 127.0.0.1:3306 users:((\"mysqld\",pid=101,fd=4))\nLISTEN 0.0.0.0:6379 users:((\"redis-server\",pid=102,fd=5))", // 10: listening ports
			"14\n", // 11: failed logins
		}
		return strings.Join(sections, "\n---\n"), "", 0, nil
	}

	err := env.inspector.InspectHost(hostID)
	if err != nil {
		t.Fatalf("inspect host failed: %v", err)
	}

	h, err := env.db.GetHost(hostID)
	if err != nil || h == nil {
		t.Fatalf("get host failed: %v", err)
	}

	// 1. Verify Bandwidth
	if h.NetRxBytes != 10485760 || h.NetTxBytes != 20971520 {
		t.Fatalf("unexpected net bytes: rx=%d, tx=%d", h.NetRxBytes, h.NetTxBytes)
	}

	// 2. Verify Internet Egress & Latency
	if !h.InternetOnline {
		t.Fatalf("expected InternetOnline to be true")
	}
	if h.InternetLatencyMs != 13 {
		t.Fatalf("expected InternetLatencyMs 13 (rounded from 12.8), got %d", h.InternetLatencyMs)
	}
	if h.PublicIP != "103.145.22.8" {
		t.Fatalf("expected public IP '103.145.22.8', got %q", h.PublicIP)
	}

	// 3. Verify Active Connections & Top Remote IPs
	if h.ActiveConnCount != 3 {
		t.Fatalf("expected 3 active established conns, got %d", h.ActiveConnCount)
	}
	var topConns []store.TopConn
	if err := json.Unmarshal([]byte(h.TopConnections), &topConns); err != nil {
		t.Fatalf("unmarshal top connections: %v", err)
	}
	if len(topConns) < 2 {
		t.Fatalf("expected at least 2 top remote IPs, got %d", len(topConns))
	}
	if topConns[0].RemoteIP != "203.0.113.1" || topConns[0].Count != 2 {
		t.Fatalf("expected top IP 203.0.113.1 with count 2, got %+v", topConns[0])
	}
	if topConns[1].RemoteIP != "198.51.100.5" || topConns[1].Count != 1 {
		t.Fatalf("expected second IP 198.51.100.5 with count 1, got %+v", topConns[1])
	}

	// 4. Verify Listening Ports & Security Exposure Risk
	var ports []store.ListeningPort
	if err := json.Unmarshal([]byte(h.ListeningPorts), &ports); err != nil {
		t.Fatalf("unmarshal listening ports: %v", err)
	}
	if len(ports) != 3 {
		t.Fatalf("expected 3 listening ports, got %d", len(ports))
	}
	// Port 80 (nginx): Public, low risk
	if ports[0].Port != "80" || !ports[0].Public || ports[0].Risk != "low" {
		t.Fatalf("unexpected port 80 status: %+v", ports[0])
	}
	// Port 3306 (mysql): Localhost, low risk
	if ports[1].Port != "3306" || ports[1].Public || ports[1].Risk != "low" {
		t.Fatalf("unexpected port 3306 status: %+v", ports[1])
	}
	// Port 6379 (redis): Public 0.0.0.0, high risk!
	if ports[2].Port != "6379" || !ports[2].Public || ports[2].Risk != "high" {
		t.Fatalf("expected port 6379 to have high risk, got: %+v", ports[2])
	}

	// 5. Verify Failed Logins
	if h.FailedLoginsCount != 14 {
		t.Fatalf("expected 14 failed logins, got %d", h.FailedLoginsCount)
	}

	// 6. Verify Web API returns all network details
	resp, err := client.Get(fmt.Sprintf("%s/api/hosts/%d", env.httpServer.URL, hostID))
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("api get host failed: %v", err)
	}
	var apiData struct {
		Host store.Host `json:"host"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&apiData)
	resp.Body.Close()

	if !apiData.Host.InternetOnline || apiData.Host.PublicIP != "103.145.22.8" {
		t.Fatalf("api host missing network data: %+v", apiData.Host)
	}
}

// Ticket 13: Legacy Server & CentOS 6 Compatibility
func TestTicket13_LegacyServerCompatibility(t *testing.T) {
	env := setupTestEnv(t)
	client := loginClient(t, env)

	// 1. Test Key Regeneration Endpoint
	regenResp, err := client.Post(env.httpServer.URL+"/api/settings/regenerate-ssh-key", "application/json", nil)
	if err != nil || regenResp.StatusCode != http.StatusOK {
		t.Fatalf("regenerate ssh key failed: %v, status: %d", err, regenResp.StatusCode)
	}
	var regenData map[string]string
	_ = json.NewDecoder(regenResp.Body).Decode(&regenData)
	regenResp.Body.Close()

	if !strings.HasPrefix(regenData["ssh_public_key"], "ssh-rsa") {
		t.Fatalf("expected regenerated key to be ssh-rsa, got %s", regenData["ssh_public_key"])
	}

	// 2. Create CentOS 6 Legacy Host
	hostID, _ := env.db.CreateHost(&store.Host{
		Name: "CentOS 6 Legacy DB",
		Host: "192.168.100.60",
		Port: 22,
		User: "root",
	})

	// 3. Mock SystemMetricsBatchCmd for CentOS 6 environment
	// CentOS 6: /etc/redhat-release instead of /etc/os-release, free -b with -/+ buffers/cache, no dmesg --level, ss without -H
	env.mockRunner.Handlers[inspector.SystemMetricsBatchCmd] = func() (string, string, int, error) {
		out := "2.6.32-754.35.1.el6.x86_64\n" +
			"---\n" +
			"CentOS release 6.10 (Final)\n" +
			"---\n" +
			" 10:20:00 up 450 days,  1:12,  1 user,  load average: 0.20, 0.15, 0.10\n" +
			"---\n" +
			"             total       used       free     shared    buffers     cached\n" +
			"Mem:       4194304    3800000     394304          0     300000    2500000\n" +
			"-/+ buffers/cache:    1000000    3194304\n" +
			"Swap:      2097152          0    2097152\n" +
			"---\n" +
			"Filesystem     1K-blocks    Used Available Use% Mounted on\n" +
			"/dev/sda1       41943040 8388608  31414432  22% /\n" +
			"---\n" +
			"0\n" +
			"---\n" +
			"10000000 20000000\n" +
			"---\n" +
			"15.4\n" +
			"---\n" +
			"203.0.113.55\n" +
			"---\n" +
			"192.168.100.60:22 192.168.100.1:54321\n" +
			"---\n" +
			"LISTEN 0.0.0.0:3306 mysqld\n" +
			"---\n" +
			"3\n"
		return out, "", 0, nil
	}

	// 4. Setup Desired Rule for SysVinit service "mysqld"
	_ = env.db.SaveDesiredRule(&store.DesiredRule{
		HostID:   hostID,
		Kind:     "service",
		Target:   "mysqld",
		Expected: "active",
		Enabled:  true,
	})

	// Mock stopped service status and log excerpt
	env.mockRunner.DefaultExec = func(cmd string) (string, string, int, error) {
		if strings.Contains(cmd, "mysqld") {
			if strings.Contains(cmd, "/var/log") || strings.Contains(cmd, "tail") {
				return "2026-09-09 10:15:00 [ERROR] InnoDB: Out of memory\n2026-09-09 10:15:01 [ERROR] mysqld died", "", 0, nil
			}
			return "inactive", "", 0, nil
		}
		return "", "", 0, nil
	}

	// 5. Run Inspection
	err = env.inspector.InspectHost(hostID)
	if err != nil {
		t.Fatalf("inspection on centos 6 host failed: %v", err)
	}

	h, _ := env.db.GetHost(hostID)
	// Verify OS parsed from /etc/redhat-release
	if h.OSInfo != "CentOS release 6.10 (Final)" {
		t.Fatalf("expected OS 'CentOS release 6.10 (Final)', got '%s'", h.OSInfo)
	}

	// Verify RAM parsed from -/+ buffers/cache: used should be 1,000,000 bytes (not 3,800,000)
	if h.RAMUsedBytes != 1000000 || h.RAMTotalBytes != 4194304 {
		t.Fatalf("expected RAM used 1000000 (after buffers/cache), got used=%d, total=%d", h.RAMUsedBytes, h.RAMTotalBytes)
	}

	// Verify Lifecycle Score docked for EOL CentOS 6
	if h.LifecycleScore > 70 || !strings.Contains(h.LifecycleNotes, "CentOS release 6") {
		t.Fatalf("expected lifecycle score penalty for EOL CentOS 6, got score=%d, notes=%s", h.LifecycleScore, h.LifecycleNotes)
	}

	// Verify service drift detected and root cause pulled from /var/log/
	alerts, _ := env.db.ListActiveAlerts()
	foundAlert := false
	for _, alt := range alerts {
		if alt.HostID == hostID && strings.Contains(alt.Message, "mysqld") {
			foundAlert = true
			if !strings.Contains(alt.RootCause, "InnoDB: Out of memory") {
				t.Fatalf("expected SysVinit log excerpt in root cause, got: %s", alt.RootCause)
			}
		}
	}
	if !foundAlert {
		t.Fatalf("expected alert for stopped mysqld service on CentOS 6 host")
	}
}

func TestTicket14_EncryptedSystemSnapshotAndGitHubSyncAPI(t *testing.T) {
	env1 := setupTestEnv(t)
	client1 := loginClient(t, env1)

	// 1. Status on fresh DB
	resp, err := http.Get(env1.httpServer.URL + "/api/snapshot/status")
	if err != nil {
		t.Fatalf("status get: %v", err)
	}
	var statusResp struct {
		Fresh     bool `json:"fresh"`
		HostCount int  `json:"host_count"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&statusResp)
	resp.Body.Close()
	if !statusResp.Fresh || statusResp.HostCount != 0 {
		t.Fatalf("expected fresh=true, host_count=0, got fresh=%v, count=%d", statusResp.Fresh, statusResp.HostCount)
	}

	// 2. Add host to env1
	hID, err := env1.db.CreateHost(&store.Host{
		Name: "Backup-Target-Host",
		Host: "192.168.10.50",
		Port: 22,
		User: "root",
	})
	if err != nil {
		t.Fatalf("create host: %v", err)
	}
	_ = env1.db.SaveDesiredRule(&store.DesiredRule{
		HostID:   hID,
		Kind:     "container",
		Target:   "postgres",
		Expected: "running",
		Enabled:  true,
	})

	// Verify no longer fresh
	resp2, _ := http.Get(env1.httpServer.URL + "/api/snapshot/status")
	_ = json.NewDecoder(resp2.Body).Decode(&statusResp)
	resp2.Body.Close()
	if statusResp.Fresh || statusResp.HostCount != 1 {
		t.Fatalf("expected fresh=false, host_count=1, got fresh=%v, count=%d", statusResp.Fresh, statusResp.HostCount)
	}

	// 3. Export snapshot via API
	exportPayload, _ := json.Marshal(map[string]string{"passphrase": "VaultSecretKey99!"})
	expResp, err := client1.Post(env1.httpServer.URL+"/api/snapshot/export", "application/json", bytes.NewReader(exportPayload))
	if err != nil {
		t.Fatalf("export post: %v", err)
	}
	encryptedData, err := io.ReadAll(expResp.Body)
	expResp.Body.Close()
	if err != nil || len(encryptedData) == 0 {
		t.Fatalf("expected non-empty encrypted data from export: %v", err)
	}
	if !bytes.HasPrefix(encryptedData, snapshot.MagicHeader) {
		t.Fatalf("expected encrypted data to have snapshot.MagicHeader")
	}

	// 4. Create fresh second environment
	env2 := setupTestEnv(t)

	// Verify env2 starts fresh
	fresh2, _ := env2.db.IsFresh()
	if !fresh2 {
		t.Fatalf("expected env2 to start fresh")
	}

	// 5. Import into env2 using multipart form (unauthenticated allowed since env2 is fresh)
	var b bytes.Buffer
	w := io.MultiWriter(&b)
	boundary := "---------------------------pantauBoundary123"
	w.Write([]byte("--" + boundary + "\r\n"))
	w.Write([]byte("Content-Disposition: form-data; name=\"passphrase\"\r\n\r\n"))
	w.Write([]byte("VaultSecretKey99!\r\n"))
	w.Write([]byte("--" + boundary + "\r\n"))
	w.Write([]byte("Content-Disposition: form-data; name=\"file\"; filename=\"pantau-state.enc\"\r\n"))
	w.Write([]byte("Content-Type: application/octet-stream\r\n\r\n"))
	w.Write(encryptedData)
	w.Write([]byte("\r\n--" + boundary + "--\r\n"))

	importReq, err := http.NewRequest(http.MethodPost, env2.httpServer.URL+"/api/snapshot/import", &b)
	if err != nil {
		t.Fatalf("new import req: %v", err)
	}
	importReq.Header.Set("Content-Type", "multipart/form-data; boundary="+boundary)

	impResp, err := http.DefaultClient.Do(importReq)
	if err != nil {
		t.Fatalf("import do: %v", err)
	}
	defer impResp.Body.Close()
	if impResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(impResp.Body)
		t.Fatalf("import failed (%d): %s", impResp.StatusCode, string(body))
	}

	// Verify env2 now has the restored host and rule
	hosts2, err := env2.db.ListHosts()
	if err != nil || len(hosts2) != 1 {
		t.Fatalf("expected 1 host in env2 after restore, got %d, err=%v", len(hosts2), err)
	}
	if hosts2[0].Name != "Backup-Target-Host" || hosts2[0].Host != "192.168.10.50" {
		t.Fatalf("host mismatch in env2: %+v", hosts2[0])
	}

	rules2, _ := env2.db.ListDesiredRules(hosts2[0].ID)
	if len(rules2) != 1 || rules2[0].Target != "postgres" {
		t.Fatalf("rules mismatch in env2: %+v", rules2)
	}

	fresh2After, _ := env2.db.IsFresh()
	if fresh2After {
		t.Fatalf("expected env2 to no longer be fresh after import")
	}
}

func TestTicket15_NotesSearchAndOrdering(t *testing.T) {
	env := setupTestEnv(t)
	client := loginClient(t, env)

	// 1. Global Notes API (GET/PUT)
	getResp, err := client.Get(env.httpServer.URL + "/api/notes")
	if err != nil {
		t.Fatalf("GET /api/notes failed: %v", err)
	}
	var noteRes struct {
		Notes string `json:"notes"`
	}
	_ = json.NewDecoder(getResp.Body).Decode(&noteRes)
	getResp.Body.Close()
	if noteRes.Notes != "" {
		t.Fatalf("expected empty initial global notes, got %q", noteRes.Notes)
	}

	setBody, _ := json.Marshal(map[string]string{"notes": "Emergency procedure: contact on-call #123"})
	putReq, _ := http.NewRequest(http.MethodPut, env.httpServer.URL+"/api/notes", bytes.NewReader(setBody))
	putReq.Header.Set("Content-Type", "application/json")
	putResp, err := client.Do(putReq)
	if err != nil || putResp.StatusCode != http.StatusOK {
		t.Fatalf("PUT /api/notes failed: %v, code=%d", err, putResp.StatusCode)
	}
	putResp.Body.Close()

	getResp2, _ := client.Get(env.httpServer.URL + "/api/notes")
	_ = json.NewDecoder(getResp2.Body).Decode(&noteRes)
	getResp2.Body.Close()
	if noteRes.Notes != "Emergency procedure: contact on-call #123" {
		t.Fatalf("unexpected global notes: %q", noteRes.Notes)
	}

	// 2. Create Host with Note
	createPayload, _ := json.Marshal(map[string]interface{}{
		"name":  "Server-Alpha",
		"host":  "10.0.0.1",
		"port":  22,
		"user":  "root",
		"notes": "Alpha host note",
	})
	cResp, err := client.Post(env.httpServer.URL+"/api/hosts", "application/json", bytes.NewReader(createPayload))
	if err != nil || cResp.StatusCode != http.StatusCreated {
		t.Fatalf("POST /api/hosts Alpha failed: %v, code=%d", err, cResp.StatusCode)
	}
	var hostAlpha store.Host
	_ = json.NewDecoder(cResp.Body).Decode(&hostAlpha)
	cResp.Body.Close()
	if hostAlpha.Notes != "Alpha host note" {
		t.Fatalf("expected notes 'Alpha host note', got %q", hostAlpha.Notes)
	}

	createBeta, _ := json.Marshal(map[string]interface{}{
		"name":  "Server-Beta",
		"host":  "10.0.0.2",
		"port":  22,
		"user":  "root",
		"notes": "Beta host note",
	})
	cResp2, err := client.Post(env.httpServer.URL+"/api/hosts", "application/json", bytes.NewReader(createBeta))
	if err != nil || cResp2.StatusCode != http.StatusCreated {
		t.Fatalf("POST /api/hosts Beta failed: %v", err)
	}
	var hostBeta store.Host
	_ = json.NewDecoder(cResp2.Body).Decode(&hostBeta)
	cResp2.Body.Close()

	// 3. Update Host Note via dedicated endpoint
	updateNotePayload, _ := json.Marshal(map[string]string{"notes": "Updated Alpha note"})
	unReq, _ := http.NewRequest(http.MethodPut, fmt.Sprintf("%s/api/hosts/%d/notes", env.httpServer.URL, hostAlpha.ID), bytes.NewReader(updateNotePayload))
	unReq.Header.Set("Content-Type", "application/json")
	unResp, err := client.Do(unReq)
	if err != nil || unResp.StatusCode != http.StatusOK {
		t.Fatalf("PUT /api/hosts/{id}/notes failed: %v, code=%d", err, unResp.StatusCode)
	}
	unResp.Body.Close()

	detailResp, _ := client.Get(fmt.Sprintf("%s/api/hosts/%d", env.httpServer.URL, hostAlpha.ID))
	var detail struct {
		Host store.Host `json:"host"`
	}
	_ = json.NewDecoder(detailResp.Body).Decode(&detail)
	detailResp.Body.Close()
	if detail.Host.Notes != "Updated Alpha note" {
		t.Fatalf("expected 'Updated Alpha note', got %q", detail.Host.Notes)
	}

	// 4. Host Reordering
	// Currently Alpha is sort_order 1, Beta is sort_order 2
	reorderPayload, _ := json.Marshal(map[string]interface{}{
		"ids": []int64{hostBeta.ID, hostAlpha.ID},
	})
	roReq, _ := http.NewRequest(http.MethodPut, env.httpServer.URL+"/api/hosts/reorder", bytes.NewReader(reorderPayload))
	roReq.Header.Set("Content-Type", "application/json")
	roResp, err := client.Do(roReq)
	if err != nil || roResp.StatusCode != http.StatusOK {
		t.Fatalf("PUT /api/hosts/reorder failed: %v, code=%d", err, roResp.StatusCode)
	}
	roResp.Body.Close()

	// List hosts and verify Beta is now first
	listResp, _ := client.Get(env.httpServer.URL + "/api/hosts")
	var hostsList []store.Host
	_ = json.NewDecoder(listResp.Body).Decode(&hostsList)
	listResp.Body.Close()
	if len(hostsList) != 2 {
		t.Fatalf("expected 2 hosts, got %d", len(hostsList))
	}
	if hostsList[0].ID != hostBeta.ID || hostsList[1].ID != hostAlpha.ID {
		t.Fatalf("expected Beta first then Alpha, got: %+v", hostsList)
	}
}


