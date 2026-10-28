package web

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"pantau/internal/inspector"
	"pantau/internal/sshrunner"
	"pantau/internal/store"

	"github.com/pkg/sftp"
)

// mockTestSSHRunner implements sshrunner.Runner for testing native terminal injection & cleanup.
type mockTestSSHRunner struct {
	mu           sync.Mutex
	execLogs     []string
	execCallback func(cmd string) (stdout, stderr string, exitCode int, err error)
	closed       bool
}

func (m *mockTestSSHRunner) Exec(cmd string) (stdout, stderr string, exitCode int, err error) {
	return m.ExecTimeout(cmd, 10*time.Second)
}

func (m *mockTestSSHRunner) ExecTimeout(cmd string, timeout time.Duration) (stdout, stderr string, exitCode int, err error) {
	m.mu.Lock()
	m.execLogs = append(m.execLogs, cmd)
	m.mu.Unlock()
	if m.execCallback != nil {
		return m.execCallback(cmd)
	}
	return "", "", 0, nil
}

func (m *mockTestSSHRunner) Stream(cmd string, out io.Writer) error                                      { return nil }
func (m *mockTestSSHRunner) PipeCommand(cmd string, in io.Reader, out io.Writer) error                   { return nil }
func (m *mockTestSSHRunner) SFTP() (*sftp.Client, error)                                                 { return nil, nil }
func (m *mockTestSSHRunner) Terminal(in io.Reader, out io.Writer, c, r int, rc <-chan [2]int) error     { return nil }
func (m *mockTestSSHRunner) DialContext(ctx context.Context, network, addr string) (net.Conn, error)    { return nil, nil }
func (m *mockTestSSHRunner) Close() error {
	m.mu.Lock()
	m.closed = true
	m.mu.Unlock()
	return nil
}

func TestGenerateEphemeralKey(t *testing.T) {
	key, err := GenerateEphemeralKey(42, 5*time.Minute)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if key == nil {
		t.Fatal("key is nil")
	}
	if key.HostID != 42 {
		t.Errorf("expected hostID 42, got %d", key.HostID)
	}
	if !strings.HasPrefix(key.PublicKey, "ssh-rsa ") {
		t.Errorf("expected ssh-rsa prefix, got %s", key.PublicKey)
	}
	if !strings.Contains(key.PublicKey, fmt.Sprintf("pantau-ephemeral-%s-", key.ID)) {
		t.Errorf("expected comment with id %s, got %s", key.ID, key.PublicKey)
	}

	// Verify private key is valid PEM
	block, rest := pem.Decode(key.PrivateKey)
	if block == nil || len(rest) > 0 {
		t.Fatalf("failed to parse private key PEM")
	}
	if block.Type != "OPENSSH PRIVATE KEY" {
		t.Errorf("expected OPENSSH PRIVATE KEY block, got %s", block.Type)
	}
}

func TestNativeTicketSingleUseBurn(t *testing.T) {
	s := &Server{}
	key, err := GenerateEphemeralKey(1, 5*time.Minute)
	if err != nil {
		t.Fatal(err)
	}

	ticket := &NativeTicket{
		Token:        "secret-token-12345",
		HostID:       1,
		HostName:     "web-prod-01",
		HostAddress:  "192.168.1.100",
		HostPort:     22,
		HostUser:     "ubuntu",
		EphemeralKey: key,
		ExpiresAt:    time.Now().Add(60 * time.Second),
	}

	s.storeNativeTicket(ticket)

	// First redemption must succeed
	burned1, ok1 := s.getAndBurnNativeTicket("secret-token-12345")
	if !ok1 || burned1 == nil {
		t.Fatalf("expected first burn to succeed")
	}
	if burned1.HostName != "web-prod-01" {
		t.Errorf("expected web-prod-01, got %s", burned1.HostName)
	}

	// Second redemption must FAIL (Burn-After-Read enforcement)
	burned2, ok2 := s.getAndBurnNativeTicket("secret-token-12345")
	if ok2 || burned2 != nil {
		t.Fatalf("expected second burn to fail due to single-use invariant")
	}

	// Expired ticket must FAIL
	expiredTicket := &NativeTicket{
		Token:        "expired-token",
		ExpiresAt:    time.Now().Add(-1 * time.Second),
		EphemeralKey: key,
	}
	s.storeNativeTicket(expiredTicket)
	_, okExp := s.getAndBurnNativeTicket("expired-token")
	if okExp {
		t.Errorf("expected expired ticket redemption to fail")
	}
}

