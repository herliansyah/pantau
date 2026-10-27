package web

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
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

	"pantau/internal/inspector"
	"pantau/internal/sshrunner"
	"pantau/internal/store"
	"pantau/internal/updater"

	"github.com/gorilla/websocket"
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
		"header-divider",
		"header-cluster",
		"btnInspectAll",
		"inspect_all_confirm_msg",
		"inspect_all_confirm_title",
		".toast-warning",
		".badge-warning",
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
	count := strings.Count(body, `<span class="footer-badge">v0.5.2-alpha</span>`)
	if count < 2 {
		t.Errorf("expected custom version in footer badge and login view (at least 2 occurrences), got %d", count)
	}
	if !strings.Contains(body, `id="loginAppVersion"`) {
		t.Errorf("expected loginAppVersion badge to be present in body")
	}
	if strings.Contains(body, `<span class="footer-badge">v1.0.0</span>`) {
		t.Errorf("expected default v1.0.0 to be replaced in all badges")
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
		"id=\"btnHeaderTerminal\"",
		"id=\"activeTerminalBadge\"",
		"id=\"inspectBtnModal\"",
		"id=\"terminalTabsList\"",
		"id=\"newTabDropdownMenu\"",
		"openTerminalForHost",
		"startRenameTab",
		"toggleTerminalDock",
		"minimizeTerminalDock",
		"restoreTerminalDock",
		"toggleTerminalDockMaximize",
		"closeTerminalTab",
		"updateTerminalSessionIndicators",
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

	// 8. Terminal tab removed from host detail tabs and floating pill eliminated
	if strings.Contains(body, "id=\"terminalMinimizedPill\"") {
		t.Errorf("expected floating terminalMinimizedPill to be removed in favor of header launcher")
	}
	if strings.Contains(body, `data-tab="terminal"`) {
		t.Errorf("expected redundant terminal tab to be removed from host detail modal tabs")
	}
	if strings.Contains(body, "id=\"btnDetailTerminal\"") {
		t.Errorf("expected btnDetailTerminal to be removed from modal header")
	}
	if !strings.Contains(body, "id=\"inspectBtnModal\"") {
		t.Errorf("expected inspectBtnModal to be present in modal header")
	}
	if !strings.Contains(body, "btn-host-terminal") || !strings.Contains(body, "host-terminal-badge") {
		t.Errorf("expected host cards to contain btn-host-terminal and host-terminal-badge")
	}

	// 9. Terminal Tab Host Filter checks in new tab dropdown
	if !strings.Contains(body, "id=\"terminalHostSearchInput\"") {
		t.Errorf("expected terminalHostSearchInput to be present in new tab dropdown")
	}
	if !strings.Contains(body, "id=\"terminalDropdownHostList\"") {
		t.Errorf("expected terminalDropdownHostList to be present in new tab dropdown")
	}
	if !strings.Contains(body, "data-i18n-placeholder=\"search_terminal_host_ph\"") {
		t.Errorf("expected search_terminal_host_ph placeholder attribute on search input")
	}
	if !strings.Contains(body, `"search_terminal_host_ph": "Search host or IP..."`) {
		t.Errorf("expected search_terminal_host_ph in English dictionary")
	}
	if !strings.Contains(body, `"search_terminal_host_ph": "Cari host atau IP..."`) {
		t.Errorf("expected search_terminal_host_ph in Indonesian dictionary")
	}
	if !strings.Contains(body, "filterTerminalDropdownHosts") || !strings.Contains(body, "onTerminalHostSearchKeyDown") {
		t.Errorf("expected filterTerminalDropdownHosts and onTerminalHostSearchKeyDown functions to be defined")
	}
	if !strings.Contains(body, "name.includes(q) || host.includes(q)") {
		t.Errorf("expected filter to match both host name and host IP/address")
	}
}

func TestHandleDocs(t *testing.T) {
	mockDocs := fstest.MapFS{
		"README.md":             &fstest.MapFile{Data: []byte("# Pantau\n\nEnglish Overview")},
		"README.id.md":          &fstest.MapFile{Data: []byte("# Pantau\n\nRingkasan Bahasa Indonesia")},
		"docs/user-guide.md":    &fstest.MapFile{Data: []byte("# User Guide\n\nEnglish Guide")},
		"docs/user-guide.id.md": &fstest.MapFile{Data: []byte("# Panduan Pengguna\n\nPanduan Bahasa Indonesia")},
		"CHANGELOG.md":          &fstest.MapFile{Data: []byte("# Changelog\n\nAll notable changes")},
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

	// 5. GET /api/docs?name=changelog -> CHANGELOG.md (language agnostic fallback)
	req = httptest.NewRequest(http.MethodGet, "/api/docs?name=changelog&lang=id", nil)
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for changelog, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "All notable changes") {
		t.Errorf("expected Changelog content, got %q", rec.Body.String())
	}

	// 6. Invalid name -> 400 Bad Request
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

func TestIndexGzipAndETagRevalidation(t *testing.T) {
	srv := NewServer(nil, nil, nil)

	// 1. Raw request without Accept-Encoding: gzip
	reqRaw := httptest.NewRequest("GET", "/", nil)
	wRaw := httptest.NewRecorder()
	srv.ServeHTTP(wRaw, reqRaw)

	if wRaw.Code != http.StatusOK {
		t.Fatalf("expected 200 for raw /, got %d", wRaw.Code)
	}
	etag := wRaw.Header().Get("ETag")
	if etag == "" {
		t.Fatalf("expected non-empty ETag header on /")
	}
	if wRaw.Header().Get("Content-Encoding") != "" {
		t.Errorf("expected no Content-Encoding without Accept-Encoding header")
	}
	if wRaw.Body.Len() < 200000 {
		t.Errorf("expected raw uncompressed body > 200KB, got %d bytes", wRaw.Body.Len())
	}

	// 2. Request with Accept-Encoding: gzip
	reqGz := httptest.NewRequest("GET", "/", nil)
	reqGz.Header.Set("Accept-Encoding", "gzip, deflate, br")
	wGz := httptest.NewRecorder()
	srv.ServeHTTP(wGz, reqGz)

	if wGz.Code != http.StatusOK {
		t.Fatalf("expected 200 for gzipped /, got %d", wGz.Code)
	}
	if wGz.Header().Get("Content-Encoding") != "gzip" {
		t.Fatalf("expected Content-Encoding: gzip, got %q", wGz.Header().Get("Content-Encoding"))
	}
	if wGz.Header().Get("ETag") != etag {
		t.Errorf("expected matching ETag %s, got %s", etag, wGz.Header().Get("ETag"))
	}
	// Verify compression ratio: ~150KB vs ~690KB (<160KB)
	if wGz.Body.Len() > 160000 {
		t.Errorf("expected gzipped body < 160KB, got %d bytes", wGz.Body.Len())
	}

	// Decompress and compare with raw
	gr, err := gzip.NewReader(wGz.Body)
	if err != nil {
		t.Fatalf("failed to create gzip reader: %v", err)
	}
	decompressed, err := io.ReadAll(gr)
	_ = gr.Close()
	if err != nil {
		t.Fatalf("failed to read decompressed body: %v", err)
	}
	if !bytes.Equal(decompressed, wRaw.Body.Bytes()) {
		t.Errorf("decompressed gzip payload does not match raw uncompressed body")
	}

	// 3. ETag 304 Not Modified revalidation
	reqETag := httptest.NewRequest("GET", "/", nil)
	reqETag.Header.Set("If-None-Match", etag)
	wETag := httptest.NewRecorder()
	srv.ServeHTTP(wETag, reqETag)

	if wETag.Code != http.StatusNotModified {
		t.Fatalf("expected 304 Not Modified, got %d", wETag.Code)
	}
	if wETag.Body.Len() != 0 {
		t.Errorf("expected empty body on 304 Not Modified, got %d bytes", wETag.Body.Len())
	}

	// 4. SetVersion updates ETag and content
	srv.SetVersion("v2.5.0")
	reqVer := httptest.NewRequest("GET", "/", nil)
	wVer := httptest.NewRecorder()
	srv.ServeHTTP(wVer, reqVer)

	newETag := wVer.Header().Get("ETag")
	if newETag == etag {
		t.Errorf("expected ETag to change after SetVersion, got %s", newETag)
	}
	if !strings.Contains(wVer.Body.String(), "v2.5.0") {
		t.Errorf("expected updated version in HTML body")
	}
}

func TestFavicon(t *testing.T) {
	srv := NewServer(nil, nil, nil)
	req := httptest.NewRequest("GET", "/favicon.ico", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200 for favicon, got %d", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); ct != "image/x-icon" {
		t.Errorf("expected Content-Type image/x-icon, got %s", ct)
	}
	if w.Body.Len() == 0 {
		t.Errorf("expected non-empty favicon body")
	}
}

func TestUpdateEndpointsAndUI(t *testing.T) {
	srv := NewServer(nil, nil, nil)

	// 1. Verify UI elements exist in rendered HTML
	reqUI := httptest.NewRequest("GET", "/", nil)
	wUI := httptest.NewRecorder()
	srv.ServeHTTP(wUI, reqUI)
	body := wUI.Body.String()

	if !strings.Contains(body, `id="footerUpdateBadge"`) {
		t.Errorf("expected footerUpdateBadge in HTML")
	}
	if !strings.Contains(body, `id="tabSettingsUpdates"`) {
		t.Errorf("expected tabSettingsUpdates in HTML")
	}
	if !strings.Contains(body, `id="settingsPanelUpdates"`) {
		t.Errorf("expected settingsPanelUpdates in HTML")
	}

	// 2. Unauthenticated check returns 401
	reqUnauth := httptest.NewRequest("GET", "/api/update/check", nil)
	wUnauth := httptest.NewRecorder()
	srv.ServeHTTP(wUnauth, reqUnauth)
	if wUnauth.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for unauthenticated update check, got %d", wUnauth.Code)
	}

	// 3. Authenticated check with updater manager
	token := "update_test_token"
	srv.sessions.Store(token, time.Now().Add(time.Hour))
	authCookie := &http.Cookie{Name: "pantau_session", Value: token}

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"tag_name":     "v2.0.0",
			"body":         "Test changelog",
			"html_url":     "http://example.com/release/v2.0.0",
			"published_at": "2026-09-17T00:00:00Z",
			"assets":       []interface{}{},
		})
	}))
	defer ts.Close()

	upd := updater.NewManager("v1.0.0", false)
	upd.SetAPIURL(ts.URL)
	srv.SetUpdaterManager(upd)

	reqAuth := httptest.NewRequest("GET", "/api/update/check?force=true", nil)
	reqAuth.AddCookie(authCookie)
	wAuth := httptest.NewRecorder()
	srv.ServeHTTP(wAuth, reqAuth)

	if wAuth.Code != http.StatusOK {
		t.Fatalf("expected 200 for authenticated update check, got %d", wAuth.Code)
	}

	var res updater.CheckResult
	if err := json.NewDecoder(wAuth.Body).Decode(&res); err != nil {
		t.Fatalf("failed to decode check result: %v", err)
	}
	if !res.Available {
		t.Errorf("expected update to be available, got false")
	}
	if res.LatestVersion != "v2.0.0" {
		t.Errorf("expected latest version v2.0.0, got %s", res.LatestVersion)
	}

	// 4. Method not allowed on POST routes with GET
	reqMethod := httptest.NewRequest("GET", "/api/update/apply", nil)
	reqMethod.AddCookie(authCookie)
	wMethod := httptest.NewRecorder()
	srv.ServeHTTP(wMethod, reqMethod)
	if wMethod.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405 for GET on /api/update/apply, got %d", wMethod.Code)
	}
}

