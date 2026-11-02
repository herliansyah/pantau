package store

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func setupTestDB(t *testing.T) *DB {
	t.Helper()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test_pantau.db")
	db, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestStatusPageTargetCRUD(t *testing.T) {
	db := setupTestDB(t)

	// Create a test host
	host := &Host{
		Name:   "Production Web 1",
		Host:   "192.168.1.100",
		Port:   2222,
		User:   "admin",
		Status: "healthy",
	}
	id, err := db.CreateHost(host)
	if err != nil {
		t.Fatalf("CreateHost failed: %v", err)
	}
	host.ID = id

	// Create a status target for host
	target := &StatusPageTarget{
		TargetType: "host",
		HostID:     host.ID,
		PublicName: "Web Application Portal",
		GroupName:  "Customer Frontends",
		SortOrder:  1,
		Enabled:    true,
	}
	if err := db.CreateStatusPageTarget(target); err != nil {
		t.Fatalf("CreateStatusPageTarget failed: %v", err)
	}
	if target.ID == 0 {
		t.Fatalf("expected non-zero ID")
	}

	// Retrieve by ID
	retrieved, err := db.GetStatusPageTargetByID(target.ID)
	if err != nil {
		t.Fatalf("GetStatusPageTargetByID failed: %v", err)
	}
	if retrieved.PublicName != "Web Application Portal" || retrieved.HostName != "Production Web 1" {
		t.Errorf("unexpected retrieved target: %+v", retrieved)
	}

	// Update target
	retrieved.PublicName = "Web Portal (Updated)"
	retrieved.Enabled = false
	if err := db.UpdateStatusPageTarget(retrieved); err != nil {
		t.Fatalf("UpdateStatusPageTarget failed: %v", err)
	}

	// List all
	list, err := db.GetStatusPageTargets()
	if err != nil {
		t.Fatalf("GetStatusPageTargets failed: %v", err)
	}
	if len(list) != 1 || list[0].PublicName != "Web Portal (Updated)" || list[0].Enabled {
		t.Errorf("unexpected list: %+v", list)
	}

	// Delete target
	if err := db.DeleteStatusPageTarget(target.ID); err != nil {
		t.Fatalf("DeleteStatusPageTarget failed: %v", err)
	}
	listAfter, _ := db.GetStatusPageTargets()
	if len(listAfter) != 0 {
		t.Errorf("expected empty list after deletion")
	}
}

func TestDailyRollupUPSERTAndPrune(t *testing.T) {
	db := setupTestDB(t)

	host := &Host{Name: "API Server", Host: "10.0.0.1", Port: 22, User: "root", Status: "healthy"}
	hostID, _ := db.CreateHost(host)
	_, _ = db.Exec(`UPDATE hosts SET status = 'healthy' WHERE id = ?`, hostID)

	target := &StatusPageTarget{
		TargetType: "host",
		HostID:     hostID,
		PublicName: "Core API",
		GroupName:  "APIs",
		Enabled:    true,
	}
	_ = db.CreateStatusPageTarget(target)

	today := time.Now().UTC().Format("2006-01-02")

	// 1st check: operational
	if err := db.RecordDailyRollup(target.ID, today, "operational"); err != nil {
		t.Fatalf("RecordDailyRollup 1 failed: %v", err)
	}

	// 2nd check: degraded
	if err := db.RecordDailyRollup(target.ID, today, "degraded"); err != nil {
		t.Fatalf("RecordDailyRollup 2 failed: %v", err)
	}

	// 3rd check: operational
	if err := db.RecordDailyRollup(target.ID, today, "operational"); err != nil {
		t.Fatalf("RecordDailyRollup 3 failed: %v", err)
	}

	rollups, err := db.GetDailyRollups(target.ID, 7)
	if err != nil {
		t.Fatalf("GetDailyRollups failed: %v", err)
	}
	if len(rollups) != 1 {
		t.Fatalf("expected 1 rollup row, got %d", len(rollups))
	}

	r := rollups[0]
	if r.TotalChecks != 3 || r.SuccessfulChecks != 2 || r.DegradedChecks != 1 || r.OutageChecks != 0 {
		t.Errorf("unexpected check counts: %+v", r)
	}
	// (2 + 0.5 * 1) / 3 * 100 = 83.33%
	if r.UptimePercentage < 83.0 || r.UptimePercentage > 84.0 {
		t.Errorf("expected uptime ~83.33%%, got %f", r.UptimePercentage)
	}

	// Test Prune
	pruned, err := db.PruneDailyRollups(90)
	if err != nil {
		t.Fatalf("PruneDailyRollups failed: %v", err)
	}
	if pruned != 0 {
		t.Errorf("expected 0 rows pruned for today's data, got %d", pruned)
	}
}

func TestStatusPageIncidents(t *testing.T) {
	db := setupTestDB(t)

	inc := &StatusPageIncident{
		Title:           "Elevated Latency",
		Status:          "investigating",
		Impact:          "minor",
		Message:         "Investigating network delay.",
		AffectedTargets: []int64{1, 2},
	}

	id, err := db.CreateStatusPageIncident(inc)
	if err != nil {
		t.Fatalf("CreateStatusPageIncident failed: %v", err)
	}
	if id == 0 {
		t.Fatalf("expected non-zero incident ID")
	}

	// Verify incident updates were automatically initialized with initial message
	updates, err := db.GetStatusPageIncidentUpdates(id)
	if err != nil {
		t.Fatalf("GetStatusPageIncidentUpdates failed: %v", err)
	}
	if len(updates) != 1 || updates[0].Status != "investigating" {
		t.Errorf("unexpected initial updates: %+v", updates)
	}

	// Post an update
	if err := db.AddStatusPageIncidentUpdate(id, "monitoring", "Traffic rerouted via secondary provider."); err != nil {
		t.Fatalf("AddStatusPageIncidentUpdate failed: %v", err)
	}

	// Resolve incident
	if err := db.AddStatusPageIncidentUpdate(id, "resolved", "All traffic latency returned to baseline."); err != nil {
		t.Fatalf("AddStatusPageIncidentUpdate resolved failed: %v", err)
	}

	incResolved, err := db.GetStatusPageIncidentByID(id)
	if err != nil {
		t.Fatalf("GetStatusPageIncidentByID failed: %v", err)
	}
	if incResolved.Status != "resolved" || incResolved.ResolvedAt == nil {
		t.Errorf("expected status resolved with timestamp, got %+v", incResolved)
	}

	// Active only list should be empty
	active, err := db.GetStatusPageIncidents(true)
	if err != nil {
		t.Fatalf("GetStatusPageIncidents failed: %v", err)
	}
	if len(active) != 0 {
		t.Errorf("expected 0 active incidents, got %d", len(active))
	}
}

func TestPublicStatusSummaryZeroDataLeakage(t *testing.T) {
	db := setupTestDB(t)

	// Set settings
	_ = db.SetSetting("status_page_title", "Acme Cloud Status")
	_ = db.SetSetting("status_page_header_text", "Public Uptime")

	// Host with private credentials
	host := &Host{
		Name:      "Internal Secret Server",
		Host:      "192.168.99.123", // PRIVATE IP
		Port:      22022,            // PRIVATE PORT
		User:      "root_secret",    // PRIVATE USER
		CustomKey: "SECRET_PRIVATE_KEY_DATA",
		Status:    "healthy",
	}
	hostID, _ := db.CreateHost(host)
	_, _ = db.Exec(`UPDATE hosts SET status = 'healthy' WHERE id = ?`, hostID)

	target := &StatusPageTarget{
		TargetType: "host",
		HostID:     hostID,
		PublicName: "Public Frontend Cloud",
		GroupName:  "Global Services",
		Enabled:    true,
	}
	_ = db.CreateStatusPageTarget(target)

	summary, err := db.GetPublicStatusSummary()
	if err != nil {
		t.Fatalf("GetPublicStatusSummary failed: %v", err)
	}

	if summary.Page.Title != "Acme Cloud Status" {
		t.Errorf("unexpected title: %s", summary.Page.Title)
	}
	if summary.Status.Indicator != "operational" {
		t.Errorf("unexpected indicator: %s", summary.Status.Indicator)
	}
	if len(summary.Targets) != 1 {
		t.Fatalf("expected 1 target, got %d", len(summary.Targets))
	}

	t0 := summary.Targets[0]
	if t0.Name != "Public Frontend Cloud" {
		t.Errorf("unexpected target name: %s", t0.Name)
	}
	if len(t0.DailyHistory) != 90 {
		t.Errorf("expected 90 days history, got %d", len(t0.DailyHistory))
	}

	// JSON Marshalling test to verify NO private fields exist
	bytes, err := summaryMarshal(summary)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}
	payloadStr := string(bytes)

	// Assertions that private data NEVER leaks
	leaks := []string{
		"192.168.99.123",
		"22022",
		"root_secret",
		"SECRET_PRIVATE_KEY_DATA",
		"Internal Secret Server",
	}
	for _, leak := range leaks {
		if strings.Contains(payloadStr, leak) {
			t.Fatalf("CRITICAL SECURITY LEAK: payload leaked private datum: %s", leak)
		}
	}
}