func TestValidateTerminalTemplate(t *testing.T) {
	// Missing %cmd% placeholder
	if err := validateTerminalTemplate("alacritty -e bash"); err == nil {
		t.Errorf("expected error for missing %%cmd%%")
	}

	// Disallowed shell metacharacter
	if err := validateTerminalTemplate("sh -c %cmd%; rm -rf /"); err == nil {
		t.Errorf("expected error for semicolon metacharacter")
	}
	if err := validateTerminalTemplate("sh -c %cmd% && echo evil"); err == nil {
		t.Errorf("expected error for && metacharacter")
	}

	// Non-existent executable
	if err := validateTerminalTemplate("totally_fake_term_binary_xyz -e %cmd%"); err == nil {
		t.Errorf("expected error for non-existent executable")
	}

	// Valid template (using 'sh' which exists on Linux/macOS/Git Bash)
	if err := validateTerminalTemplate("sh -c %cmd%"); err != nil {
		// Only check if sh exists on path
		t.Logf("valid template test note: %v", err)
	}
}

func TestScriptGenerators(t *testing.T) {
	key, err := GenerateEphemeralKey(10, 5*time.Minute)
	if err != nil {
		t.Fatal(err)
	}

	ticket := &NativeTicket{
		Token:        "tok-abc",
		HostID:       10,
		HostName:     "db-master",
		HostAddress:  "10.0.0.50",
		HostPort:     2222,
		HostUser:     "deploy",
		EphemeralKey: key,
	}

	// 1. Windows CMD Script (delegates cleanly to in-memory PowerShell)
	cmdScript := generateWindowsCmdScript(ticket)
	if !strings.Contains(cmdScript, "@echo off") {
		t.Errorf("expected @echo off in cmd script")
	}
	if !strings.Contains(cmdScript, "powershell.exe") {
		t.Errorf("expected powershell.exe in cmd script")
	}
	if !strings.Contains(cmdScript, "deploy@10.0.0.50") {
		t.Errorf("expected user@host in cmd script")
	}
	if strings.Contains(cmdScript, `del /f /q "%~f0"`) {
		t.Errorf("cmd script must not contain self-delete pattern (causes antivirus false positive)")
	}

	// 2. Unix Linux Script
	shScript := generateUnixScript(ticket, false)
	if strings.Contains(shScript, "\r") {
		t.Errorf("Unix script must not contain CRLF (\\r) characters")
	}
	if !strings.Contains(shScript, "#!/usr/bin/env bash\n") {
		t.Errorf("expected bash shebang with LF in sh script")
	}
	if !strings.Contains(shScript, "trap 'rm -f \"$KEYFILE\"") {
		t.Errorf("expected trap self-delete in sh script")
	}
	if !strings.Contains(shScript, "chmod 600 \"$KEYFILE\"") {
		t.Errorf("expected chmod 600 in sh script")
	}
	if !strings.Contains(shScript, "-p 2222 deploy@10.0.0.50") {
		t.Errorf("expected ssh command in sh script")
	}
	if !strings.Contains(shScript, "set +e\n") {
		t.Errorf("expected set +e before ssh command")
	}
	if !strings.Contains(shScript, "SSH_EXIT=$?") {
		t.Errorf("expected SSH_EXIT capture in sh script")
	}

	// 3. macOS Script (with exec $SHELL retention)
	macScript := generateUnixScript(ticket, true)
	if strings.Contains(macScript, "\r") {
		t.Errorf("macOS script must not contain CRLF (\\r) characters")
	}
	if !strings.Contains(macScript, "exec $SHELL") {
		t.Errorf("expected exec $SHELL in macOS script")
	}
}