func TestSettingsModalLayout(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "pantau-settings-test-*")
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
	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	body := w.Body.String()
	requiredSnippets := []string{
		`modal-dialog-lg`,
		`id="tabSettingsGeneral"`,
		`id="tabSettingsSecurity"`,
		`id="tabSettingsNotif"`,
		`id="tabSettingsBackup"`,
		`id="tabSettingsPresets"`,
		`id="tabSettingsUpdates"`,
		`id="settingsPanelGeneral"`,
		`id="settingsPanelSecurity"`,
		`repeat(6, 1fr)`,
	}
	for _, snippet := range requiredSnippets {
		if !strings.Contains(body, snippet) {
			t.Errorf("expected HTML body to contain %q", snippet)
		}
	}
}

func TestPresetModalAndModalStacking(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "pantau-preset-modal-test-*")
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
	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	body := w.Body.String()

	// 1. Regression test: scope option must not have double emoji
	if strings.Contains(body, "`🎯 ${t('scope_host')}") || strings.Contains(body, "🎯 ${t('scope_host')}") {
		t.Errorf("found double emoji pattern in preset scope option")
	}

	// 2. Regression test: openCreatePresetModal must resolve target host from context
	if !strings.Contains(body, "currentPresetTargetHost") || !strings.Contains(body, "openCreatePresetModal(editPreset = null, explicitHostId = null)") {
		t.Errorf("expected openCreatePresetModal to support explicitHostId and context host resolution")
	}

	// 3. Regression test: dynamic modal stacking via openModal
	requiredModalSnippets := []string{
		`modalZIndexCounter += 10`,
		`openModal('presetModal')`,
		`openModal('presetManagerModal')`,
		`openModal('settingsModal')`,
		`openModal('ruleModal')`,
		`openModal('hostNoteModal')`,
		`openModal('snapshotExportModal')`,
	}
	for _, snippet := range requiredModalSnippets {
		if !strings.Contains(body, snippet) {
			t.Errorf("expected HTML body to contain modal stacking snippet %q", snippet)
		}
	}

	// 4. Regression test: presetManagerModal layout and table column styling
	if !strings.Contains(body, `<div id="presetManagerModal" class="modal">`+"\n"+`  <div class="modal-content modal-dialog-lg">`) {
		t.Errorf("expected presetManagerModal to use modal-dialog-lg")
	}
	if !strings.Contains(body, `<th style="padding: 6px; width: 100%; min-width: 260px;">Command</th>`) {
		t.Errorf("expected command column to have width 100%% and min-width")
	}
}

func TestInspectHost_CooldownAndInFlight(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "pantau-web-ins-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	db, err := store.Open(filepath.Join(tmpDir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	mockRunner := sshrunner.NewMockRunner()
	mockRunner.DefaultExec = func(cmd string) (string, string, int, error) {
		return "", "", 0, nil
	}
	factory := func(host string, port int, user, key string) (sshrunner.Runner, error) {
		return mockRunner, nil
	}
	ins := inspector.New(db, factory, nil)
	srv := NewServer(db, ins, nil)
	token := "test_inspect_token"
	srv.sessions.Store(token, time.Now().Add(time.Hour))
	authReq := func(req *http.Request) {
		req.AddCookie(&http.Cookie{
			Name:  "pantau_session",
			Value: token,
		})
	}

	hostID, err := db.CreateHost(&store.Host{
		Name: "Test Node",
		Host: "192.168.1.100",
		Port: 22,
		User: "root",
	})
	if err != nil {
		t.Fatal(err)
	}

	// 1. First inspect: succeeds
	req1 := httptest.NewRequest("POST", "/api/hosts/"+strconv.FormatInt(hostID, 10)+"/inspect", nil)
	authReq(req1)
	w1 := httptest.NewRecorder()
	srv.ServeHTTP(w1, req1)
	if w1.Code != http.StatusOK {
		t.Fatalf("first inspect expected 200, got %d: %s", w1.Code, w1.Body.String())
	}

	// 2. Immediate second inspect: must trigger 429 Too Many Requests (cooldown guard)
	req2 := httptest.NewRequest("POST", "/api/hosts/"+strconv.FormatInt(hostID, 10)+"/inspect", nil)
	authReq(req2)
	w2 := httptest.NewRecorder()
	srv.ServeHTTP(w2, req2)
	if w2.Code != http.StatusTooManyRequests {
		t.Fatalf("second inspect expected 429, got %d: %s", w2.Code, w2.Body.String())
	}
	var res2 map[string]interface{}
	if err := json.NewDecoder(w2.Body).Decode(&res2); err != nil {
		t.Fatal(err)
	}
	if res2["cooldown"] != true {
		t.Fatalf("expected cooldown: true, got %v", res2)
	}

	// 3. In-flight inspect: set LastInspected to 1 hour ago so cooldown passes, but mark host as in-flight
	past := time.Now().Add(-1 * time.Hour)
	h, _ := db.GetHost(hostID)
	h.LastInspected = &past
	_ = db.UpdateHostInspection(h)

	// Artificially trigger in-flight state in Inspector using exported method or by calling with blocking runner
	blockCh := make(chan struct{})
	mockRunner.DefaultExec = func(cmd string) (string, string, int, error) {
		<-blockCh
		return "", "", 0, nil
	}
	go func() {
		_ = ins.InspectHost(hostID)
	}()
	// Wait a tiny bit for goroutine to acquire inFlight lock
	time.Sleep(20 * time.Millisecond)

	req3 := httptest.NewRequest("POST", "/api/hosts/"+strconv.FormatInt(hostID, 10)+"/inspect", nil)
	authReq(req3)
	w3 := httptest.NewRecorder()
	srv.ServeHTTP(w3, req3)

	close(blockCh) // release the blocked inspection

	if w3.Code != http.StatusConflict {
		t.Fatalf("in-flight inspect expected 409 Conflict, got %d: %s", w3.Code, w3.Body.String())
	}
	var res3 map[string]interface{}
	if err := json.NewDecoder(w3.Body).Decode(&res3); err != nil {
		t.Fatal(err)
	}
	if res3["in_flight"] != true {
		t.Fatalf("expected in_flight: true, got %v", res3)
	}
}

