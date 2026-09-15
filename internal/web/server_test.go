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
