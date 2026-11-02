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
	"path/filepath"
	"strings"
	"testing"
	"time"

	"pantau/internal/store"
)

func setupTestWebServer(t *testing.T) (*Server, *store.DB, *http.Cookie) {
	t.Helper()
	tmpDir, err := os.MkdirTemp("", "pantau-status-web-*")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(tmpDir) })

	db, err := store.Open(filepath.Join(tmpDir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	srv := NewServer(db, nil, nil)
	token := "valid_admin_session_token"
	srv.sessions.Store(token, time.Now().Add(time.Hour))
	authCookie := &http.Cookie{Name: "pantau_session", Value: token}

	return srv, db, authCookie
}

func TestPublicStatusEndpoints_DisabledAndEnabled(t *testing.T) {
	srv, db, authCookie := setupTestWebServer(t)

	// 1. By default, status page is disabled -> /status and /api/status-page/summary should return 404
	reqHTML := httptest.NewRequest("GET", "/status", nil)
	recHTML := httptest.NewRecorder()
	srv.ServeHTTP(recHTML, reqHTML)
	if recHTML.Code != http.StatusNotFound {
		t.Errorf("expected 404 for disabled /status, got %d", recHTML.Code)
	}

	reqJSON := httptest.NewRequest("GET", "/api/status-page/summary", nil)
	recJSON := httptest.NewRecorder()
	srv.ServeHTTP(recJSON, reqJSON)
	if recJSON.Code != http.StatusNotFound {
		t.Errorf("expected 404 for disabled /api/status-page/summary, got %d", recJSON.Code)
	}

	// 2. Enable status page via settings
	_ = db.SetSetting("status_page_enabled", "true")
	_ = db.SetSetting("status_page_title", "Pantau Live Status")
	srv.rebuildStatusPageSnapshot()

	// 3. Request /status HTML -> 200 OK with strict CSP
	reqHTML = httptest.NewRequest("GET", "/status", nil)
	recHTML = httptest.NewRecorder()
	srv.ServeHTTP(recHTML, reqHTML)
	if recHTML.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for /status, got %d", recHTML.Code)
	}
	csp := recHTML.Header().Get("Content-Security-Policy")
	if !strings.Contains(csp, "default-src 'self'") || strings.Contains(csp, "ws:") {
		t.Errorf("expected strict CSP without websockets, got: %s", csp)
	}
	etagHTML := recHTML.Header().Get("ETag")
	if etagHTML == "" {
		t.Errorf("expected non-empty ETag on /status")
	}

	// 4. Test ETag 304 on /status
	reqHTML304 := httptest.NewRequest("GET", "/status", nil)
	reqHTML304.Header.Set("If-None-Match", etagHTML)
	recHTML304 := httptest.NewRecorder()
	srv.ServeHTTP(recHTML304, reqHTML304)
	if recHTML304.Code != http.StatusNotModified {
		t.Errorf("expected 304 Not Modified for matching ETag, got %d", recHTML304.Code)
	}

	// 5. Test /api/status-page/summary
	reqSummary := httptest.NewRequest("GET", "/api/status-page/summary", nil)
	recSummary := httptest.NewRecorder()
	srv.ServeHTTP(recSummary, reqSummary)
	if recSummary.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for summary, got %d", recSummary.Code)
	}

	var summary store.PublicStatusSummary
	if err := json.NewDecoder(recSummary.Body).Decode(&summary); err != nil {
		t.Fatalf("failed to decode summary JSON: %v", err)
	}
	if summary.Page.Title != "Pantau Live Status" {
		t.Errorf("expected title 'Pantau Live Status', got '%s'", summary.Page.Title)
	}

	// 6. Test Zero-Trust: Pass admin cookie to public summary endpoint, ensure no leaks
	reqZeroTrust := httptest.NewRequest("GET", "/api/status-page/summary", nil)
	reqZeroTrust.AddCookie(authCookie)
	recZeroTrust := httptest.NewRecorder()
	srv.ServeHTTP(recZeroTrust, reqZeroTrust)
	if recZeroTrust.Code != http.StatusOK {
		t.Errorf("expected 200 OK regardless of admin cookie, got %d", recZeroTrust.Code)
	}

	// 7. Test Gzip Compression
	reqGzip := httptest.NewRequest("GET", "/api/status-page/summary", nil)
	reqGzip.Header.Set("Accept-Encoding", "gzip")
	recGzip := httptest.NewRecorder()
	srv.ServeHTTP(recGzip, reqGzip)
	if recGzip.Header().Get("Content-Encoding") != "gzip" {
		t.Errorf("expected Content-Encoding: gzip, got '%s'", recGzip.Header().Get("Content-Encoding"))
	}
	gzReader, err := gzip.NewReader(recGzip.Body)
	if err != nil {
		t.Fatalf("gzip.NewReader failed: %v", err)
	}
	decompressed, err := io.ReadAll(gzReader)
	if err != nil {
		t.Fatalf("failed reading gzip stream: %v", err)
	}
	if !strings.Contains(string(decompressed), "Pantau Live Status") {
		t.Errorf("expected decompressed body to contain title")
	}
}