func TestInspectAll_CooldownAndInFlight(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "pantau-web-ins-all-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	db, err := store.Open(filepath.Join(tmpDir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	mockRunner := sshrunner.NewMockRunner()
	blockCh := make(chan struct{})
	mockRunner.DefaultExec = func(cmd string) (string, string, int, error) {
		<-blockCh
		return "", "", 0, nil
	}
	factory := func(host string, port int, user, key string) (sshrunner.Runner, error) {
		return mockRunner, nil
	}
	ins := inspector.New(db, factory, nil)
	srv := NewServer(db, ins, nil)
	token := "test_inspect_all_token"
	srv.sessions.Store(token, time.Now().Add(time.Hour))
	authReq := func(req *http.Request) {
		req.AddCookie(&http.Cookie{
			Name:  "pantau_session",
			Value: token,
		})
	}

	_, err = db.CreateHost(&store.Host{
		Name: "Host 1",
		Host: "192.168.1.101",
		Port: 22,
		User: "root",
	})
	if err != nil {
		t.Fatal(err)
	}

	// 1. First inspect-all: starts in background, runner blocks on blockCh
	req1 := httptest.NewRequest("POST", "/api/hosts/inspect-all", nil)
	authReq(req1)
	w1 := httptest.NewRecorder()
	srv.ServeHTTP(w1, req1)
	if w1.Code != http.StatusOK {
		t.Fatalf("first inspect-all expected 200, got %d: %s", w1.Code, w1.Body.String())
	}

	// Wait a moment for worker to acquire host in-flight lock
	time.Sleep(20 * time.Millisecond)

	// 2. Second inspect-all while first is in-flight: must be rejected with 409 Conflict
	req2 := httptest.NewRequest("POST", "/api/hosts/inspect-all", nil)
	authReq(req2)
	w2 := httptest.NewRecorder()
	srv.ServeHTTP(w2, req2)
	if w2.Code != http.StatusConflict {
		close(blockCh)
		t.Fatalf("concurrent inspect-all expected 409 Conflict, got %d: %s", w2.Code, w2.Body.String())
	}

	// Unblock first inspection and wait for it to complete
	close(blockCh)
	for i := 0; i < 100; i++ {
		sRes := httptest.NewRecorder()
		sReq := httptest.NewRequest("GET", "/api/hosts/inspect-all", nil)
		authReq(sReq)
		srv.ServeHTTP(sRes, sReq)
		var st map[string]interface{}
		_ = json.NewDecoder(sRes.Body).Decode(&st)
		if st["in_flight"] == false {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	// 3. Third inspect-all immediately after first finishes: must trigger 429 Too Many Requests (cooldown guard)
	req3 := httptest.NewRequest("POST", "/api/hosts/inspect-all", nil)
	authReq(req3)
	w3 := httptest.NewRecorder()
	srv.ServeHTTP(w3, req3)
	if w3.Code != http.StatusTooManyRequests {
		t.Fatalf("inspect-all within cooldown expected 429, got %d: %s", w3.Code, w3.Body.String())
	}
}

func TestFileManagerFilterAndSortUI(t *testing.T) {
	html := string(embeddedHTML)

	requiredUIElements := []string{
		"file-filter-toolbar",
		"file-search-box",
		"fileSearchInput",
		"fileSearchClear",
		"file-pill-group",
		"file-pill-btn",
		"fileCountLabel",
		"sortable-th",
		"toggleFileSort('name')",
		"toggleFileSort('size')",
		"toggleFileSort('mod_time')",
		"setFileTypeFilter('all')",
		"setFileTypeFilter('dir')",
		"setFileTypeFilter('file')",
		"clearFileFilter()",
		"onFileFilterInput",
		"renderFilesTableBody",
	}
	for _, el := range requiredUIElements {
		if !strings.Contains(html, el) {
			t.Errorf("expected embedded index.html to contain file manager UI element %q", el)
		}
	}

	requiredI18nKeys := []string{
		`"filter_placeholder":`,
		`"filter_all":`,
		`"filter_folders":`,
		`"filter_files":`,
		`"showing_entries":`,
		`"no_matching_files":`,
	}
	for _, key := range requiredI18nKeys {
		if strings.Count(html, key) < 2 {
			t.Errorf("expected i18n key %q to exist in at least 2 dictionaries (EN and ID), found %d", key, strings.Count(html, key))
		}
	}
}

func TestAlertAckAllAndRunsFilter(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "pantau-web-alerts-*")
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
	token := "test_alert_token"
	srv.sessions.Store(token, time.Now().Add(time.Hour))
	authReq := func(req *http.Request) {
		req.AddCookie(&http.Cookie{
			Name:  "pantau_session",
			Value: token,
		})
	}

	hID, _ := db.CreateHost(&store.Host{Name: "Server X", Host: "10.0.0.1", Port: 22, User: "root"})

	// 1. Create alerts & test /api/alerts/ack-all
	_, _ = db.CreateAlert(&store.Alert{HostID: hID, Level: "critical", Message: "Drift in rule 1"})
	_, _ = db.CreateAlert(&store.Alert{HostID: hID, Level: "warning", Message: "Drift in rule 2"})

	reqAckAll := httptest.NewRequest("POST", "/api/alerts/ack-all", nil)
	authReq(reqAckAll)
	wAckAll := httptest.NewRecorder()
	srv.ServeHTTP(wAckAll, reqAckAll)
	if wAckAll.Code != http.StatusOK {
		t.Fatalf("expected 200 from ack-all, got %d", wAckAll.Code)
	}

	activeAlerts, _ := db.ListActiveAlerts()
	if len(activeAlerts) != 0 {
		t.Fatalf("expected 0 active alerts after ack-all, got %d", len(activeAlerts))
	}

	// 2. Create runs & test /api/hosts/{id}/runs?filter=issues
	_ = db.RecordInspectionRun(&store.InspectionRun{HostID: hID, Status: "ok", Summary: "All ok"})
	_ = db.RecordInspectionRun(&store.InspectionRun{HostID: hID, Status: "drift", Summary: "Drift container", Details: "Exited with 1"})
	_ = db.RecordInspectionRun(&store.InspectionRun{HostID: hID, Status: "error", Summary: "SSH timeout", Details: "Connection timed out"})

	reqIssues := httptest.NewRequest("GET", "/api/hosts/"+strconv.FormatInt(hID, 10)+"/runs?filter=issues", nil)
	authReq(reqIssues)
	wIssues := httptest.NewRecorder()
	srv.ServeHTTP(wIssues, reqIssues)
	if wIssues.Code != http.StatusOK {
		t.Fatalf("expected 200 from runs?filter=issues, got %d", wIssues.Code)
	}

	var issueRuns []store.InspectionRun
	if err := json.Unmarshal(wIssues.Body.Bytes(), &issueRuns); err != nil {
		t.Fatalf("failed unmarshaling issue runs: %v", err)
	}
	if len(issueRuns) != 2 {
		t.Fatalf("expected 2 issue runs, got %d", len(issueRuns))
	}
	for _, r := range issueRuns {
		if r.Status == "ok" {
			t.Fatalf("expected non-ok status, got %s", r.Status)
		}
	}

	// 3. UI checks in embedded HTML
	html := string(embeddedHTML)
	requiredUI := []string{
		"alert-summary-bar",
		"alert-overlay-panel",
		"toggleAlertOverlay",
		"ackAllAlerts",
		"btnRunsFilterAll",
		"btnRunsFilterIssues",
		"switchRunsFilter",
	}
	for _, el := range requiredUI {
		if !strings.Contains(html, el) {
			t.Errorf("expected embedded index.html to contain %q", el)
		}
	}

	requiredI18n := []string{
		`"runs_filter_all":`,
		`"runs_filter_issues":`,
		`"active_alerts_count":`,
		`"dismiss_all":`,
	}
	for _, k := range requiredI18n {
		if strings.Count(html, k) < 2 {
			t.Errorf("expected i18n key %q to exist in both EN and ID dictionaries, found %d", k, strings.Count(html, k))
		}
	}
}

func TestCommissionDate_RoutesAndRecalculate(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "pantau-comm-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	db, err := store.Open(filepath.Join(tmpDir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	ins := inspector.New(db, nil, nil)
	srv := NewServer(db, ins, nil)
	token := "test_comm_token"
	srv.sessions.Store(token, time.Now().Add(time.Hour))
	authReq := func(req *http.Request) {
		req.AddCookie(&http.Cookie{
			Name:  "pantau_session",
			Value: token,
		})
	}

	// 1. Create a physical host with 2017 BIOS and initial penalized score (75)
	past := time.Now().Add(-1 * time.Hour)
	host := &store.Host{
		Name:               "NOS Baremetal",
		Host:               "10.0.0.99",
		Port:               22,
		User:               "root",
		HardwareModel:      "HP ProLiant",
		BIOSDate:           "05/10/2017",
		OSInfo:             "Ubuntu 22.04 LTS",
		CPUCores:           4,
		CPULoad:            "0.1, 0.1",
		RAMUsedBytes:       2000000000,
		RAMTotalBytes:      8000000000,
		DiskUsedBytes:      20000000000,
		DiskTotalBytes:     100000000000,
		LifecycleScore:     75,
		LifecycleNotes:     "Exceeds 8-yr critical lifespan",
		LifecycleBreakdown: `[{"name":"Productive Lifespan","status":"fail","score_deduction":-25,"detail":"Exceeds 8-yr critical lifespan"}]`,
		LastInspected:      &past,
	}
	hostID, err := db.CreateHost(host)
	if err != nil {
		t.Fatal(err)
	}
	host.ID = hostID
	_ = db.UpdateHostInspection(host)

	// 2. Reject future commission date
	futurePayload := `{"commission_date":"2099-01-01"}`
	reqFuture := httptest.NewRequest("POST", "/api/hosts/"+strconv.FormatInt(hostID, 10)+"/commission-date", strings.NewReader(futurePayload))
	authReq(reqFuture)
	wFuture := httptest.NewRecorder()
	srv.ServeHTTP(wFuture, reqFuture)
	if wFuture.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 Bad Request for future date, got %d", wFuture.Code)
	}

	// 3. Reject commission date earlier than motherboard BIOS
	beforeBIOSPayload := `{"commission_date":"2015-05-01"}`
	reqBefore := httptest.NewRequest("POST", "/api/hosts/"+strconv.FormatInt(hostID, 10)+"/commission-date", strings.NewReader(beforeBIOSPayload))
	authReq(reqBefore)
	wBefore := httptest.NewRecorder()
	srv.ServeHTTP(wBefore, reqBefore)
	if wBefore.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 Bad Request for date before BIOS, got %d", wBefore.Code)
	}
	if !strings.Contains(wBefore.Body.String(), "cannot be earlier than motherboard BIOS date") {
		t.Fatalf("expected error message to mention BIOS date constraint, got: %s", wBefore.Body.String())
	}

	// 3b. Virtual machines must NOT be blocked if commission date is earlier than VM emulator BIOS date
	vmHost := &store.Host{
		Name:          "QEMU Guest",
		Host:          "10.0.0.100",
		Port:          22,
		User:          "root",
		HardwareModel: "QEMU Standard PC (i440FX + PIIX, 1996)",
		BIOSDate:      "04/01/2026", // recent emulator release
	}
	vmID, err := db.CreateHost(vmHost)
	if err != nil {
		t.Fatal(err)
	}
	vmHost.ID = vmID
	_ = db.UpdateHostInspection(vmHost)
	vmPayload := `{"commission_date":"2023-01-01"}`
	reqVM := httptest.NewRequest("POST", "/api/hosts/"+strconv.FormatInt(vmID, 10)+"/commission-date", strings.NewReader(vmPayload))
	authReq(reqVM)
	wVM := httptest.NewRecorder()
	srv.ServeHTTP(wVM, reqVM)
	if wVM.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for VM commission date even if earlier than emulator BIOS date, got %d: %s", wVM.Code, wVM.Body.String())
	}

	// 4. Accept valid recent commission date and verify immediate recalculation
	recentDate := time.Now().AddDate(0, -6, 0).Format("2006-01-02")
	validPayload := `{"commission_date":"` + recentDate + `"}`
	reqValid := httptest.NewRequest("POST", "/api/hosts/"+strconv.FormatInt(hostID, 10)+"/commission-date", strings.NewReader(validPayload))
	authReq(reqValid)
	wValid := httptest.NewRecorder()
	srv.ServeHTTP(wValid, reqValid)
	if wValid.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for valid commission date, got %d", wValid.Code)
	}

	// Verify host in DB has updated score and breakdown
	updatedHost, err := db.GetHost(hostID)
	if err != nil {
		t.Fatal(err)
	}
	if updatedHost.CommissionDate != recentDate {
		t.Fatalf("expected commission_date %q, got %q", recentDate, updatedHost.CommissionDate)
	}
	if updatedHost.LifecycleScore != 100 {
		t.Fatalf("expected immediate recalculation to update lifecycle_score to 100, got %d", updatedHost.LifecycleScore)
	}
	if !strings.Contains(updatedHost.LifecycleBreakdown, "Commissioned: "+recentDate) {
		t.Fatalf("expected breakdown to contain Commissioned date, got: %s", updatedHost.LifecycleBreakdown)
	}
	if !strings.Contains(updatedHost.LifecycleBreakdown, "Motherboard BIOS: May 2017") {
		t.Fatalf("expected breakdown to retain Motherboard BIOS audit note, got: %s", updatedHost.LifecycleBreakdown)
	}
	// 4b. Test PUT /api/hosts/{id} validation
	putFuturePayload := `{"name":"NOS Baremetal","host":"10.0.0.99","port":22,"user":"root","commission_date":"2099-01-01"}`
	reqPUTFuture := httptest.NewRequest("PUT", "/api/hosts/"+strconv.FormatInt(hostID, 10), strings.NewReader(putFuturePayload))
	authReq(reqPUTFuture)
	wPUTFuture := httptest.NewRecorder()
	srv.ServeHTTP(wPUTFuture, reqPUTFuture)
	if wPUTFuture.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 Bad Request on PUT host with future date, got %d", wPUTFuture.Code)
	}

	putBeforeBIOSPayload := `{"name":"NOS Baremetal","host":"10.0.0.99","port":22,"user":"root","commission_date":"2015-05-01"}`
	reqPUTBefore := httptest.NewRequest("PUT", "/api/hosts/"+strconv.FormatInt(hostID, 10), strings.NewReader(putBeforeBIOSPayload))
	authReq(reqPUTBefore)
	wPUTBefore := httptest.NewRecorder()
	srv.ServeHTTP(wPUTBefore, reqPUTBefore)
	if wPUTBefore.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 Bad Request on PUT host with date before BIOS, got %d", wPUTBefore.Code)
	}

	// 5. Test UI elements & i18n keys
	html := string(embeddedHTML)
	uiChecks := []string{
		"hostCommissionDate",
		"commissionDateModal",
		"openCommissionDateModal",
		"submitQuickCommissionDate",
		"clearCommissionDate",
	}
	for _, el := range uiChecks {
		if !strings.Contains(html, el) {
			t.Errorf("expected embedded index.html to contain %q", el)
		}
	}

	commI18n := []string{
		`"commission_date_label":`,
		`"commission_date_hint":`,
		`"commission_date_title":`,
		`"edit_commission_date":`,
		`"set_commission_date":`,
	}
	for _, k := range commI18n {
		if strings.Count(html, k) < 2 {
			t.Errorf("expected i18n key %q to exist in both EN and ID dictionaries, found %d", k, strings.Count(html, k))
		}
	}
}

