package web

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"pantau/internal/store"
)

func TestIsProtectedPathString(t *testing.T) {
	tests := []struct {
		path      string
		protected bool
	}{
		{"/", true},
		{".", true},
		{"", true},
		{"/bin", true},
		{"/bin/sh", true},
		{"/boot", true},
		{"/boot/vmlinuz", true},
		{"/dev", true},
		{"/dev/null", true},
		{"/etc", true},
		{"/etc/shadow", true},
		{"/etc/nginx/nginx.conf", true},
		{"/lib", true},
		{"/lib64", true},
		{"/lib32", true},
		{"/proc", true},
		{"/proc/1/cmdline", true},
		{"/root", true},
		{"/root/.ssh/authorized_keys", true},
		{"/sbin", true},
		{"/sbin/iptables", true},
		{"/sys", true},
		{"/usr", true},
		{"/usr/local/bin/app", true},
		{"/run", true},
		{"/run/systemd", true},

		// Path traversal attempts
		{"/var/www/../../etc/passwd", true},
		{"/home/ubuntu/../../root/.bashrc", true},
		{"etc/shadow", true},
		{"../etc/shadow", true},

		// Allowed user / app locations
		{"/var/www/html/index.php", false},
		{"/home/user/project/file.txt", false},
		{"/opt/myapp/config.yaml", false},
		{"/tmp/test.log", false},
		{"/srv/data/file.csv", false},
	}

	for _, tt := range tests {
		got := isProtectedPathString(tt.path)
		if got != tt.protected {
			t.Errorf("isProtectedPathString(%q) = %v; want %v", tt.path, got, tt.protected)
		}
	}
}

func TestFormatPresetExecution(t *testing.T) {
	// 1. Simple command
	cmd1 := formatPresetExecution("htop", false)
	if !strings.Contains(cmd1, "command -v htop") || !strings.Contains(cmd1, "Binary 'htop' not found") {
		t.Errorf("unexpected format for htop: %s", cmd1)
	}

	// 2. Command with arguments & sudo
	cmd2 := formatPresetExecution("sudo mytop -u root -p", false)
	if !strings.Contains(cmd2, "command -v mytop") || !strings.Contains(cmd2, "sudo mytop -u root -p") {
		t.Errorf("unexpected format for sudo mytop: %s", cmd2)
	}

	// 3. Command with tmux enabled
	cmd3 := formatPresetExecution("docker stats", true)
	if !strings.Contains(cmd3, "tmux new-session -A -s pantau-docker") {
		t.Errorf("unexpected tmux format for docker stats: %s", cmd3)
	}
}

func TestTerminalPresetsAPI(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "pantau-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "pantau.db")
	db, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	srv := NewServer(db, nil, nil)
	token := "valid_test_token"
	srv.sessions.Store(token, time.Now().Add(time.Hour))

	authReq := func(req *http.Request) {
		req.AddCookie(&http.Cookie{
			Name:  "pantau_session",
			Value: token,
		})
	}

	// 1. Test GET /api/presets (should include 3 seeded presets)
	req1 := httptest.NewRequest(http.MethodGet, "/api/presets?host_id=all", nil)
	authReq(req1)
	rr1 := httptest.NewRecorder()
	srv.ServeHTTP(rr1, req1)

	if rr1.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr1.Code)
	}
	var presets []store.TerminalPreset
	if err := json.NewDecoder(rr1.Body).Decode(&presets); err != nil {
		t.Fatal(err)
	}
	if len(presets) < 3 {
		t.Fatalf("expected at least 3 seeded presets, got %d", len(presets))
	}

	// 2. Test POST /api/presets
	newPresetJSON := `{"name":"Nginx Logs","command":"tail -f /var/log/nginx/access.log","sort_order":5,"use_tmux":false}`
	req2 := httptest.NewRequest(http.MethodPost, "/api/presets", bytes.NewBufferString(newPresetJSON))
	authReq(req2)
	rr2 := httptest.NewRecorder()
	srv.ServeHTTP(rr2, req2)

	if rr2.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d", rr2.Code)
	}
	var created store.TerminalPreset
	if err := json.NewDecoder(rr2.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}
	if created.ID == 0 || created.Name != "Nginx Logs" {
		t.Fatalf("unexpected created preset: %+v", created)
	}

	// 3. Test GET /api/presets/{id}
	idStr := strconv.FormatInt(created.ID, 10)
	req3 := httptest.NewRequest(http.MethodGet, "/api/presets/"+idStr, nil)
	authReq(req3)
	rr3 := httptest.NewRecorder()
	srv.ServeHTTP(rr3, req3)
	if rr3.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr3.Code)
	}

	// 4. Test PUT /api/presets/{id}
	updateJSON := `{"name":"Nginx Logs Updated","command":"tail -n 50 -f /var/log/nginx/access.log","sort_order":6,"use_tmux":true}`
	req4 := httptest.NewRequest(http.MethodPut, "/api/presets/"+idStr, bytes.NewBufferString(updateJSON))
	authReq(req4)
	rr4 := httptest.NewRecorder()
	srv.ServeHTTP(rr4, req4)
	if rr4.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr4.Code)
	}

	// 5. Test DELETE /api/presets/{id}
	req5 := httptest.NewRequest(http.MethodDelete, "/api/presets/"+idStr, nil)
	authReq(req5)
	rr5 := httptest.NewRecorder()
	srv.ServeHTTP(rr5, req5)
	if rr5.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr5.Code)
	}
}

