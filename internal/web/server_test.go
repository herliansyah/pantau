package web

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
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
	// Verify compression ratio: ~56KB vs ~261KB
	if wGz.Body.Len() > 80000 {
		t.Errorf("expected gzipped body < 80KB, got %d bytes", wGz.Body.Len())
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