func TestWorkspaceShelfAndSmartResumeUI(t *testing.T) {
	html := string(embeddedHTML)

	requiredUIElements := []string{
		`id="workspaceShelf"`,
		`id="workspaceShelfChips"`,
		`workspace-shelf`,
		`workspace-chip`,
		`btn-modal-minimize`,
		`host-session-badge`,
		`minimizeHostDetail()`,
		`minimizeEditor()`,
		`restoreWorkspaceSession`,
		`renderWorkspaceShelf()`,
		`closeWorkspaceSession`,
		`updateHostSessionBadges()`,
		`closeHostDetailModal()`,
		`closeEditorModalWithConfirm()`,
	}
	for _, el := range requiredUIElements {
		if !strings.Contains(html, el) {
			t.Errorf("expected embedded index.html to contain Workspace Shelf element %q", el)
		}
	}

	requiredI18nKeys := []string{
		`"workspace_shelf_title":`,
		`"btn_minimize":`,
		`"active_session_badge":`,
		`"shelf_limit_reached":`,
		`"discard_unsaved_changes":`,
		`"close_session":`,
	}
	for _, k := range requiredI18nKeys {
		if count := strings.Count(html, k); count < 2 {
			t.Errorf("expected i18n key %q to exist in both EN and ID dictionaries, found %d", k, count)
		}
	}

	// Verify Smart Resume logic exists in openHostDetail
	if !strings.Contains(html, "existingSession") || !strings.Contains(html, "restoreWorkspaceSession(existingSession.id)") {
		t.Errorf("expected openHostDetail to implement Smart Resume restoring existingSession")
	}
}

