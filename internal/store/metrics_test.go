package store

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestMetricsStoreAndProbes(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "pantau-metrics-store-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	db, err := Open(filepath.Join(tmpDir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	// 1. Test Host Creation & Tri-state metrics toggle
	hostID, err := db.CreateHost(&Host{
		Name:           "Test Host",
		Host:           "127.0.0.1",
		Port:           22,
		User:           "root",
		MetricsEnabled: -1, // inherit
	})
	if err != nil {
		t.Fatal(err)
	}

	h, err := db.GetHost(hostID)
	if err != nil || h == nil {
		t.Fatalf("failed getting host: %v", err)
	}
	if h.MetricsEnabled != -1 {
		t.Errorf("expected MetricsEnabled -1, got %d", h.MetricsEnabled)
	}

	// Global is default enabled
	if !db.IsHostMetricsEnabled(h) {
		t.Errorf("expected host metrics to be enabled by default")
	}

	// Disable globally
	_ = db.SetSetting("metrics_enabled", "false")
	if db.IsHostMetricsEnabled(h) {
		t.Errorf("expected host metrics to be disabled when global is false")
	}

	// Force host ON
	h.MetricsEnabled = 1
	_ = db.UpdateHost(h)
	hUpdated, _ := db.GetHost(hostID)
	if !db.IsHostMetricsEnabled(hUpdated) {
		t.Errorf("expected host metrics to be enabled when Force ON")
	}

	// Force host OFF
	h.MetricsEnabled = 0
	_ = db.SetSetting("metrics_enabled", "true")
	_ = db.UpdateHost(h)
	hUpdated, _ = db.GetHost(hostID)
	if db.IsHostMetricsEnabled(hUpdated) {
		t.Errorf("expected host metrics to be disabled when Force OFF")
	}

	// 2. Test Metric Samples Save & Query
	now := time.Now().Unix()
	samples1 := map[string]float64{
		"cpu": 25.5,
		"ram": 60.0,
	}
	if err := db.SaveMetricSamples(hostID, samples1, now-120); err != nil {
		t.Fatalf("failed saving samples1: %v", err)
	}

	samples2 := map[string]float64{
		"cpu": 35.5,
		"ram": 65.0,
	}
	if err := db.SaveMetricSamples(hostID, samples2, now-60); err != nil {
		t.Fatalf("failed saving samples2: %v", err)
	}

	// Raw query
	resp, err := db.GetMetricSamples(hostID, nil, now-300, now+10, 0)
	if err != nil {
		t.Fatalf("failed query metrics: %v", err)
	}
	if len(resp.Timestamps) != 2 {
		t.Fatalf("expected 2 timestamps, got %d", len(resp.Timestamps))
	}
	if len(resp.Series["cpu"]) != 2 || resp.Series["cpu"][0] != 25.5 || resp.Series["cpu"][1] != 35.5 {
		t.Errorf("unexpected cpu series: %v", resp.Series["cpu"])
	}

	// Bucketed query (step=300)
	respBucket, err := db.GetMetricSamples(hostID, []string{"cpu"}, now-300, now+10, 300)
	if err != nil {
		t.Fatalf("failed query bucketed metrics: %v", err)
	}
	if len(respBucket.Timestamps) == 0 {
		t.Errorf("expected bucketed timestamps, got 0")
	}

	// 3. Test Auto-Prune
	oldSampleTime := now - (10 * 86400) // 10 days ago
	_ = db.SaveMetricSamples(hostID, map[string]float64{"cpu": 50.0}, oldSampleTime)

	pruned, err := db.PruneMetricSamples(7)
	if err != nil {
		t.Fatalf("failed pruning: %v", err)
	}
	if pruned != 1 {
		t.Errorf("expected 1 row pruned, got %d", pruned)
	}

	// 4. Test HostMetricProbes CRUD
	probe := &HostMetricProbe{
		HostID:  hostID,
		Name:    "Nginx Status",
		Command: "curl -s http://127.0.0.1/nginx_status",
		Enabled: true,
	}
	pID, err := db.CreateHostMetricProbe(probe)
	if err != nil {
		t.Fatalf("failed creating probe: %v", err)
	}
	probe.ID = pID

	probes, err := db.GetHostMetricProbes(hostID)
	if err != nil || len(probes) != 1 {
		t.Fatalf("expected 1 probe, got %d (err: %v)", len(probes), err)
	}
	if probes[0].Name != "Nginx Status" || !probes[0].Enabled {
		t.Errorf("unexpected probe data: %+v", probes[0])
	}

	// Update
	probe.Name = "Nginx Live Status"
	probe.Enabled = false
	if err := db.UpdateHostMetricProbe(probe); err != nil {
		t.Fatalf("failed updating probe: %v", err)
	}

	probes, _ = db.GetHostMetricProbes(hostID)
	if probes[0].Name != "Nginx Live Status" || probes[0].Enabled {
		t.Errorf("unexpected updated probe: %+v", probes[0])
	}

	// Delete
	if err := db.DeleteHostMetricProbe(hostID, pID); err != nil {
		t.Fatalf("failed deleting probe: %v", err)
	}
	probes, _ = db.GetHostMetricProbes(hostID)
	if len(probes) != 0 {
		t.Errorf("expected 0 probes after delete, got %d", len(probes))
	}
}