func TestPublicStatusSummaryRateLimiter(t *testing.T) {
	srv, db, _ := setupTestWebServer(t)
	_ = db.SetSetting("status_page_enabled", "true")
	srv.rebuildStatusPageSnapshot()

	// Override limiter with small burst for fast testing
	srv.statusLimiter = newShardedRateLimiter(0.1, 3.0) // 3 tokens burst

	clientIP := "198.51.100.42:54321"

	for i := 0; i < 3; i++ {
		req := httptest.NewRequest("GET", "/api/status-page/summary", nil)
		req.RemoteAddr = clientIP
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Errorf("request %d expected 200 OK, got %d", i+1, rec.Code)
		}
	}

	// 4th request should exceed burst -> 429 Too Many Requests
	reqThrottled := httptest.NewRequest("GET", "/api/status-page/summary", nil)
	reqThrottled.RemoteAddr = clientIP
	recThrottled := httptest.NewRecorder()
	srv.ServeHTTP(recThrottled, reqThrottled)
	if recThrottled.Code != http.StatusTooManyRequests {
		t.Errorf("expected 429 Too Many Requests, got %d", recThrottled.Code)
	}
	if recThrottled.Header().Get("Retry-After") != "60" {
		t.Errorf("expected Retry-After header: 60, got %s", recThrottled.Header().Get("Retry-After"))
	}
}