func TestWorkspaceModalMaximizeAndTerminalFromPath(t *testing.T) {
	html := string(embeddedHTML)

	// 1. Check Workspace Modal Maximize UI elements & CSS
	maximizeElements := []string{
		"workspace-maximized",
		"workspace-maximized-wrapper",
		"btnHostDetailMax",
		"toggleHostDetailMaximize",
		"applyHostDetailMaximizeState",
	}
	for _, el := range maximizeElements {
		if !strings.Contains(html, el) {
			t.Errorf("expected embedded index.html to contain %q", el)
		}
	}

	// 2. Check Per-Host Tab & Path State functions
	tabStateElements := []string{
		"getHostActiveTab",
		"setHostActiveTab",
		"getHostFilesPath",
		"setHostFilesPath",
		"pantau_host_tab_",
		"pantau_host_files_path_",
	}
	for _, el := range tabStateElements {
		if !strings.Contains(html, el) {
			t.Errorf("expected embedded index.html to contain %q", el)
		}
	}

	// 3. Check Terminal From Path & Pins actions
	terminalPathElements := []string{
		"openTerminalForPath",
		"targetTermPath",
		"initialDir",
		"openFileManagerForTab",
		"toggleTerminalPinsDropdown",
		"openTerminalPinModal",
		"triggerPinAction",
		"toggleTerminalPresetsDropdown",
		"filterTabPresets",
		"renderTabPresetsList",
		"presetSearchInput_",
		"presetDropdownList_",
		"filterTerminalPins",
		"pinSearchInput_",
	}
	for _, el := range terminalPathElements {
		if !strings.Contains(html, el) {
			t.Errorf("expected embedded index.html to contain %q", el)
		}
	}

	// 4. Check i18n keys exist in both EN and ID dictionaries
	newI18nKeys := []string{
		`"maximize_workspace":`,
		`"restore_workspace":`,
		`"open_terminal_here":`,
		`"open_in_file_manager":`,
		`"pinned_paths":`,
		`"pin_new_path":`,
		`"search_presets":`,
		`"no_matching_presets":`,
		`"search_pins":`,
		`"no_matching_pins":`,
	}
	for _, k := range newI18nKeys {
		cnt := strings.Count(html, k)
		if cnt < 2 {
			t.Errorf("expected i18n key %q to exist in both EN and ID dictionaries, found %d", k, cnt)
		}
	}

	// 5. Check active terminal path tracking
	activePathElements := []string{
		"getTerminalActivePath",
		"extractPathFromTerminalTitle",
	}
	for _, el := range activePathElements {
		if !strings.Contains(html, el) {
			t.Errorf("expected embedded index.html to contain active path tracking element %q", el)
		}
	}

	// 6. Test handleWSTerminal invalid host_id and invalid host error
	tmpDB := filepath.Join(t.TempDir(), "test.db")
	db, err := store.Open(tmpDB)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	srv := NewServer(db, nil, nil)
	testToken := "ws_test_token"
	srv.sessions.Store(testToken, time.Now().Add(time.Hour))
	authCookie := &http.Cookie{Name: "pantau_session", Value: testToken}

	// Unauthenticated request to /ws/terminal returns 401
	reqUnauth := httptest.NewRequest("GET", "/ws/terminal?host_id=1", nil)
	wUnauth := httptest.NewRecorder()
	srv.mux.ServeHTTP(wUnauth, reqUnauth)
	if wUnauth.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 Unauthorized for unauthenticated /ws/terminal, got %d", wUnauth.Code)
	}

	// Invalid host_id query (authenticated)
	reqBad := httptest.NewRequest("GET", "/ws/terminal?host_id=invalid", nil)
	reqBad.AddCookie(authCookie)
	wBad := httptest.NewRecorder()
	srv.mux.ServeHTTP(wBad, reqBad)
	if wBad.Code != http.StatusBadRequest {
		t.Errorf("expected 400 Bad Request for invalid host_id, got %d", wBad.Code)
	}

	// Valid host_id with dir param but non-existent host (authenticated)
	reqNonExistent := httptest.NewRequest("GET", "/ws/terminal?host_id=999999&dir=/var/log", nil)
	reqNonExistent.AddCookie(authCookie)
	wNonExistent := httptest.NewRecorder()
	srv.mux.ServeHTTP(wNonExistent, reqNonExistent)
	// WebSockets return 400 Bad Request if not a websocket upgrade request
	if wNonExistent.Code != http.StatusBadRequest {
		t.Errorf("expected 400 Bad Request on non-websocket request to /ws/terminal, got %d", wNonExistent.Code)
	}

	// Docker logs unauthenticated returns 401
	reqDockUnauth := httptest.NewRequest("GET", "/ws/docker/logs?host_id=1&container=mycontainer", nil)
	wDockUnauth := httptest.NewRecorder()
	srv.mux.ServeHTTP(wDockUnauth, reqDockUnauth)
	if wDockUnauth.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 Unauthorized for unauthenticated /ws/docker/logs, got %d", wDockUnauth.Code)
	}

	// Docker logs with invalid container name returns 400
	reqDockBad := httptest.NewRequest("GET", "/ws/docker/logs?host_id=1&container=my_container;rm", nil)
	reqDockBad.AddCookie(authCookie)
	wDockBad := httptest.NewRecorder()
	srv.mux.ServeHTTP(wDockBad, reqDockBad)
	if wDockBad.Code != http.StatusBadRequest {
		t.Errorf("expected 400 Bad Request for invalid container name, got %d", wDockBad.Code)
	}
}