func TestShellStaticHTML(t *testing.T) {
	html := string(embeddedHTML)
	requiredStrings := []string{
		"btn-icon",
		`data-i18n-title="transfers_title"`,
		`data-i18n-title="global_notes_title"`,
		`data-i18n-title="settings_title"`,
		`data-i18n-title="logout_title"`,
		"logout_confirm",
		"footerEngineDot",
		"footerHostMetrics",
		"footer-pill",
		"v1.0.0",
		"btn-inspect-quick",
		"injectKeyModal",
		"confirmModal",
		"ruleModal",
		"snapshotExportModal",
		"toastContainer",
		"settingsTabs",
		"tabSettingsGeneral",
		"tabSettingsNotif",
		"tabSettingsBackup",
		"tabSettingsPresets",
		"toggleDropdown",
		"showConfirm",
		"showToast",
		"openModal",
		"closeModal",
	}
	for _, s := range requiredStrings {
		if !strings.Contains(html, s) {
			t.Errorf("expected embedded index.html to contain %q", s)
		}
	}

	forbiddenPatterns := []string{
		"alert(",
		"prompt(",
		"confirm(",
	}
	for _, p := range forbiddenPatterns {
		if strings.Contains(html, p) {
			t.Errorf("embedded index.html should not contain native dialog call %q", p)
		}
	}

	// Validate JS syntax using node if available in PATH
	if nodePath, err := exec.LookPath("node"); err == nil && nodePath != "" {
		start := strings.Index(html, "<script>")
		end := strings.LastIndex(html, "</script>")
		if start != -1 && end != -1 && end > start {
			jsCode := html[start+len("<script>") : end]
			cmd := exec.Command(nodePath, "--check")
			cmd.Stdin = strings.NewReader(jsCode)
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Errorf("JavaScript syntax check failed: %v\nOutput: %s", err, string(out))
			}
		}
	}
}

func TestServerSetVersion(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "pantau-ver-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	db, err := store.Open(filepath.Join(tmpDir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	srv := NewServer(db, nil, nil)
	srv.SetVersion("v0.5.2-alpha")

	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	body := w.Body.String()
	if !strings.Contains(body, `<span class="footer-badge">v0.5.2-alpha</span>`) {
		t.Errorf("expected custom version in footer badge, got body without it")
	}
	if strings.Contains(body, `<span class="footer-badge">v1.0.0</span>`) {
		t.Errorf("expected default v1.0.0 to be replaced in footer badge")
	}
}