func TestAdminStatusPageSettingsAndTargetAPI(t *testing.T) {
	srv, db, authCookie := setupTestWebServer(t)

	// Create a host for target testing
	hostID, _ := db.CreateHost(&store.Host{
		Name: "App Server 1",
		Host: "10.10.10.1",
		Port: 22,
		User: "root",
	})

	// 1. GET Settings
	reqGetSettings := httptest.NewRequest("GET", "/api/settings/status-page", nil)
	reqGetSettings.AddCookie(authCookie)
	recGetSettings := httptest.NewRecorder()
	srv.ServeHTTP(recGetSettings, reqGetSettings)
	if recGetSettings.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for GET settings, got %d", recGetSettings.Code)
	}

	// 2. POST Settings (Update)
	settingsPayload := map[string]any{
		"enabled":      true,
		"title":        "Custom Status Title",
		"organization": "Custom Org",
		"header_text":  "Custom Header",
		"logo_url":     "/static/logo.png",
		"support_url":  "https://support.example.com",
	}
	bodyBytes, _ := json.Marshal(settingsPayload)
	reqPostSettings := httptest.NewRequest("POST", "/api/settings/status-page", bytes.NewReader(bodyBytes))
	reqPostSettings.AddCookie(authCookie)
	recPostSettings := httptest.NewRecorder()
	srv.ServeHTTP(recPostSettings, reqPostSettings)
	if recPostSettings.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for POST settings, got %d", recPostSettings.Code)
	}

	// 3. Create Status Target
	targetPayload := map[string]any{
		"target_type": "host",
		"host_id":     hostID,
		"public_name": "Core Application Service",
		"group_name":  "Production Tier",
		"sort_order":  1,
		"enabled":     true,
	}
	tBytes, _ := json.Marshal(targetPayload)
	reqCreateTarget := httptest.NewRequest("POST", "/api/settings/status-page/targets", bytes.NewReader(tBytes))
	reqCreateTarget.AddCookie(authCookie)
	recCreateTarget := httptest.NewRecorder()
	srv.ServeHTTP(recCreateTarget, reqCreateTarget)
	if recCreateTarget.Code != http.StatusCreated {
		t.Fatalf("expected 201 Created for target, got %d: %s", recCreateTarget.Code, recCreateTarget.Body.String())
	}

	var createdTarget store.StatusPageTarget
	_ = json.NewDecoder(recCreateTarget.Body).Decode(&createdTarget)
	if createdTarget.ID == 0 {
		t.Fatalf("expected non-zero created target ID")
	}

	// 4. Update Target
	createdTarget.PublicName = "Core App (Renamed)"
	tUpBytes, _ := json.Marshal(createdTarget)
	reqUpdateTarget := httptest.NewRequest("PUT", fmt.Sprintf("/api/settings/status-page/targets/%d", createdTarget.ID), bytes.NewReader(tUpBytes))
	reqUpdateTarget.AddCookie(authCookie)
	recUpdateTarget := httptest.NewRecorder()
	srv.ServeHTTP(recUpdateTarget, reqUpdateTarget)
	if recUpdateTarget.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for target update, got %d", recUpdateTarget.Code)
	}

	// 5. Create Incident
	incPayload := map[string]any{
		"title":            "Network Degraded",
		"status":           "investigating",
		"impact":           "minor",
		"message":          "Investigating slow network response.",
		"affected_targets": []int64{createdTarget.ID},
	}
	incBytes, _ := json.Marshal(incPayload)
	reqCreateInc := httptest.NewRequest("POST", "/api/settings/status-page/incidents", bytes.NewReader(incBytes))
	reqCreateInc.AddCookie(authCookie)
	recCreateInc := httptest.NewRecorder()
	srv.ServeHTTP(recCreateInc, reqCreateInc)
	if recCreateInc.Code != http.StatusCreated {
		t.Fatalf("expected 201 Created for incident, got %d", recCreateInc.Code)
	}

	var createdInc store.StatusPageIncident
	_ = json.NewDecoder(recCreateInc.Body).Decode(&createdInc)
	if createdInc.ID == 0 {
		t.Fatalf("expected non-zero incident ID")
	}

	// 6. Post Incident Update
	updatePayload := map[string]any{
		"status":  "resolved",
		"message": "Service recovered.",
	}
	uBytes, _ := json.Marshal(updatePayload)
	reqPostUpdate := httptest.NewRequest("POST", fmt.Sprintf("/api/settings/status-page/incidents/%d/updates", createdInc.ID), bytes.NewReader(uBytes))
	reqPostUpdate.AddCookie(authCookie)
	recPostUpdate := httptest.NewRecorder()
	srv.ServeHTTP(recPostUpdate, reqPostUpdate)
	if recPostUpdate.Code != http.StatusCreated {
		t.Fatalf("expected 201 Created for update, got %d: %s", recPostUpdate.Code, recPostUpdate.Body.String())
	}

	// 7. Delete Target
	reqDelTarget := httptest.NewRequest("DELETE", fmt.Sprintf("/api/settings/status-page/targets/%d", createdTarget.ID), nil)
	reqDelTarget.AddCookie(authCookie)
	recDelTarget := httptest.NewRecorder()
	srv.ServeHTTP(recDelTarget, reqDelTarget)
	if recDelTarget.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for target delete, got %d", recDelTarget.Code)
	}
}