func TestFileDuplicationRoutingAndValidation(t *testing.T) {
	tmpDB := filepath.Join(t.TempDir(), "test.db")
	db, err := store.Open(tmpDB)
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

	// 1. Unauthenticated request to /api/hosts/1/files/duplicate should return 401
	reqUnauth := httptest.NewRequest(http.MethodPost, "/api/hosts/1/files/duplicate", strings.NewReader(`{"path":"/var/www/test.txt","new_name":"test_20261001.txt"}`))
	reqUnauth.Header.Set("Content-Type", "application/json")
	wUnauth := httptest.NewRecorder()
	srv.ServeHTTP(wUnauth, reqUnauth)
	if wUnauth.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 Unauthorized, got %d", wUnauth.Code)
	}

	// 2. Non-existent host returns 404
	reqNonExistent := httptest.NewRequest(http.MethodPost, "/api/hosts/9999/files/duplicate", strings.NewReader(`{"path":"/var/www/test.txt","new_name":"test_20261001.txt"}`))
	reqNonExistent.Header.Set("Content-Type", "application/json")
	authReq(reqNonExistent)
	wNonExistent := httptest.NewRecorder()
	srv.ServeHTTP(wNonExistent, reqNonExistent)
	if wNonExistent.Code != http.StatusNotFound {
		t.Errorf("expected 404 Not Found for non-existent host, got %d", wNonExistent.Code)
	}

	// 3. GET on /api/hosts/{id}/files/duplicate (wrong method)
	hID, err := db.CreateHost(&store.Host{
		Name: "Test Host",
		Host: "127.0.0.1",
		User: "root",
		Port: 22,
	})
	if err != nil {
		t.Fatal(err)
	}

	// Since mock SSH runner is nil in srv, ConnectSSH will fail when attempting connection
	// But method not allowed (GET) should fail with method check or connection error
	reqGet := httptest.NewRequest(http.MethodGet, "/api/hosts/"+strconv.FormatInt(hID, 10)+"/files/duplicate", nil)
	authReq(reqGet)
	wGet := httptest.NewRecorder()
	srv.ServeHTTP(wGet, reqGet)
	if wGet.Code != http.StatusMethodNotAllowed && wGet.Code != http.StatusInternalServerError {
		t.Errorf("expected 405 or 500 for GET on duplicate endpoint, got %d", wGet.Code)
	}
}

func TestHostPackagesAPI(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "pantau-web-pkg-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	db, err := store.Open(filepath.Join(tmpDir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	hostID, err := db.CreateHost(&store.Host{Name: "web1", Host: "10.0.0.1", Port: 22, User: "root"})
	if err != nil {
		t.Fatal(err)
	}

	_ = db.ReplaceHostPackages(hostID, []*store.HostPackage{
		{HostID: hostID, Name: "nginx", Version: "1.18", SizeBytes: 500000, IsManual: true, ServiceStatus: "active"},
	})

	srv := NewServer(db, nil, nil)
	token := "valid_test_token"
	srv.sessions.Store(token, time.Now().Add(time.Hour))
	authReq := func(req *http.Request) {
		req.AddCookie(&http.Cookie{
			Name:  "pantau_session",
			Value: token,
		})
	}

	// 1. GET /api/hosts/{id}/packages
	req := httptest.NewRequest(http.MethodGet, "/api/hosts/"+strconv.FormatInt(hostID, 10)+"/packages", nil)
	authReq(req)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d: %s", w.Code, w.Body.String())
	}

	var pkgs []*store.HostPackage
	if err := json.NewDecoder(w.Body).Decode(&pkgs); err != nil {
		t.Fatalf("failed decoding packages json: %v", err)
	}
	if len(pkgs) != 1 || pkgs[0].Name != "nginx" || !pkgs[0].IsManual {
		t.Errorf("unexpected pkgs response: %+v", pkgs)
	}

	// 2. POST /api/hosts/{id}/packages/scan within cooldown should return 429 Too Many Requests
	reqScan := httptest.NewRequest(http.MethodPost, "/api/hosts/"+strconv.FormatInt(hostID, 10)+"/packages/scan", nil)
	authReq(reqScan)
	wScan := httptest.NewRecorder()
	srv.ServeHTTP(wScan, reqScan)

	if wScan.Code != http.StatusTooManyRequests {
		t.Errorf("expected 429 Too Many Requests due to cooldown, got %d: %s", wScan.Code, wScan.Body.String())
	}
}

func TestFilePinsAPI(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "pantau-web-pins-*")
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
	token := "test_token_pins"
	srv.sessions.Store(token, time.Now().Add(time.Hour))
	authReq := func(req *http.Request) {
		req.AddCookie(&http.Cookie{
			Name:  "pantau_session",
			Value: token,
		})
	}

	hostID, err := db.CreateHost(&store.Host{
		Name: "App Server",
		Host: "192.168.1.50",
		Port: 22,
		User: "deploy",
	})
	if err != nil {
		t.Fatal(err)
	}

	// 1. Initial GET /api/hosts/{id}/files/pins -> empty
	req := httptest.NewRequest(http.MethodGet, "/api/hosts/"+strconv.FormatInt(hostID, 10)+"/files/pins", nil)
	authReq(req)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var pins []store.FilePin
	if err := json.NewDecoder(w.Body).Decode(&pins); err != nil {
		t.Fatalf("failed decoding pins: %v", err)
	}
	if len(pins) != 0 {
		t.Fatalf("expected 0 pins, got %d", len(pins))
	}

	// 2. POST pin
	body := `{"path":"/var/log/syslog","is_dir":false,"note":"System log"}`
	req = httptest.NewRequest(http.MethodPost, "/api/hosts/"+strconv.FormatInt(hostID, 10)+"/files/pins", strings.NewReader(body))
	authReq(req)
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 on create pin, got %d: %s", w.Code, w.Body.String())
	}

	// 3. GET pins again
	req = httptest.NewRequest(http.MethodGet, "/api/hosts/"+strconv.FormatInt(hostID, 10)+"/files/pins", nil)
	authReq(req)
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	pins = nil
	if err := json.NewDecoder(w.Body).Decode(&pins); err != nil {
		t.Fatalf("decode pins: %v", err)
	}
	if len(pins) != 1 || pins[0].Path != "/var/log/syslog" || pins[0].Note != "System log" {
		t.Fatalf("unexpected pins: %+v", pins)
	}

	// 4. GET /api/pins (global)
	req = httptest.NewRequest(http.MethodGet, "/api/pins", nil)
	authReq(req)
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 from /api/pins, got %d: %s", w.Code, w.Body.String())
	}
	var globalPins []store.GlobalFilePin
	if err := json.NewDecoder(w.Body).Decode(&globalPins); err != nil {
		t.Fatalf("decode global pins: %v", err)
	}
	if len(globalPins) != 1 || globalPins[0].HostName != "App Server" {
		t.Fatalf("unexpected global pins: %+v", globalPins)
	}

	// 5. DELETE pin
	req = httptest.NewRequest(http.MethodDelete, "/api/hosts/"+strconv.FormatInt(hostID, 10)+"/files/pins?path=/var/log/syslog", nil)
	authReq(req)
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 on delete pin, got %d: %s", w.Code, w.Body.String())
	}

	// 6. Verify empty again
	req = httptest.NewRequest(http.MethodDelete, "/api/hosts/"+strconv.FormatInt(hostID, 10)+"/files/pins?path=/var/log/syslog", nil)
	authReq(req)
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	pins = nil
	_ = json.NewDecoder(w.Body).Decode(&pins)
	if len(pins) != 0 {
		t.Fatalf("expected 0 pins after delete, got %d", len(pins))
	}
}