func TestAirgappedSelfContainedAssets(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "pantau-airgap-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	db, err := store.Open(filepath.Join(tmpDir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	srv := NewServer(db, nil, nil)

	// 1. Check Root / Response: CSP, Cache-Control, and Zero CDN References
	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 for /, got %d", w.Code)
	}

	csp := w.Header().Get("Content-Security-Policy")
	if !strings.Contains(csp, "default-src 'self'") {
		t.Errorf("expected default-src 'self' in CSP, got: %s", csp)
	}
	if !strings.Contains(csp, "connect-src 'self' ws: wss:") {
		t.Errorf("expected connect-src with ws/wss in CSP, got: %s", csp)
	}

	cacheControl := w.Header().Get("Cache-Control")
	if cacheControl != "no-cache" {
		t.Errorf("expected Cache-Control: no-cache on HTML root, got: %s", cacheControl)
	}

	htmlBody := w.Body.String()
	if strings.Contains(htmlBody, "cdn.jsdelivr.net") {
		t.Errorf("found online CDN jsdelivr reference in index.html")
	}
	if strings.Contains(htmlBody, "cdnjs.cloudflare.com") {
		t.Errorf("found online CDN cdnjs reference in index.html")
	}
	if !strings.Contains(htmlBody, `href="/vendor/xterm.css"`) {
		t.Errorf("expected local /vendor/xterm.css link in index.html")
	}
	if !strings.Contains(htmlBody, `src="/vendor/xterm.js"`) {
		t.Errorf("expected local /vendor/xterm.js script in index.html")
	}

	// 2. Check Static Vendor Serving
	testVendorAssets := []struct {
		path        string
		contentType string
	}{
		{"/vendor/xterm.js", "text/javascript"},
		{"/vendor/xterm.css", "text/css"},
		{"/vendor/codemirror.min.js", "text/javascript"},
		{"/vendor/codemirror.min.css", "text/css"},
		{"/vendor/nord.min.css", "text/css"},
		{"/vendor/xterm-addon-fit.js", "text/javascript"},
		{"/vendor/mode/yaml.min.js", "text/javascript"},
		{"/vendor/mode/shell.min.js", "text/javascript"},
	}

	for _, tc := range testVendorAssets {
		reqV := httptest.NewRequest("GET", tc.path, nil)
		wV := httptest.NewRecorder()
		srv.ServeHTTP(wV, reqV)

		if wV.Code != http.StatusOK {
			t.Errorf("expected 200 for %s, got %d", tc.path, wV.Code)
		}
		cc := wV.Header().Get("Cache-Control")
		if !strings.Contains(cc, "immutable") {
			t.Errorf("expected immutable Cache-Control for %s, got %s", tc.path, cc)
		}
		ct := wV.Header().Get("Content-Type")
		if !strings.Contains(ct, tc.contentType) && !strings.Contains(ct, "application/javascript") {
			t.Errorf("expected %s content-type for %s, got %s", tc.contentType, tc.path, ct)
		}
		if wV.Body.Len() == 0 {
			t.Errorf("expected non-empty body for %s", tc.path)
		}
	}
}

func TestGlobalTerminalDockWebAssets(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	srv := NewServer(nil, nil, nil)
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 for index, got %d", w.Code)
	}

	body := w.Body.String()
	requiredSnippets := []string{
		"id=\"terminalDockModal\"",
		"id=\"terminalMinimizedPill\"",
		"id=\"terminalTabsList\"",
		"id=\"newTabDropdownMenu\"",
		"openTerminalForHost",
		"startRenameTab",
		"minimizeTerminalDock",
		"restoreTerminalDock",
		"toggleTerminalDockMaximize",
		"closeTerminalTab",
		"global_terminal_dock",
		"close_all_terminals_confirm",
	}

	for _, s := range requiredSnippets {
		if !strings.Contains(body, s) {
			t.Errorf("expected index.html to contain %q", s)
		}
	}

	// Regression checks for terminal dock bug fixes:
	// 1. Confirm modal must have higher z-index than terminal dock modal
	if !strings.Contains(body, "#confirmModal") || !strings.Contains(body, "100001") {
		t.Errorf("expected confirmModal to have z-index 100001 to prevent being hidden beneath terminal dock")
	}

	// 2. New tab dropdown wrapper must not be trapped inside overflow-x tabs container
	if strings.Contains(body, `<div class="terminal-tabs-container">`+"\n"+`        <div class="terminal-tabs-list" id="terminalTabsList"></div>`+"\n"+`        <div id="newTabDropdownMenu"`) {
		t.Errorf("newTabDropdownMenu should be placed outside .terminal-tabs-container to prevent overflow clipping")
	}

	// 3. openTerminalForHost must auto-reconnect if inactive/disconnected
	if !strings.Contains(body, "reconnectTabTerminal(existing.id)") {
		t.Errorf("expected openTerminalForHost to reconnect existing disconnected tab")
	}

	// 4. renderTerminalTabsList must not shadow t() translation function with arrow parameter
	if strings.Contains(body, "terminalTabs.map(t =>") {
		t.Errorf("terminalTabs.map parameter must not be 't' to avoid shadowing global t() translation function")
	}

	// 5. terminal-dropdown-menu must align right: 0 to prevent overflowing right boundary
	if !strings.Contains(body, ".terminal-dropdown-menu") || !strings.Contains(body, "right: 0;") {
		t.Errorf("expected terminal-dropdown-menu to have right: 0 to prevent clipping against dock edge")
	}

	// 6. terminal-dock-tab must sit flush on header bottom border
	if !strings.Contains(body, "margin-bottom: -1px;") || !strings.Contains(body, "align-items: flex-end;") {
		t.Errorf("expected terminal dock tabs to sit flush on header bottom border")
	}

	// 7. Active tab must mask bottom line and hide scrollbar on tabs container
	if !strings.Contains(body, "active-tab::after") || !strings.Contains(body, "scrollbar-width: none;") {
		t.Errorf("expected active tab mask and hidden scrollbar on tabs container")
	}

	// 8. Minimized pill must be placed above footer
	if !strings.Contains(body, "bottom: 48px;") {
		t.Errorf("expected minimized pill to have bottom: 48px to clear footer")
	}
}

