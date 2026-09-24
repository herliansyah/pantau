package store

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDualBucketAutoPruningAndFilteredRuns(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "pantau-store-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	db, err := Open(filepath.Join(tmpDir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	hostID, err := db.CreateHost(&Host{
		Name: "Server Alpha",
		Host: "192.168.1.10",
		Port: 22,
		User: "root",
	})
	if err != nil {
		t.Fatal(err)
	}

	// 1. Insert 105 OK runs
	for i := 1; i <= 105; i++ {
		err := db.RecordInspectionRun(&InspectionRun{
			HostID:     hostID,
			StartedAt:  time.Now().Add(time.Duration(i) * time.Minute),
			DurationMs: 150,
			Status:     "ok",
			Summary:    fmt.Sprintf("OK run #%d", i),
		})
		if err != nil {
			t.Fatalf("failed recording ok run: %v", err)
		}
	}

	// Verify only 100 OK runs are kept
	allRuns, err := db.ListInspectionRuns(hostID, 200, "all")
	if err != nil {
		t.Fatal(err)
	}
	if len(allRuns) != 100 {
		t.Fatalf("expected 100 ok runs after pruning, got %d", len(allRuns))
	}

	// 2. Insert 105 non-OK runs (drift / error)
	for i := 1; i <= 105; i++ {
		err := db.RecordInspectionRun(&InspectionRun{
			HostID:     hostID,
			StartedAt:  time.Now().Add(time.Duration(105+i) * time.Minute),
			DurationMs: 320,
			Status:     "drift",
			Summary:    fmt.Sprintf("Drift run #%d", i),
			Details:    "Service nginx inactive",
		})
		if err != nil {
			t.Fatalf("failed recording drift run: %v", err)
		}
	}

	// Verify dual bucket: 100 ok runs + 100 issue runs = 200 total in db
	issueRuns, err := db.ListInspectionRuns(hostID, 200, "issues")
	if err != nil {
		t.Fatal(err)
	}
	if len(issueRuns) != 100 {
		t.Fatalf("expected exactly 100 issue runs, got %d", len(issueRuns))
	}
	for _, r := range issueRuns {
		if r.Status == "ok" {
			t.Fatalf("expected non-ok status in issueRuns, got %s", r.Status)
		}
	}

	totalRuns, err := db.ListInspectionRuns(hostID, 300)
	if err != nil {
		t.Fatal(err)
	}
	if len(totalRuns) != 200 {
		t.Fatalf("expected 200 total runs (100 ok + 100 drift), got %d", len(totalRuns))
	}
}

func TestAlertAcknowledgmentOperations(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "pantau-alert-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	db, err := Open(filepath.Join(tmpDir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	h1, _ := db.CreateHost(&Host{Name: "Host 1", Host: "10.0.0.1", Port: 22, User: "root"})
	h2, _ := db.CreateHost(&Host{Name: "Host 2", Host: "10.0.0.2", Port: 22, User: "root"})

	// Create alerts for host 1 and host 2
	a1, _ := db.CreateAlert(&Alert{HostID: h1, Level: "warning", Message: "Disk high"})
	_, _ = db.CreateAlert(&Alert{HostID: h1, Level: "critical", Message: "Service down"})
	_, _ = db.CreateAlert(&Alert{HostID: h2, Level: "warning", Message: "CPU high"})

	active, err := db.ListActiveAlerts()
	if err != nil || len(active) != 3 {
		t.Fatalf("expected 3 active alerts, got %d (err: %v)", len(active), err)
	}

	// 1. Acknowledge single alert
	if err := db.AcknowledgeAlert(a1); err != nil {
		t.Fatal(err)
	}
	active, _ = db.ListActiveAlerts()
	if len(active) != 2 {
		t.Fatalf("expected 2 active alerts after single ack, got %d", len(active))
	}

	// 2. Acknowledge all alerts for host 1
	if err := db.AcknowledgeHostAlerts(h1); err != nil {
		t.Fatal(err)
	}
	active, _ = db.ListActiveAlerts()
	if len(active) != 1 || active[0].HostID != h2 {
		t.Fatalf("expected 1 active alert for host 2, got %d", len(active))
	}

	// 3. Acknowledge all alerts
	if err := db.AcknowledgeAllAlerts(); err != nil {
		t.Fatal(err)
	}
	active, _ = db.ListActiveAlerts()
	if len(active) != 0 {
		t.Fatalf("expected 0 active alerts after ack all, got %d", len(active))
	}
}