func TestHostTestConnection_AdHoc(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "pantau-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	db, err := store.Open(filepath.Join(tmpDir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	mockRunner := sshrunner.NewMockRunner()
	mockRunner.DefaultExec = func(cmd string) (string, string, int, error) {
		if strings.Contains(cmd, "uname") {
			return "Linux 6.1.0-custom x86_64\n 12:00:00 up 10 days", "", 0, nil
		}
		return "", "", 0, nil
	}
	factory := func(host string, port int, user, key string) (sshrunner.Runner, error) {
		return mockRunner, nil
	}
	ins := inspector.New(db, factory, nil)
	srv := NewServer(db, ins, nil)
	token := "valid_test_token"
	srv.sessions.Store(token, time.Now().Add(time.Hour))
	authReq := func(req *http.Request) {
		req.AddCookie(&http.Cookie{Name: "pantau_session", Value: token})
	}

	// 1. Missing host address should fail cleanly
	payloadInvalid := map[string]interface{}{"host": "", "port": 22, "user": "root"}
	body, _ := json.Marshal(payloadInvalid)
	req := httptest.NewRequest(http.MethodPost, "/api/hosts/test-connection", bytes.NewReader(body))
	authReq(req)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var res map[string]interface{}
	_ = json.NewDecoder(w.Body).Decode(&res)
	if res["ok"] == true {
		t.Fatalf("expected ok=false for empty host, got %+v", res)
	}

	// 2. Valid test connection via key
	payloadValid := map[string]interface{}{
		"host": "192.168.1.100",
		"port": 22,
		"user": "root",
		"custom_key": "-----BEGIN TEST KEY-----",
	}
	body, _ = json.Marshal(payloadValid)
	req = httptest.NewRequest(http.MethodPost, "/api/hosts/test-connection", bytes.NewReader(body))
	authReq(req)
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	res = nil
	_ = json.NewDecoder(w.Body).Decode(&res)
	if res["ok"] != true {
		t.Fatalf("expected ok=true, got %+v", res)
	}
	if !strings.Contains(res["output"].(string), "Linux 6.1.0-custom") {
		t.Fatalf("expected uname output, got %v", res["output"])
	}
	if res["auth_method"] != "key" {
		t.Fatalf("expected auth_method=key, got %v", res["auth_method"])
	}
}

func TestHostCreate_ImmediateInspectionAndEnrichment(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "pantau-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	db, err := store.Open(filepath.Join(tmpDir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	mockRunner := sshrunner.NewMockRunner()
	mockRunner.DefaultExec = func(cmd string) (string, string, int, error) {
		return "", "", 0, nil
	}
	factory := func(host string, port int, user, key string) (sshrunner.Runner, error) {
		return mockRunner, nil
	}
	ins := inspector.New(db, factory, nil)
	srv := NewServer(db, ins, nil)
	token := "valid_test_token"
	srv.sessions.Store(token, time.Now().Add(time.Hour))
	authReq := func(req *http.Request) {
		req.AddCookie(&http.Cookie{Name: "pantau_session", Value: token})
	}

	// Create host via POST /api/hosts
	newHost := map[string]interface{}{
		"name": "Immediate Host",
		"host": "192.168.10.5",
		"port": 22,
		"user": "root",
	}
	body, _ := json.Marshal(newHost)
	req := httptest.NewRequest(http.MethodPost, "/api/hosts", bytes.NewReader(body))
	authReq(req)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var createdHost store.Host
	_ = json.NewDecoder(w.Body).Decode(&createdHost)
	if createdHost.ID == 0 {
		t.Fatalf("expected non-zero host ID")
	}
	if !createdHost.IsInspecting {
		t.Fatalf("expected created host to have IsInspecting=true")
	}

	// Wait up to 100ms for background inspection goroutine
	time.Sleep(100 * time.Millisecond)

	// GET /api/hosts should list the host with enriched IsInspecting field
	req = httptest.NewRequest(http.MethodGet, "/api/hosts", nil)
	authReq(req)
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var hosts []store.Host
	_ = json.NewDecoder(w.Body).Decode(&hosts)
	if len(hosts) != 1 || hosts[0].Name != "Immediate Host" {
		t.Fatalf("unexpected hosts list: %+v", hosts)
	}
}

func TestDuplicateHostPrevention(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "pantau-dup-test-*")
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
	token := "dup_test_token"
	srv.sessions.Store(token, time.Now().Add(time.Hour))
	authReq := func(req *http.Request) {
		req.AddCookie(&http.Cookie{Name: "pantau_session", Value: token})
	}

	// 1. Create first host
	body1, _ := json.Marshal(map[string]interface{}{
		"name": "Web Primary",
		"host": "192.168.1.50",
		"port": 22,
		"user": "root",
	})
	req1 := httptest.NewRequest(http.MethodPost, "/api/hosts", bytes.NewReader(body1))
	authReq(req1)
	w1 := httptest.NewRecorder()
	srv.ServeHTTP(w1, req1)
	if w1.Code != http.StatusCreated {
		t.Fatalf("expected 201 Created for host 1, got %d: %s", w1.Code, w1.Body.String())
	}
	var created1 store.Host
	_ = json.NewDecoder(w1.Body).Decode(&created1)

	// 2. Create second host on different port (allowed)
	body2, _ := json.Marshal(map[string]interface{}{
		"name": "Web Secondary",
		"host": "192.168.1.50",
		"port": 2222,
		"user": "root",
	})
	req2 := httptest.NewRequest(http.MethodPost, "/api/hosts", bytes.NewReader(body2))
	authReq(req2)
	w2 := httptest.NewRecorder()
	srv.ServeHTTP(w2, req2)
	if w2.Code != http.StatusCreated {
		t.Fatalf("expected 201 Created for host 2 on diff port, got %d: %s", w2.Code, w2.Body.String())
	}
	var created2 store.Host
	_ = json.NewDecoder(w2.Body).Decode(&created2)

	// 3. Attempt to create duplicate host on 192.168.1.50:22 (should be rejected with 409 Conflict)
	bodyDup, _ := json.Marshal(map[string]interface{}{
		"name": "Web Clone",
		"host": " 192.168.1.50 ",
		"port": 22,
		"user": "admin",
	})
	reqDup := httptest.NewRequest(http.MethodPost, "/api/hosts", bytes.NewReader(bodyDup))
	authReq(reqDup)
	wDup := httptest.NewRecorder()
	srv.ServeHTTP(wDup, reqDup)
	if wDup.Code != http.StatusConflict {
		t.Fatalf("expected 409 Conflict for duplicate target, got %d: %s", wDup.Code, wDup.Body.String())
	}
	if !strings.Contains(wDup.Body.String(), "Web Primary") {
		t.Fatalf("expected error message to mention 'Web Primary', got: %s", wDup.Body.String())
	}
	if !strings.Contains(wDup.Body.String(), "already registered as") {
		t.Fatalf("expected default error in English, got: %s", wDup.Body.String())
	}

	// 3b. Test Indonesian localized duplicate error via Accept-Language header
	reqDupID := httptest.NewRequest(http.MethodPost, "/api/hosts", bytes.NewReader(bodyDup))
	authReq(reqDupID)
	reqDupID.Header.Set("Accept-Language", "id-ID,id;q=0.9")
	wDupID := httptest.NewRecorder()
	srv.ServeHTTP(wDupID, reqDupID)
	if wDupID.Code != http.StatusConflict {
		t.Fatalf("expected 409 Conflict for Indonesian header, got %d", wDupID.Code)
	}
	if !strings.Contains(wDupID.Body.String(), "sudah terdaftar sebagai") {
		t.Fatalf("expected Indonesian error message, got: %s", wDupID.Body.String())
	}

	// 4. Same display name but different IP is allowed
	bodySameName, _ := json.Marshal(map[string]interface{}{
		"name": "Web Primary",
		"host": "192.168.1.51",
		"port": 22,
		"user": "root",
	})
	reqSameName := httptest.NewRequest(http.MethodPost, "/api/hosts", bytes.NewReader(bodySameName))
	authReq(reqSameName)
	wSameName := httptest.NewRecorder()
	srv.ServeHTTP(wSameName, reqSameName)
	if wSameName.Code != http.StatusCreated {
		t.Fatalf("expected 201 Created for same name diff IP, got %d: %s", wSameName.Code, wSameName.Body.String())
	}

	// 5. PUT /api/hosts/{id} updating host 2 target to host 1's target (should fail with 409)
	bodyPutConflict, _ := json.Marshal(map[string]interface{}{
		"name": "Web Secondary",
		"host": "192.168.1.50",
		"port": 22,
		"user": "root",
	})
	reqPutConflict := httptest.NewRequest(http.MethodPut, fmt.Sprintf("/api/hosts/%d", created2.ID), bytes.NewReader(bodyPutConflict))
	authReq(reqPutConflict)
	wPutConflict := httptest.NewRecorder()
	srv.ServeHTTP(wPutConflict, reqPutConflict)
	if wPutConflict.Code != http.StatusConflict {
		t.Fatalf("expected 409 Conflict when editing target to an existing one, got %d: %s", wPutConflict.Code, wPutConflict.Body.String())
	}

	// 6. PUT /api/hosts/{id} updating host 2 with its own target (should succeed)
	bodyPutSelf, _ := json.Marshal(map[string]interface{}{
		"name": "Web Secondary Renamed",
		"host": "192.168.1.50",
		"port": 2222,
		"user": "root",
	})
	reqPutSelf := httptest.NewRequest(http.MethodPut, fmt.Sprintf("/api/hosts/%d", created2.ID), bytes.NewReader(bodyPutSelf))
	authReq(reqPutSelf)
	wPutSelf := httptest.NewRecorder()
	srv.ServeHTTP(wPutSelf, reqPutSelf)
	if wPutSelf.Code != http.StatusOK {
		t.Fatalf("expected 200 OK when editing self, got %d: %s", wPutSelf.Code, wPutSelf.Body.String())
	}
}