func TestHandleDocs(t *testing.T) {
	mockDocs := fstest.MapFS{
		"README.md":             &fstest.MapFile{Data: []byte("# Pantau\n\nEnglish Overview")},
		"README.id.md":          &fstest.MapFile{Data: []byte("# Pantau\n\nRingkasan Bahasa Indonesia")},
		"docs/user-guide.md":    &fstest.MapFile{Data: []byte("# User Guide\n\nEnglish Guide")},
		"docs/user-guide.id.md": &fstest.MapFile{Data: []byte("# Panduan Pengguna\n\nPanduan Bahasa Indonesia")},
	}

	srv := NewServer(nil, nil, nil)
	srv.SetDocsFS(mockDocs)

	// 1. Default GET /api/docs -> README.md (public, unauthenticated)
	req := httptest.NewRequest(http.MethodGet, "/api/docs", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for default /api/docs, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "English Overview") {
		t.Errorf("expected default docs to contain English Overview, got %q", rec.Body.String())
	}
	if rec.Header().Get("Content-Type") != "text/plain; charset=utf-8" {
		t.Errorf("expected text/plain content type, got %s", rec.Header().Get("Content-Type"))
	}

	// 2. GET /api/docs?name=readme&lang=id -> README.id.md
	req = httptest.NewRequest(http.MethodGet, "/api/docs?name=readme&lang=id", nil)
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for readme id, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "Ringkasan Bahasa Indonesia") {
		t.Errorf("expected Indonesian readme, got %q", rec.Body.String())
	}

	// 3. GET /api/docs?name=guide&lang=en -> docs/user-guide.md
	req = httptest.NewRequest(http.MethodGet, "/api/docs?name=guide&lang=en", nil)
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for guide en, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "English Guide") {
		t.Errorf("expected English guide, got %q", rec.Body.String())
	}

	// 4. GET /api/docs?name=guide&lang=id -> docs/user-guide.id.md
	req = httptest.NewRequest(http.MethodGet, "/api/docs?name=guide&lang=id", nil)
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for guide id, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "Panduan Bahasa Indonesia") {
		t.Errorf("expected Indonesian guide, got %q", rec.Body.String())
	}

	// 5. Invalid name -> 400 Bad Request
	req = httptest.NewRequest(http.MethodGet, "/api/docs?name=unknown", nil)
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for unknown doc name, got %d", rec.Code)
	}

	// 6. Non-GET method -> 405 Method Not Allowed
	req = httptest.NewRequest(http.MethodPost, "/api/docs", nil)
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405 for POST /api/docs, got %d", rec.Code)
	}
}

func TestDocumentationModalUI(t *testing.T) {
	html := string(embeddedHTML)

	// Check Workspace Modal container and elements
	mustContain := []string{
		`id="documentationModal"`,
		`class="modal-content modal-workspace"`,
		`id="docsSidebar"`,
		`id="docsTocList"`,
		`id="docsContentPane"`,
		`id="docsContentBody"`,
		`id="docsLangToggleBtn"`,
		`openDocumentationModal()`,
		`parseMicroMarkdown(`,
		`docs_title`,
		`docs_link`,
		`doc_readme`,
		`doc_guide`,
		`doc_toc`,
	}
	for _, s := range mustContain {
		if !strings.Contains(html, s) {
			t.Errorf("expected embedded index.html to contain %q", s)
		}
	}
}

func TestTerminalFontConfigurationUI(t *testing.T) {
	html := string(embeddedHTML)

	mustContain := []string{
		`class="term-font-size-val"`,
		`adjustTerminalFontSize(`,
		`TERMINAL_FONT_FAMILY`,
		`JetBrains Mono`,
		`Fira Code`,
		`Cascadia Code`,
		`getTerminalFontSize()`,
		`applyTerminalFontSize()`,
		`fontFamily: TERMINAL_FONT_FAMILY`,
		`focusActiveTerminal()`,
		`term.focus()`,
	}
	for _, s := range mustContain {
		if !strings.Contains(html, s) {
			t.Errorf("expected embedded index.html to contain %q", s)
		}
	}
}