func setupTestServerWithDB(t *testing.T, runner sshrunner.Runner) (*Server, *store.DB, *store.Host) {
	t.Helper()
	tmpDir, err := os.MkdirTemp("", "pantau_test_*")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = os.RemoveAll(tmpDir)
	})

	db, err := store.Open(filepath.Join(tmpDir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	host := &store.Host{
		Name: "Test Target",
		Host: "127.0.0.1",
		Port: 22,
		User: "testuser",
	}
	hID, err := db.CreateHost(host)
	if err != nil {
		t.Fatal(err)
	}
	host.ID = hID

	mockFactory := func(h string, p int, u, k string) (sshrunner.Runner, error) {
		return runner, nil
	}

	ins := inspector.New(db, mockFactory, nil)
	s := NewServer(db, ins, nil)
	return s, db, host
}

func TestNativeTerminalHTTPFlow(t *testing.T) {
	mockRunner := &mockTestSSHRunner{}
	s, _, host := setupTestServerWithDB(t, mockRunner)

	// Create test session cookie
	sessionToken := "test-session-token"
	s.sessions.Store(sessionToken, time.Now().Add(1*time.Hour))

	// 1. Test POST /api/terminal/native-ticket
	reqBody := bytes.NewBufferString(fmt.Sprintf(`{"host_id": %d}`, host.ID))
	req := httptest.NewRequest(http.MethodPost, "/api/terminal/native-ticket", reqBody)
	req.AddCookie(&http.Cookie{Name: "pantau_session", Value: sessionToken})
	rec := httptest.NewRecorder()

	s.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for ticket creation, got %d: %s", rec.Code, rec.Body.String())
	}

	var ticketResp struct {
		OK          bool   `json:"ok"`
		Ticket      string `json:"ticket"`
		ExpiresIn   int    `json:"expires_in"`
		CommandUnix string `json:"command_unix"`
		CommandWin  string `json:"command_win"`
		ProtocolURL string `json:"protocol_url"`
		StreamURL   string `json:"stream_url"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &ticketResp); err != nil {
		t.Fatalf("failed to decode ticket json: %v", err)
	}
	if !ticketResp.OK || ticketResp.Ticket == "" {
		t.Fatalf("expected valid ticket token, got empty")
	}
	if !strings.HasPrefix(ticketResp.ProtocolURL, "pantau://") {
		t.Errorf("expected protocol_url to start with pantau://, got %s", ticketResp.ProtocolURL)
	}
	if ticketResp.StreamURL == "" {
		t.Errorf("expected stream_url in ticket response")
	}

	// Verify injection command was sent to mock runner
	mockRunner.mu.Lock()
	hasInjected := false
	closed := mockRunner.closed
	for _, cmd := range mockRunner.execLogs {
		if strings.Contains(cmd, "pantau-ephemeral-") && strings.Contains(cmd, "authorized_keys") {
			hasInjected = true
			break
		}
	}
	mockRunner.mu.Unlock()
	if !hasInjected {
		t.Errorf("expected ephemeral key injection command in runner logs")
	}
	if !closed {
		t.Errorf("expected mockRunner to be closed after key injection to prevent connection leak")
	}

	// 2. Test GET /api/terminal/native-download (First time - Success & Burn)
	downReq := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/terminal/native-download?ticket=%s&os=linux", ticketResp.Ticket), nil)
	downRec := httptest.NewRecorder()
	s.ServeHTTP(downRec, downReq)

	if downRec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for download, got %d: %s", downRec.Code, downRec.Body.String())
	}
	if !strings.Contains(downRec.Body.String(), "testuser@127.0.0.1") {
		t.Errorf("expected script content with host address, got %s", downRec.Body.String())
	}
	if !strings.Contains(downRec.Header().Get("Content-Disposition"), "pantau-") {
		t.Errorf("expected Content-Disposition with pantau filename, got %s", downRec.Header().Get("Content-Disposition"))
	}

	// 3. Test GET /api/terminal/native-download (Second time - Must return 410 Gone due to Burn-After-Read)
	downReq2 := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/terminal/native-download?ticket=%s&os=linux", ticketResp.Ticket), nil)
	downRec2 := httptest.NewRecorder()
	s.ServeHTTP(downRec2, downReq2)

	if downRec2.Code != http.StatusGone {
		t.Fatalf("expected 410 Gone for burned ticket, got %d: %s", downRec2.Code, downRec2.Body.String())
	}
}

func TestNativeTerminalStreamingRun(t *testing.T) {
	mockRunner := &mockTestSSHRunner{}
	s, _, host := setupTestServerWithDB(t, mockRunner)
	sessionToken := "test-session-stream"
	s.sessions.Store(sessionToken, time.Now().Add(1*time.Hour))

	// Get ticket
	reqBody := bytes.NewBufferString(fmt.Sprintf(`{"host_id": %d}`, host.ID))
	req := httptest.NewRequest(http.MethodPost, "/api/terminal/native-ticket", reqBody)
	req.AddCookie(&http.Cookie{Name: "pantau_session", Value: sessionToken})
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)

	var ticketResp struct {
		Ticket string `json:"ticket"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &ticketResp)

	// Stream run via curl
	runReq := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/terminal/native-run?ticket=%s", ticketResp.Ticket), nil)
	runReq.Header.Set("User-Agent", "curl/7.88.1")
	runRec := httptest.NewRecorder()
	s.ServeHTTP(runRec, runReq)

	if runRec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for native-run, got %d: %s", runRec.Code, runRec.Body.String())
	}
	if !strings.Contains(runRec.Body.String(), "#!/usr/bin/env bash") {
		t.Errorf("expected bash script stream, got %s", runRec.Body.String())
	}

	// Replay must fail
	runRec2 := httptest.NewRecorder()
	s.ServeHTTP(runRec2, runReq)
	if runRec2.Code != http.StatusGone {
		t.Fatalf("expected 410 Gone for burned stream ticket, got %d", runRec2.Code)
	}
}