func summaryMarshal(s *PublicStatusSummary) ([]byte, error) {
	return json.Marshal(s)
}

func TestInitialOutageRollupPercentage(t *testing.T) {
	db := setupTestDB(t)

	host := &Host{Name: "Down Node", Host: "10.0.0.5", Port: 22, User: "root", Status: "down"}
	hostID, err := db.CreateHost(host)
	if err != nil {
		t.Fatalf("CreateHost failed: %v", err)
	}

	target := &StatusPageTarget{
		TargetType: "host",
		HostID:     hostID,
		PublicName: "Down Gateway",
		GroupName:  "Gateways",
		Enabled:    true,
	}
	if err := db.CreateStatusPageTarget(target); err != nil {
		t.Fatalf("CreateStatusPageTarget failed: %v", err)
	}

	today := time.Now().UTC().Format("2006-01-02")

	// 1st check of the day is an OUTAGE
	if err := db.RecordDailyRollup(target.ID, today, "outage"); err != nil {
		t.Fatalf("RecordDailyRollup failed: %v", err)
	}

	rollups, err := db.GetDailyRollups(target.ID, 1)
	if err != nil {
		t.Fatalf("GetDailyRollups failed: %v", err)
	}
	if len(rollups) != 1 {
		t.Fatalf("expected 1 rollup, got %d", len(rollups))
	}

	r := rollups[0]
	if r.TotalChecks != 1 || r.OutageChecks != 1 {
		t.Fatalf("unexpected check count: %+v", r)
	}
	if r.UptimePercentage != 0.0 {
		t.Fatalf("BUG: expected initial outage uptime to be 0.0%%, but got %f%%", r.UptimePercentage)
	}
}

