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

	// 6. Storage Monitors Endpoints (POST / GET / DELETE)
	// 6a. Empty path validation
	badBody, _ := json.Marshal(map[string]string{"path": "", "label": "test"})
	rec6a := httptest.NewRecorder()
	req6a := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/hosts/%d/storage-monitors", hostID), bytes.NewReader(badBody))
	req6a.AddCookie(authCookie)
	srv.mux.ServeHTTP(rec6a, req6a)
	if rec6a.Code != http.StatusBadRequest {
		t.Errorf("expected 400 Bad Request for empty path, got %d", rec6a.Code)
	}

	// 6b. Traversal path validation
	travBody, _ := json.Marshal(map[string]string{"path": "/var/log/../../etc", "label": "test"})
	rec6b := httptest.NewRecorder()
	req6b := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/hosts/%d/storage-monitors", hostID), bytes.NewReader(travBody))
	req6b.AddCookie(authCookie)
	srv.mux.ServeHTTP(rec6b, req6b)
	if rec6b.Code != http.StatusBadRequest {
		t.Errorf("expected 400 Bad Request for path traversal, got %d", rec6b.Code)
	}

	// 6b2. Newline and Tab path validation
	nlBody, _ := json.Marshal(map[string]string{"path": "/var/log/app.log\nreboot", "label": "test"})
	rec6b2 := httptest.NewRecorder()
	req6b2 := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/hosts/%d/storage-monitors", hostID), bytes.NewReader(nlBody))
	req6b2.AddCookie(authCookie)
	srv.mux.ServeHTTP(rec6b2, req6b2)
	if rec6b2.Code != http.StatusBadRequest {
		t.Errorf("expected 400 Bad Request for newline in path, got %d", rec6b2.Code)
	}

	tabBody, _ := json.Marshal(map[string]string{"path": "/var/log/app.log\t123", "label": "test"})
	recTab := httptest.NewRecorder()
	reqTab := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/hosts/%d/storage-monitors", hostID), bytes.NewReader(tabBody))
	reqTab.AddCookie(authCookie)
	srv.mux.ServeHTTP(recTab, reqTab)
	if recTab.Code != http.StatusBadRequest {
		t.Errorf("expected 400 Bad Request for tab in path, got %d", recTab.Code)
	}

	// 6c. Valid POST storage monitor with max_size threshold
	validBody, _ := json.Marshal(map[string]interface{}{"path": "/var/log/custom.log", "label": "Custom Log", "max_size": "10 GB"})
	rec6c := httptest.NewRecorder()
	req6c := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/hosts/%d/storage-monitors", hostID), bytes.NewReader(validBody))
	req6c.AddCookie(authCookie)
	srv.mux.ServeHTTP(rec6c, req6c)
	if rec6c.Code != http.StatusOK {
		t.Errorf("expected 200 OK for valid storage-monitor POST, got %d: %s", rec6c.Code, rec6c.Body.String())
	}
	var postResp map[string]interface{}
	if err := json.Unmarshal(rec6c.Body.Bytes(), &postResp); err != nil || postResp["ok"] != true {
		t.Errorf("unexpected post response: %v", rec6c.Body.String())
	}

	// 6d. GET storage monitors
	rec6d := httptest.NewRecorder()
	req6d := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/hosts/%d/storage-monitors", hostID), nil)
	req6d.AddCookie(authCookie)
	srv.mux.ServeHTTP(rec6d, req6d)
	if rec6d.Code != http.StatusOK {
		t.Errorf("expected 200 OK for GET storage-monitors, got %d: %s", rec6d.Code, rec6d.Body.String())
	}
	var monList []storagetriage.MonitoredItem
	if err := json.Unmarshal(rec6d.Body.Bytes(), &monList); err != nil {
		t.Fatalf("failed to decode GET storage-monitors json: %v", err)
	}
	if len(monList) != 1 || monList[0].Path != "/var/log/custom.log" || monList[0].Label != "Custom Log" || monList[0].MaxSizeBytes != 10*1024*1024*1024 {
		t.Errorf("unexpected monList: %+v", monList)
	}

	// 6d2. PATCH storage monitor limit
	patchBody, _ := json.Marshal(map[string]interface{}{"path": "/var/log/custom.log", "max_size": "25 GB"})
	rec6d2 := httptest.NewRecorder()
	req6d2 := httptest.NewRequest(http.MethodPatch, fmt.Sprintf("/api/hosts/%d/storage-monitors", hostID), bytes.NewReader(patchBody))
	req6d2.AddCookie(authCookie)
	srv.mux.ServeHTTP(rec6d2, req6d2)
	if rec6d2.Code != http.StatusOK {
		t.Errorf("expected 200 OK for PATCH storage-monitors, got %d: %s", rec6d2.Code, rec6d2.Body.String())
	}

	// 6e. DELETE storage monitor (with trailing slash flexibility)
	rec6e := httptest.NewRecorder()
	req6e := httptest.NewRequest(http.MethodDelete, fmt.Sprintf("/api/hosts/%d/storage-monitors?path=%s", hostID, "/var/log/custom.log/"), nil)
	req6e.AddCookie(authCookie)
	srv.mux.ServeHTTP(rec6e, req6e)
	if rec6e.Code != http.StatusOK {
		t.Errorf("expected 200 OK for DELETE storage-monitors, got %d: %s", rec6e.Code, rec6e.Body.String())
	}

	// 6f. GET storage monitors after delete
	rec6f := httptest.NewRecorder()
	req6f := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/hosts/%d/storage-monitors", hostID), nil)
	req6f.AddCookie(authCookie)
	srv.mux.ServeHTTP(rec6f, req6f)
	var emptyList []storagetriage.MonitoredItem
	_ = json.Unmarshal(rec6f.Body.Bytes(), &emptyList)
	if len(emptyList) != 0 {
		t.Errorf("expected 0 monitors after delete, got %d", len(emptyList))
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
		"renderMonitoredPathsPanel",
		"showAddMonitoredPathModal",
		"closeAddMonitoredPathModal",
		"submitAddMonitoredPath",
		"quickAddMonitoredPath",
		"deleteMonitoredPath",
		"refreshMonitoredPaths",
		"checkSingleMonitoredPath",
		"showEditMonitorLimitModal",
		"closeEditMonitorLimitModal",
		"submitEditMonitorLimit",
		"storage-monitors",
	}
	for _, fn := range jsFunctions {
		if !strings.Contains(html, fn) {
			t.Errorf("expected JS function/state %s in index.html", fn)
		}
	}

	// 4. Verify Alert banner and host card deep links
	if !strings.Contains(html, "openStorageTriageFromAlert") {
		t.Errorf("expected openStorageTriageFromAlert in alert banner overlay")
	}
	if !strings.Contains(html, "diskPct > 85 ?") || !strings.Contains(html, "openStorageTriageFromAlert(${h.id})") {
		t.Errorf("expected host card disk widget deep link when diskPct > 85")
	}

	// 4b. Verify executeSafeReclaim defines isWin to prevent ReferenceError
	if !strings.Contains(html, "async function executeSafeReclaim") || !strings.Contains(html, "const isWin = currentHost && currentHost.os_type === 'windows';") {
		t.Errorf("expected executeSafeReclaim to define isWin")
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
		`"triage_top_hogs_win"`,
		`"triage_badge_admin"`,
		`"triage_reclaimable_desc_win"`,
		`"triage_analyzing_sub_win"`,
		`"triage_no_reclaim_win"`,
		`"triage_no_hogs_win"`,
		`"triage_monitored_title"`,
		`"triage_add_monitor_btn"`,
		`"triage_no_monitors"`,
		`"triage_max_size_label"`,
		`"triage_status_over_limit"`,
		`"triage_status_warning"`,
		`"triage_set_limit_btn"`,
		`Stop-Process -Name robocopy -Force`,
	}
	for _, check := range i18nChecks {
		if !strings.Contains(html, check) {
			t.Errorf("expected i18n marker %s in index.html", check)
		}
	}
}