func TestWSTerminal_ClosesRunnerOnDisconnect(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "pantau-term-leak-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	db, err := store.Open(filepath.Join(tmpDir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	host := &store.Host{
		Name: "term-node",
		Host: "192.168.1.111",
		Port: 22,
		User: "root",
	}
	hID, err := db.CreateHost(host)
	if err != nil {
		t.Fatal(err)
	}
	host.ID = hID

	mockRunner := sshrunner.NewMockRunner()
	// Simulate long-running terminal command that does not exit on stdin EOF (like top or tail -f)
	termTerminated := make(chan struct{})
	mockRunner.TerminalFunc = func(in io.Reader, out io.Writer, cols, rows int, resizeChan <-chan [2]int) error {
		_, _ = fmt.Fprintln(out, "bash-5.1$ top")
		<-termTerminated
		return nil
	}
	mockRunnerDefaultClose := mockRunner.Close
	_ = mockRunnerDefaultClose

	ins := inspector.New(db, func(h string, p int, u, k string) (sshrunner.Runner, error) {
		return mockRunner, nil
	}, nil)

	srv := NewServer(db, ins, nil)
	token := "term_test_token"
	srv.sessions.Store(token, time.Now().Add(time.Hour))

	ts := httptest.NewServer(srv.mux)
	defer ts.Close()

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + fmt.Sprintf("/ws/terminal?host_id=%d", host.ID)
	headers := http.Header{}
	headers.Add("Cookie", "pantau_session="+token)

	ws, _, err := websocket.DefaultDialer.Dial(wsURL, headers)
	if err != nil {
		t.Fatalf("failed to dial websocket terminal: %v", err)
	}

	// Read initial banner
	_, msg, err := ws.ReadMessage()
	if err != nil {
		t.Fatalf("failed to read from terminal: %v", err)
	}
	if !strings.Contains(string(msg), "top") {
		t.Fatalf("expected top in initial terminal output, got %s", string(msg))
	}

	// Now client disconnects / closes browser
	_ = ws.Close()
	close(termTerminated)

	// Verify runner is promptly closed by handleTerminal
	deadline := time.Now().Add(2 * time.Second)
	for !mockRunner.IsClosed() && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}

	if !mockRunner.IsClosed() {
		t.Errorf("expected mock runner to be closed immediately on terminal websocket disconnect")
	}
}

func TestWSDockerLogs_ClosesRunnerOnDisconnect(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "pantau-logs-leak-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	db, err := store.Open(filepath.Join(tmpDir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	host := &store.Host{
		Name: "docker-node",
		Host: "192.168.1.112",
		Port: 22,
		User: "root",
	}
	hID, err := db.CreateHost(host)
	if err != nil {
		t.Fatal(err)
	}
	host.ID = hID

	mockRunner := sshrunner.NewMockRunner()
	// Simulate docker logs -f streaming forever
	streamTerminated := make(chan struct{})
	mockRunner.StreamFunc = func(cmd string, out io.Writer) error {
		_, _ = fmt.Fprintln(out, "2026-10-26T12:00:00Z Log message 1")
		<-streamTerminated
		return nil
	}

	ins := inspector.New(db, func(h string, p int, u, k string) (sshrunner.Runner, error) {
		return mockRunner, nil
	}, nil)

	srv := NewServer(db, ins, nil)
	token := "logs_test_token"
	srv.sessions.Store(token, time.Now().Add(time.Hour))

	ts := httptest.NewServer(srv.mux)
	defer ts.Close()

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + fmt.Sprintf("/ws/docker/logs?host_id=%d&container=test-service", host.ID)
	headers := http.Header{}
	headers.Add("Cookie", "pantau_session="+token)

	ws, _, err := websocket.DefaultDialer.Dial(wsURL, headers)
	if err != nil {
		t.Fatalf("failed to dial websocket docker logs: %v", err)
	}

	// Read initial log line
	_, msg, err := ws.ReadMessage()
	if err != nil {
		t.Fatalf("failed to read from docker logs: %v", err)
	}
	if !strings.Contains(string(msg), "Log message 1") {
		t.Fatalf("expected log output, got %s", string(msg))
	}

	// Now client disconnects / closes modal
	_ = ws.Close()
	close(streamTerminated)

	// Verify runner is promptly closed by handleWSDockerLogs
	deadline := time.Now().Add(2 * time.Second)
	for !mockRunner.IsClosed() && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}

	if !mockRunner.IsClosed() {
		t.Errorf("expected mock runner to be closed immediately on docker logs websocket disconnect")
	}
}

func TestHostListDropdownPositioning(t *testing.T) {
	html := string(embeddedHTML)

	// Verify dropdown styles in table view default to opening downwards to avoid colliding with fixed header
	if !strings.Contains(html, ".host-list-table .dropdown-menu { bottom: auto; top: 100%; margin-top: 6px; margin-bottom: 0; }") {
		t.Errorf("expected .host-list-table .dropdown-menu to open downwards by default")
	}

	// Verify open-down and open-up classes exist
	if !strings.Contains(html, ".dropdown.open.open-down .dropdown-menu") {
		t.Errorf("expected .dropdown.open.open-down .dropdown-menu class")
	}
	if !strings.Contains(html, ".dropdown.open.open-up .dropdown-menu") {
		t.Errorf("expected .dropdown.open.open-up .dropdown-menu class")
	}

	// Verify toggleDropdown contains smart collision handling
	if !strings.Contains(html, "spaceAbove") || !strings.Contains(html, "spaceBelow") {
		t.Errorf("expected toggleDropdown to calculate spaceAbove and spaceBelow")
	}
	if !strings.Contains(html, "open-down") || !strings.Contains(html, "open-up") {
		t.Errorf("expected toggleDropdown to toggle open-down and open-up")
	}

	// Verify host action buttons (view, terminal, and 3-dots more actions) share equal heights
	if !strings.Contains(html, ".btn-sm.btn-icon { min-width: 24px; height: 24px;") {
		t.Errorf("expected .btn-sm.btn-icon to define compact 24px height matching view and terminal buttons")
	}
	if !strings.Contains(html, ".btn-host-view, .btn-host-terminal { height: 24px; }") {
		t.Errorf("expected .btn-host-view and .btn-host-terminal to enforce 24px height")
	}
	if !strings.Contains(html, ".dropdown { position: relative; display: inline-flex; }") {
		t.Errorf("expected .dropdown to use inline-flex for aligned button embedding")
	}

	// Verify terminal action button does not widen when badges are active (fixed width & absolute badge indicator)
	if !strings.Contains(html, ".btn-host-terminal { position: relative; width: 26px; min-width: 26px; padding: 0;") {
		t.Errorf("expected .btn-host-terminal to have fixed width and relative positioning")
	}
	if !strings.Contains(html, ".btn-host-terminal .host-terminal-badge { position: absolute;") {
		t.Errorf("expected .host-terminal-badge to have absolute positioning to avoid button widening")
	}

	// Verify header icon buttons and badges maintain fixed dimensions
	if !strings.Contains(html, ".btn-icon { position: relative;") {
		t.Errorf("expected .btn-icon to use relative positioning for badge overlays")
	}
	if !strings.Contains(html, ".header-btn-badge { position: absolute;") {
		t.Errorf("expected .header-btn-badge to use absolute positioning")
	}
	if !strings.Contains(html, `id="activeTerminalBadge" class="badge badge-healthy header-btn-badge"`) {
		t.Errorf("expected activeTerminalBadge to include header-btn-badge class")
	}
}