func TestTodayNoRollupUptimeWhenDown(t *testing.T) {
	db := setupTestDB(t)

	host := &Host{Name: "Offline Host", Host: "10.0.0.6", Port: 22, User: "root", Status: "down"}
	hostID, err := db.CreateHost(host)
	if err != nil {
		t.Fatalf("CreateHost failed: %v", err)
	}
	_, _ = db.Exec(`UPDATE hosts SET status = 'down' WHERE id = ?`, hostID)

	target := &StatusPageTarget{
		TargetType: "host",
		HostID:     hostID,
		PublicName: "Offline Service",
		GroupName:  "Services",
		Enabled:    true,
	}
	if err := db.CreateStatusPageTarget(target); err != nil {
		t.Fatalf("CreateStatusPageTarget failed: %v", err)
	}

	// No rollups recorded today yet
	summary, err := db.GetPublicStatusSummary()
	if err != nil {
		t.Fatalf("GetPublicStatusSummary failed: %v", err)
	}
	if len(summary.Targets) != 1 {
		t.Fatalf("expected 1 target, got %d", len(summary.Targets))
	}

	t0 := summary.Targets[0]
	if t0.Status != "outage" {
		t.Fatalf("expected target status to be outage, got %s", t0.Status)
	}

	todayPoint := t0.DailyHistory[89]
	if todayPoint.Status != "outage" {
		t.Fatalf("expected today's history point to be outage, got %s", todayPoint.Status)
	}
	if todayPoint.Uptime != 0.0 {
		t.Fatalf("BUG: expected today's outage history point uptime to be 0.0%%, but got %f%%", todayPoint.Uptime)
	}
}
