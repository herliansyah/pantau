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

	"pantau/internal/store"
)

func TestMetricsAndProbesAPI(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "pantau-web-metrics-*")
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
	token := "test_metrics_token"
	srv.sessions.Store(token, time.Now().Add(time.Hour))
	authCookie := &http.Cookie{Name: "pantau_session", Value: token}

	hostID, err := db.CreateHost(&store.Host{
		Name:           "Web Metrics Host",
		Host:           "127.0.0.1",
		Port:           22,
		User:           "root",
		MetricsEnabled: -1,
	})
	if err != nil {
		t.Fatal(err)
	}

	// 1. GET /api/metric-probes/presets
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/metric-probes/presets", nil)
	req.AddCookie(authCookie)
	srv.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var presets []map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&presets); err != nil || len(presets) < 3 {
		t.Fatalf("failed decoding presets or too few: %v", err)
	}

	// 2. POST /api/hosts/{id}/metric-probes
	probeBody := `{"name": "Nginx Status", "command": "curl -s http://127.0.0.1/nginx_status", "enabled": true}`
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/hosts/%d/metric-probes", hostID), bytes.NewBufferString(probeBody))
	req.AddCookie(authCookie)
	srv.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (body: %s)", rec.Code, rec.Body.String())
	}
	var postResp struct {
		Ok    bool                  `json:"ok"`
		Probe store.HostMetricProbe `json:"probe"`
	}
	_ = json.NewDecoder(rec.Body).Decode(&postResp)
	if !postResp.Ok || postResp.Probe.ID <= 0 {
		t.Fatalf("expected created probe, got: %+v", postResp)
	}
	probeID := postResp.Probe.ID

	// 3. GET /api/hosts/{id}/metric-probes
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/hosts/%d/metric-probes", hostID), nil)
	req.AddCookie(authCookie)
	srv.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var probeList []store.HostMetricProbe
	_ = json.NewDecoder(rec.Body).Decode(&probeList)
	if len(probeList) != 1 || probeList[0].ID != probeID {
		t.Fatalf("expected 1 probe in list, got: %+v", probeList)
	}

	// 4. Save some samples and query GET /api/hosts/{id}/metrics?range=24h
	now := time.Now().Unix()
	_ = db.SaveMetricSamples(hostID, map[string]float64{"cpu": 45.0, "ram": 70.0}, now-60)

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/hosts/%d/metrics?range=24h", hostID), nil)
	req.AddCookie(authCookie)
	srv.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var metricResp struct {
		Ok         bool                 `json:"ok"`
		Enabled    bool                 `json:"enabled"`
		Timestamps []int64              `json:"timestamps"`
		Series     map[string][]float64 `json:"series"`
	}
	_ = json.NewDecoder(rec.Body).Decode(&metricResp)
	if !metricResp.Ok || !metricResp.Enabled || len(metricResp.Timestamps) == 0 {
		t.Fatalf("unexpected metric response: %+v", metricResp)
	}
	if len(metricResp.Series["cpu"]) == 0 || metricResp.Series["cpu"][0] != 45.0 {
		t.Errorf("unexpected cpu data in metric response: %v", metricResp.Series)
	}

	// 4.5. PUT /api/hosts/{id}/metric-probes/{probe_id}
	updateBody := `{"name": "Nginx Status Updated", "command": "curl -s http://127.0.0.1/nginx_status", "enabled": false}`
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPut, fmt.Sprintf("/api/hosts/%d/metric-probes/%d", hostID, probeID), bytes.NewBufferString(updateBody))
	req.AddCookie(authCookie)
	srv.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 on PUT, got %d", rec.Code)
	}
	var putResp struct {
		Ok    bool                  `json:"ok"`
		Probe store.HostMetricProbe `json:"probe"`
	}
	_ = json.NewDecoder(rec.Body).Decode(&putResp)
	if !putResp.Ok || putResp.Probe.Name != "Nginx Status Updated" || putResp.Probe.Enabled {
		t.Fatalf("unexpected PUT response: %+v", putResp)
	}

	// 5. DELETE /api/hosts/{id}/metric-probes/{probe_id}
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodDelete, fmt.Sprintf("/api/hosts/%d/metric-probes/%d", hostID, probeID), nil)
	req.AddCookie(authCookie)
	srv.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/hosts/%d/metric-probes", hostID), nil)
	req.AddCookie(authCookie)
	srv.mux.ServeHTTP(rec, req)
	_ = json.NewDecoder(rec.Body).Decode(&probeList)
	if len(probeList) != 0 {
		t.Errorf("expected 0 probes after delete, got %d", len(probeList))
	}
}