func TestWindowsStorageTriageEndpoints(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "pantau-win-triage-*")
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
	token := "test_win_triage_token"
	srv.sessions.Store(token, time.Now().Add(time.Hour))
	authCookie := &http.Cookie{Name: "pantau_session", Value: token}

	winHostID, err := db.CreateHost(&store.Host{
		Name:   "Windows Target Server",
		Host:   "127.0.0.1",
		Port:   22,
		User:   "Administrator",
		OSType: "windows",
	})
	if err != nil {
		t.Fatal(err)
	}

	// 1. Storage Triage on Offline/Unconnected Windows Host returns 500 cleanly
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/hosts/%d/storage-triage", winHostID), nil)
	req.AddCookie(authCookie)
	srv.mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError && rec.Code != http.StatusBadRequest {
		t.Errorf("expected 500 or 400 for unconnectable host, got %d: %s", rec.Code, rec.Body.String())
	}

	// 2. Windows Storage Reclaim Validation: Protected system path rejection
	reclaimBody, _ := json.Marshal(storagetriage.ReclaimRequest{
		ActionKey:  "truncate",
		TargetPath: `C:\Windows\System32\cmd.exe`,
	})
	rec2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/hosts/%d/storage-reclaim", winHostID), bytes.NewReader(reclaimBody))
	req2.AddCookie(authCookie)
	srv.mux.ServeHTTP(rec2, req2)

	if rec2.Code != http.StatusBadRequest {
		t.Errorf("expected 400 Bad Request for protected Windows path truncate, got %d: %s", rec2.Code, rec2.Body.String())
	}

	// 2b. Windows Storage Reclaim Validation: Protected system path on non-C drive (e.g. D:\)
	reclaimBody2b, _ := json.Marshal(storagetriage.ReclaimRequest{
		ActionKey:  "truncate",
		TargetPath: `D:\Windows\System32\cmd.exe`,
	})
	rec2b := httptest.NewRecorder()
	req2b := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/hosts/%d/storage-reclaim", winHostID), bytes.NewReader(reclaimBody2b))
	req2b.AddCookie(authCookie)
	srv.mux.ServeHTTP(rec2b, req2b)

	if rec2b.Code != http.StatusBadRequest {
		t.Errorf("expected 400 Bad Request for protected Windows path on D: drive, got %d: %s", rec2b.Code, rec2b.Body.String())
	}

	// 3. Windows Storage Reclaim Validation: Database file rejection (e.g. MSSQL .mdf)
	reclaimBody3, _ := json.Marshal(storagetriage.ReclaimRequest{
		ActionKey:  "truncate",
		TargetPath: `C:\data\sql\mydb.mdf`,
	})
	rec3 := httptest.NewRecorder()
	req3 := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/hosts/%d/storage-reclaim", winHostID), bytes.NewReader(reclaimBody3))
	req3.AddCookie(authCookie)
	srv.mux.ServeHTTP(rec3, req3)

	if rec3.Code != http.StatusBadRequest {
		t.Errorf("expected 400 Bad Request for MSSQL .mdf truncate, got %d: %s", rec3.Code, rec3.Body.String())
	}

	// 3b. Windows Storage Reclaim Validation: Database secondary file rejection (MSSQL .ndf)
	reclaimBody3b, _ := json.Marshal(storagetriage.ReclaimRequest{
		ActionKey:  "truncate",
		TargetPath: `C:\data\sql\mydb_sec.ndf`,
	})
	rec3b := httptest.NewRecorder()
	req3b := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/hosts/%d/storage-reclaim", winHostID), bytes.NewReader(reclaimBody3b))
	req3b.AddCookie(authCookie)
	srv.mux.ServeHTTP(rec3b, req3b)

	if rec3b.Code != http.StatusBadRequest {
		t.Errorf("expected 400 Bad Request for MSSQL .ndf truncate, got %d: %s", rec3b.Code, rec3b.Body.String())
	}

	// 3c. Windows Storage Reclaim Validation: Virtual disk snapshot rejection (.avhdx)
	reclaimBody3c, _ := json.Marshal(storagetriage.ReclaimRequest{
		ActionKey:  "truncate",
		TargetPath: `C:\vm\disk.avhdx`,
	})
	rec3c := httptest.NewRecorder()
	req3c := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/hosts/%d/storage-reclaim", winHostID), bytes.NewReader(reclaimBody3c))
	req3c.AddCookie(authCookie)
	srv.mux.ServeHTTP(rec3c, req3c)

	if rec3c.Code != http.StatusBadRequest {
		t.Errorf("expected 400 Bad Request for Hyper-V .avhdx truncate, got %d: %s", rec3c.Code, rec3c.Body.String())
	}

	// 4. Windows Storage Reclaim Validation: Path traversal rejection
	reclaimBody4, _ := json.Marshal(storagetriage.ReclaimRequest{
		ActionKey:  "truncate",
		TargetPath: `C:\temp\..\Windows\System32\cmd.exe`,
	})
	rec4 := httptest.NewRecorder()
	req4 := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/hosts/%d/storage-reclaim", winHostID), bytes.NewReader(reclaimBody4))
	req4.AddCookie(authCookie)
	srv.mux.ServeHTTP(rec4, req4)

	if rec4.Code != http.StatusBadRequest {
		t.Errorf("expected 400 Bad Request for path traversal truncate, got %d: %s", rec4.Code, rec4.Body.String())
	}

	// 4b. Windows Storage Reclaim Validation: Directory path rejection (trailing slash)
	reclaimBody4b, _ := json.Marshal(storagetriage.ReclaimRequest{
		ActionKey:  "truncate",
		TargetPath: `C:\inetpub\logs\`,
	})
	rec4b := httptest.NewRecorder()
	req4b := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/hosts/%d/storage-reclaim", winHostID), bytes.NewReader(reclaimBody4b))
	req4b.AddCookie(authCookie)
	srv.mux.ServeHTTP(rec4b, req4b)

	if rec4b.Code != http.StatusBadRequest {
		t.Errorf("expected 400 Bad Request for directory path truncate, got %d: %s", rec4b.Code, rec4b.Body.String())
	}

	// 4c. Windows Storage Reclaim Validation: dumpstack.log rejection
	reclaimBody4c, _ := json.Marshal(storagetriage.ReclaimRequest{
		ActionKey:  "truncate",
		TargetPath: `C:\dumpstack.log`,
	})
	rec4c := httptest.NewRecorder()
	req4c := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/hosts/%d/storage-reclaim", winHostID), bytes.NewReader(reclaimBody4c))
	req4c.AddCookie(authCookie)
	srv.mux.ServeHTTP(rec4c, req4c)

	if rec4c.Code != http.StatusBadRequest {
		t.Errorf("expected 400 Bad Request for dumpstack.log truncate, got %d: %s", rec4c.Code, rec4c.Body.String())
	}

	// 4d. Windows Storage Reclaim Validation: Database backup .bak rejection
	reclaimBody4d, _ := json.Marshal(storagetriage.ReclaimRequest{
		ActionKey:  "truncate",
		TargetPath: `C:\backup\mydb.bak`,
	})
	rec4d := httptest.NewRecorder()
	req4d := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/hosts/%d/storage-reclaim", winHostID), bytes.NewReader(reclaimBody4d))
	req4d.AddCookie(authCookie)
	srv.mux.ServeHTTP(rec4d, req4d)

	if rec4d.Code != http.StatusBadRequest {
		t.Errorf("expected 400 Bad Request for .bak truncate, got %d: %s", rec4d.Code, rec4d.Body.String())
	}

	// 4e. Windows Storage Reclaim Validation: Archive .7z rejection
	reclaimBody4e, _ := json.Marshal(storagetriage.ReclaimRequest{
		ActionKey:  "truncate",
		TargetPath: `C:\temp\archive.7z`,
	})
	rec4e := httptest.NewRecorder()
	req4e := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/hosts/%d/storage-reclaim", winHostID), bytes.NewReader(reclaimBody4e))
	req4e.AddCookie(authCookie)
	srv.mux.ServeHTTP(rec4e, req4e)

	if rec4e.Code != http.StatusBadRequest {
		t.Errorf("expected 400 Bad Request for .7z truncate, got %d: %s", rec4e.Code, rec4e.Body.String())
	}

	// 5. Windows Storage Reclaim Validation: Invalid service reload command injection
	reclaimBody5, _ := json.Marshal(storagetriage.ReclaimRequest{
		ActionKey:  "reload_service",
		TargetPath: `W3SVC; Stop-Computer`,
	})
	rec5 := httptest.NewRecorder()
	req5 := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/hosts/%d/storage-reclaim", winHostID), bytes.NewReader(reclaimBody5))
	req5.AddCookie(authCookie)
	srv.mux.ServeHTTP(rec5, req5)

	if rec5.Code != http.StatusBadRequest {
		t.Errorf("expected 400 Bad Request for invalid Windows service name, got %d: %s", rec5.Code, rec5.Body.String())
	}
}