func TestSettingsTerminalLauncherValidation(t *testing.T) {
	s, _, _ := setupTestServerWithDB(t, &mockTestSSHRunner{})
	sessionToken := "admin-session-settings"
	s.sessions.Store(sessionToken, time.Now().Add(1*time.Hour))

	// Invalid template (missing %cmd%)
	badPayload := `{"terminal_launcher_mode": "custom", "terminal_launcher_template": "alacritty -e bash"}`
	req := httptest.NewRequest(http.MethodPost, "/api/settings", bytes.NewBufferString(badPayload))
	req.AddCookie(&http.Cookie{Name: "pantau_session", Value: sessionToken})
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 Bad Request for missing %%cmd%%, got %d: %s", rec.Code, rec.Body.String())
	}

	// Dangerous metacharacter
	evilPayload := `{"terminal_launcher_mode": "custom", "terminal_launcher_template": "sh -c %cmd%; rm -rf /"}`
	reqEvil := httptest.NewRequest(http.MethodPost, "/api/settings", bytes.NewBufferString(evilPayload))
	reqEvil.AddCookie(&http.Cookie{Name: "pantau_session", Value: sessionToken})
	recEvil := httptest.NewRecorder()
	s.ServeHTTP(recEvil, reqEvil)

	if recEvil.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 Bad Request for disallowed metacharacter, got %d", recEvil.Code)
	}
}

func TestHostKeyMutexDoesNotPanicTunnelReaper(t *testing.T) {
	s := &Server{}
	// Calling getHostKeyMutex must not store string keys into tunnelRunners
	_ = s.getHostKeyMutex(42)
	// Reaper must run without panic
	reaped := s.ReapIdleTunnelRunners(time.Minute)
	if reaped != 0 {
		t.Errorf("expected 0 reaped, got %d", reaped)
	}
}

func TestNativeTicketDoesNotPanicAuthMiddleware(t *testing.T) {
	s, _, _ := setupTestServerWithDB(t, &mockTestSSHRunner{})
	ticket := &NativeTicket{
		Token:     "test-token-safe",
		ExpiresAt: time.Now().Add(60 * time.Second),
	}
	s.storeNativeTicket(ticket)

	// Sending a request with Cookie: pantau_session=test-token-safe must NOT panic
	req := httptest.NewRequest(http.MethodGet, "/api/hosts", nil)
	req.AddCookie(&http.Cookie{Name: "pantau_session", Value: "test-token-safe"})
	rec := httptest.NewRecorder()

	s.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 Unauthorized, got %d", rec.Code)
	}
}