func TestHostMetricsEnabledTriStateAndCategoryFilter(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "pantau-web-tristate-*")
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
	token := "test_metrics_token_2"
	srv.sessions.Store(token, time.Now().Add(time.Hour))
	authCookie := &http.Cookie{Name: "pantau_session", Value: token}

	// 1. Create host via POST /api/hosts with metrics_enabled: 0 (Force OFF)
	createBody := `{"name": "Off Host", "host": "10.0.0.99", "port": 22, "user": "root", "metrics_enabled": 0}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/hosts", bytes.NewBufferString(createBody))
	req.AddCookie(authCookie)
	srv.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	var createdHost store.Host
	if err := json.NewDecoder(rec.Body).Decode(&createdHost); err != nil {
		t.Fatalf("failed decoding created host: %v", err)
	}
	if createdHost.MetricsEnabled != 0 {
		t.Errorf("expected MetricsEnabled 0 (Force OFF), got %d", createdHost.MetricsEnabled)
	}
	dbHost, err := db.GetHost(createdHost.ID)
	if err != nil || dbHost.MetricsEnabled != 0 {
		t.Errorf("expected DB MetricsEnabled 0, got %v (err: %v)", dbHost, err)
	}

	// 2. Test category=applications filter
	now := time.Now().Unix()
	_ = db.SaveMetricSamples(createdHost.ID, map[string]float64{
		"cpu":          55.0,
		"ram":          80.0,
		"nginx_active": 12.0,
	}, now-30)

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/hosts/%d/metrics?category=applications", createdHost.ID), nil)
	req.AddCookie(authCookie)
	srv.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var appMetricsResp struct {
		Series map[string][]float64 `json:"series"`
	}
	_ = json.NewDecoder(rec.Body).Decode(&appMetricsResp)
	if _, hasCPU := appMetricsResp.Series["cpu"]; hasCPU {
		t.Errorf("expected category=applications to exclude cpu, but got cpu in series: %v", appMetricsResp.Series)
	}
	if val, hasNginx := appMetricsResp.Series["nginx_active"]; !hasNginx || len(val) == 0 || val[0] != 12.0 {
		t.Errorf("expected category=applications to include nginx_active: 12.0, got: %v", appMetricsResp.Series)
	}
}

func TestUIProbeEditAndEmptyAppsStateRegression(t *testing.T) {
	htmlBytes, err := os.ReadFile("static/index.html")
	if err != nil {
		t.Fatalf("failed reading index.html: %v", err)
	}
	html := string(htmlBytes)

	// Regression 1: editExistingProbe must pass probe ID instead of serialized JSON with unescaped single quotes
	if strings.Contains(html, `editExistingProbe('${encodeURIComponent(JSON.stringify(p))}')`) {
		t.Errorf("editExistingProbe still uses encodeURIComponent(JSON.stringify(p)) which breaks on single quotes")
	}
	if !strings.Contains(html, "editExistingProbe(${p.id})") {
		t.Errorf("expected editExistingProbe(${p.id}) in probe table")
	}

	// Regression 2: toggleProbeActive must pass probe ID
	if strings.Contains(html, `toggleProbeActive(${p.id}, ${p.enabled}, '${encodeURIComponent(JSON.stringify(p))}')`) {
		t.Errorf("toggleProbeActive still uses encodeURIComponent(JSON.stringify(p)) which breaks on single quotes")
	}

	// Regression 3: Applications category empty state must not get trapped in generic waiting hourglass
	if !strings.Contains(html, "if (currentMetricCategory === 'applications')") {
		t.Errorf("expected dedicated currentMetricCategory === 'applications' check for empty samples")
	}
	if !strings.Contains(html, "No Application Metrics Recorded") {
		t.Errorf("expected 'No Application Metrics Recorded' in index.html")
	}

	// Regression 4: Application metrics must be grouped per application/domain instead of monolithic chart
	if !strings.Contains(html, "Application Metrics - Grouped per Application") {
		t.Errorf("expected application metrics to be grouped per application")
	}

	// Regression 5: Y-axis ticks must be compact-formatted (k, M, B, T) and legend formatted with locale
	if !strings.Contains(html, "function formatAxisTicks") {
		t.Errorf("expected formatAxisTicks for compact SI suffix numbers on Y-axis")
	}
	if !strings.Contains(html, "function formatLegendValue") {
		t.Errorf("expected formatLegendValue for readable comma-separated legend values")
	}
}


