package web

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"pantau/internal/storagetriage"
	"pantau/internal/store"
)

func TestStorageTriageEndpoints(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "pantau-web-triage-*")
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
	token := "test_triage_token"
	srv.sessions.Store(token, time.Now().Add(time.Hour))
	authCookie := &http.Cookie{Name: "pantau_session", Value: token}

	hostID, err := db.CreateHost(&store.Host{
		Name: "Storage Triage Host",
		Host: "127.0.0.1",
		Port: 22,
		User: "root",
	})
	if err != nil {
		t.Fatal(err)
	}

	// 1. Storage Triage on Offline Host
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/hosts/%d/storage-triage", hostID), nil)
	req.AddCookie(authCookie)
	srv.mux.ServeHTTP(rec, req)

	// Since host has no active SSH connection in test, it returns 500 error cleanly with json
	if rec.Code != http.StatusInternalServerError && rec.Code != http.StatusBadRequest {
		t.Errorf("expected 500 or 400 for unconnectable host, got %d: %s", rec.Code, rec.Body.String())
	}

	// 2. Storage Reclaim Validation (Protected system path rejection)
	reclaimBody, _ := json.Marshal(storagetriage.ReclaimRequest{
		ActionKey:  "truncate",
		TargetPath: "/etc/passwd",
	})
	rec2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/hosts/%d/storage-reclaim", hostID), bytes.NewReader(reclaimBody))
	req2.AddCookie(authCookie)
	srv.mux.ServeHTTP(rec2, req2)

	if rec2.Code != http.StatusBadRequest {
		t.Errorf("expected 400 Bad Request for protected path truncate, got %d: %s", rec2.Code, rec2.Body.String())
	}

	// 2a. Storage Reclaim Validation (Path traversal rejection)
	reclaimBody2a, _ := json.Marshal(storagetriage.ReclaimRequest{
		ActionKey:  "truncate",
		TargetPath: "/var/log/../../etc/shadow",
	})
	rec2a := httptest.NewRecorder()
	req2a := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/hosts/%d/storage-reclaim", hostID), bytes.NewReader(reclaimBody2a))
	req2a.AddCookie(authCookie)
	srv.mux.ServeHTTP(rec2a, req2a)

	if rec2a.Code != http.StatusBadRequest {
		t.Errorf("expected 400 Bad Request for path traversal truncate, got %d: %s", rec2a.Code, rec2a.Body.String())
	}

	// 2b. Storage Reclaim Validation (Database file rejection: e.g. MySQL .ibd)
	reclaimBody2b, _ := json.Marshal(storagetriage.ReclaimRequest{
		ActionKey:  "truncate",
		TargetPath: "/var/lib/mysql/surat/pesan.ibd",
	})
	rec2b := httptest.NewRecorder()
	req2b := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/hosts/%d/storage-reclaim", hostID), bytes.NewReader(reclaimBody2b))
	req2b.AddCookie(authCookie)
	srv.mux.ServeHTTP(rec2b, req2b)

	if rec2b.Code != http.StatusBadRequest {
		t.Errorf("expected 400 Bad Request for database file truncate, got %d: %s", rec2b.Code, rec2b.Body.String())
	}

	// 3. Storage Reclaim Unit Name Injection Validation
	reclaimBody3, _ := json.Marshal(storagetriage.ReclaimRequest{
		ActionKey:  "reload_service",
		TargetPath: "nginx; reboot",
	})
	rec3 := httptest.NewRecorder()
	req3 := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/hosts/%d/storage-reclaim", hostID), bytes.NewReader(reclaimBody3))
	req3.AddCookie(authCookie)
	srv.mux.ServeHTTP(rec3, req3)

	if rec3.Code != http.StatusBadRequest {
		t.Errorf("expected 400 Bad Request for dangerous unit name, got %d: %s", rec3.Code, rec3.Body.String())
	}

	// 4. Method Not Allowed
	rec4 := httptest.NewRecorder()
	req4 := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/hosts/%d/storage-triage", hostID), nil)
	req4.AddCookie(authCookie)
	srv.mux.ServeHTTP(rec4, req4)
	if rec4.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405 Method Not Allowed, got %d", rec4.Code)
	}

	// 5. Storage Triage Throttle Endpoint
	rec5 := httptest.NewRecorder()
	req5 := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/hosts/%d/storage-triage-throttle", hostID), nil)
	req5.AddCookie(authCookie)
	srv.mux.ServeHTTP(rec5, req5)
	if rec5.Code != http.StatusOK {
		t.Errorf("expected 200 OK for storage-triage-throttle, got %d: %s", rec5.Code, rec5.Body.String())
	}
	var throttleResp map[string]interface{}
	if err := json.Unmarshal(rec5.Body.Bytes(), &throttleResp); err != nil {
		t.Fatalf("failed to parse throttle json: %v", err)
	}
	if throttleResp["state"] == nil || throttleResp["allowed"] == nil {
		t.Errorf("expected state and allowed in throttle response, got %v", throttleResp)
	}
}

func TestStorageTriageUIInIndexHTML(t *testing.T) {
	html := string(embeddedHTML)

	// 1. Verify Storage Triage tab markup
	if !strings.Contains(html, `data-tab="storage"`) {
		t.Errorf("expected data-tab=\"storage\" in index.html")
	}
	if !strings.Contains(html, `switchTab('storage')`) {
		t.Errorf("expected switchTab('storage') handler in index.html")
	}
	if !strings.Contains(html, `data-i18n="tab_storage"`) {
		t.Errorf("expected data-i18n=\"tab_storage\" in index.html")
	}

	// 2. Verify CSS rules
	cssRules := []string{
		".triage-grid",
		".triage-item-card",
		".triage-bar-bg",
		".triage-bar-fill",
	}
	for _, rule := range cssRules {
		if !strings.Contains(html, rule) {
			t.Errorf("expected CSS rule %s in index.html", rule)
		}
	}

	// 3. Verify JavaScript functions and scanning state handling
	jsFunctions := []string{
		"renderStorageTriageTab",
		"runStorageTriageProbe",
		"renderStorageTriageView",
		"getStorageTriageScanningHTML",
		"isStorageTriageScanning",
		"autoRefreshTabs",
		"executeSafeReclaim",
		"executeTruncateFile",
		"executeReloadService",
		"executeReloadServiceWithPassword",
		"openStorageTriageFromAlert",
		"checkStorageTriageThrottle",
		"updateStorageTriageThrottleUI",
		"showForceScanConfirmModal",
		"btnForceStorageTriage",
		"triageThrottleBadge",
	}
	for _, fn := range jsFunctions {
		if !strings.Contains(html, fn) {
			t.Errorf("expected JS function/state %s in index.html", fn)
		}
	}

	// 4. Verify Alert banner deep link
	if !strings.Contains(html, "openStorageTriageFromAlert") {
		t.Errorf("expected openStorageTriageFromAlert in alert banner overlay")
	}

	// 5. Verify i18n dictionary keys and modal data-i18n attributes
	i18nChecks := []string{
		`data-i18n="sudo_auth_title"`,
		`data-i18n="sudo_auth_label"`,
		`data-i18n-placeholder="sudo_auth_placeholder"`,
		`"run_triage"`,
		`"triage_safe_catalog"`,
		`"triage_runaway_detected"`,
		`"triage_ghost_detected"`,
	}
	for _, check := range i18nChecks {
		if !strings.Contains(html, check) {
			t.Errorf("expected i18n marker %s in index.html", check)
		}
	}
}