func TestBuildLaunchCommandEscaping(t *testing.T) {
	// Darwin AppleScript quotes escaping
	cmdDarwin, err := buildLaunchCommand("darwin", "Terminal", "", "/path/to/key", "user", "127.0.0.1", 22, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	darwinScript := cmdDarwin.Args[len(cmdDarwin.Args)-1]
	if !strings.Contains(darwinScript, `\"/path/to/key\"`) {
		t.Errorf("expected escaped quotes in AppleScript: %s", darwinScript)
	}
	if !strings.Contains(darwinScript, "-o HostKeyAlgorithms=+ssh-rsa,ssh-dss") || !strings.Contains(darwinScript, "-o PubkeyAcceptedKeyTypes=+ssh-rsa") {
		t.Errorf("expected legacy OpenSSH compatibility flags in launch command: %s", darwinScript)
	}

	// Linux custom template execution
	cmdLinux, err := buildLaunchCommand("linux", "/bin/sh", "alacritty -e %cmd%", "/path/to/key", "user", "127.0.0.1", 22, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cmdLinux.Path != "/bin/sh" && !strings.HasSuffix(cmdLinux.Path, "sh") {
		t.Errorf("expected /bin/sh runner, got %s", cmdLinux.Path)
	}
}

func TestSafeHostRegexIPv6(t *testing.T) {
	validHosts := []string{
		"192.168.1.1",
		"example.com",
		"my-host.internal",
		"2001:db8::1",
		"::1",
		"fe80::1ff:fe00:1",
		"[2001:db8::1]",
	}
	for _, h := range validHosts {
		if !safeHostRegex.MatchString(h) {
			t.Errorf("expected valid host %s to match safeHostRegex", h)
		}
	}

	invalidHosts := []string{
		"host; rm -rf /",
		"host && evil",
		"host | cat",
		"host`whoami`",
		"host$(id)",
		"host name",
	}
	for _, h := range invalidHosts {
		if safeHostRegex.MatchString(h) {
			t.Errorf("expected invalid host %s to NOT match safeHostRegex", h)
		}
	}
}

func TestSanitizeFilename(t *testing.T) {
	cases := []struct {
		input    string
		expected string
	}{
		{"web-prod-01", "web-prod-01"},
		{"Server: Master (Prod)", "Server_Master_Prod"},
		{"db_01.internal", "db_01.internal"},
		{"   ", "host"},
		{":::", "host"},
	}
	for _, c := range cases {
		got := sanitizeFilename(c.input)
		if got != c.expected {
			t.Errorf("sanitizeFilename(%q) = %q, expected %q", c.input, got, c.expected)
		}
	}
}

func TestNativeTerminalHostIDFromString(t *testing.T) {
	mockRunner := &mockTestSSHRunner{}
	s, _, host := setupTestServerWithDB(t, mockRunner)

	sessionToken := "test-session-string-id"
	s.sessions.Store(sessionToken, time.Now().Add(1*time.Hour))

	// 1. Test POST /api/terminal/native-ticket with string host_id (e.g. from frontend button)
	reqBody := bytes.NewBufferString(fmt.Sprintf(`{"host_id": "%d"}`, host.ID))
	req := httptest.NewRequest(http.MethodPost, "/api/terminal/native-ticket", reqBody)
	req.AddCookie(&http.Cookie{Name: "pantau_session", Value: sessionToken})
	rec := httptest.NewRecorder()

	s.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for ticket creation with string host_id, got %d: %s", rec.Code, rec.Body.String())
	}

	// 2. Verify Toolbar HTML wiring: Button next to reconnect uses openNativeTerminalForTab
	html := string(embeddedHTML)
	if strings.Contains(html, "onclick=\"openNativeTerminalForHost('${tab.hostID}')\"") {
		t.Errorf("found unquoted/string hostID call in tab workspace toolbar: must use openNativeTerminalForTab('${tab.id}')")
	}
	if !strings.Contains(html, "onclick=\"openNativeTerminalForTab('${tab.id}')\"") {
		t.Errorf("expected openNativeTerminalForTab('${tab.id}') in tab workspace toolbar next to reconnect button")
	}
}

func TestCopyNativeCmdInsecureContextFallback(t *testing.T) {
	html := string(embeddedHTML)

	copyFnIdx := strings.Index(html, "function copyNativeCmd")
	if copyFnIdx == -1 {
		t.Fatalf("function copyNativeCmd not found in index.html")
	}

	copyFnEnd := strings.Index(html[copyFnIdx:], "function toggleTermTemplateInput")
	if copyFnEnd == -1 {
		copyFnEnd = len(html) - copyFnIdx
	}
	copyFnBody := html[copyFnIdx : copyFnIdx+copyFnEnd]

	if !strings.Contains(copyFnBody, "copyTextToClipboard") {
		t.Errorf("copyNativeCmd must use copyTextToClipboard to support non-HTTPS LAN access (execCommand fallback), but got: %s", copyFnBody)
	}

	if strings.Contains(copyFnBody, "navigator.clipboard.writeText") && !strings.Contains(copyFnBody, "navigator.clipboard &&") {
		t.Errorf("copyNativeCmd must not directly call navigator.clipboard.writeText without checking navigator.clipboard (causes TypeError in insecure contexts)")
	}
}

func TestNativeTerminalActivePath(t *testing.T) {
	// 1. buildLaunchCommand with active path
	cmd, err := buildLaunchCommand("linux", "/usr/bin/gnome-terminal", "", "/tmp/key", "root", "192.168.1.10", 22, "/var/log/nginx")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	cmdStr := strings.Join(cmd.Args, " ")
	if !strings.Contains(cmdStr, `-t "cd '/var/log/nginx' 2>/dev/null; exec bash -l || exec sh -l"`) {
		t.Errorf("expected cd to /var/log/nginx with bash/sh fallback in cmd, got: %s", cmdStr)
	}

	// 2. buildLaunchCommand with path escaping (single quotes)
	cmdQuote, err := buildLaunchCommand("linux", "/usr/bin/gnome-terminal", "", "/tmp/key", "root", "192.168.1.10", 22, "/home/user/app's")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	quoteCmdStr := strings.Join(cmdQuote.Args, " ")
	if !strings.Contains(quoteCmdStr, `/home/user/app'\''s`) {
		t.Errorf("expected escaped single quote in path, got: %s", quoteCmdStr)
	}

	// 3. generateUnixScript with InitialDir
	key, err := GenerateEphemeralKey(10, 5*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	ticket := &NativeTicket{
		Token:        "ticket-path-test",
		HostID:       10,
		HostName:     "srv-app",
		HostAddress:  "10.0.0.5",
		HostPort:     2222,
		HostUser:     "deploy",
		InitialDir:   "/var/www/html",
		EphemeralKey: key,
	}
	unixScript := generateUnixScript(ticket, false)
	if !strings.Contains(unixScript, `-t "cd '/var/www/html' 2>/dev/null; exec bash -l || exec sh -l"`) {
		t.Errorf("expected unix script to contain directory navigation: %s", unixScript)
	}

	// 4. generateWindowsCmdScript with InitialDir
	winScript := generateWindowsCmdScript(ticket)
	if !strings.Contains(winScript, `-t \"cd '/var/www/html' 2>/dev/null; exec bash -l || exec sh -l\"`) {
		t.Errorf("expected windows cmd script to contain escaped directory navigation: %s", winScript)
	}

	// 5. Test API ticket creation with path parameter
	mockRunner := &mockTestSSHRunner{}
	s, _, host := setupTestServerWithDB(t, mockRunner)
	sessionToken := "test-session-active-path"
	s.sessions.Store(sessionToken, time.Now().Add(1*time.Hour))

	reqBody := bytes.NewBufferString(fmt.Sprintf(`{"host_id": %d, "path": "/etc/systemd"}`, host.ID))
	req := httptest.NewRequest(http.MethodPost, "/api/terminal/native-ticket", reqBody)
	req.AddCookie(&http.Cookie{Name: "pantau_session", Value: sessionToken})
	rec := httptest.NewRecorder()

	s.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d: %s", rec.Code, rec.Body.String())
	}

	var ticketRes struct {
		Ticket string `json:"ticket"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &ticketRes); err != nil {
		t.Fatalf("failed to decode ticket json: %v", err)
	}

	// Test streaming run retrieves script with path
	runReq := httptest.NewRequest(http.MethodGet, "/api/terminal/native-run?ticket="+ticketRes.Ticket, nil)
	runRec := httptest.NewRecorder()
	s.ServeHTTP(runRec, runReq)
	if runRec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for native-run, got %d: %s", runRec.Code, runRec.Body.String())
	}
	if !strings.Contains(runRec.Body.String(), `/etc/systemd`) {
		t.Errorf("expected script output to navigate to /etc/systemd: %s", runRec.Body.String())
	}

	// 6. Verify File Manager and Terminal Tab HTML wiring
	html := string(embeddedHTML)
	if !strings.Contains(html, "openNativeTerminalForPath") {
		t.Errorf("expected openNativeTerminalForPath function and button in HTML")
	}
	if !strings.Contains(html, "onclick=\"openNativeTerminalForPath('${escapeHtml(data.path)}')\"") {
		t.Errorf("expected File Manager toolbar to have openNativeTerminalForPath button")
	}
	if !strings.Contains(html, "openNativeTerminalFromGlobalPin") {
		t.Errorf("expected Global Bookmarks modal to have openNativeTerminalFromGlobalPin button")
	}
}
