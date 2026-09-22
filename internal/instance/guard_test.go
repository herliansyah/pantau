package instance

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInstanceGuard(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test.db")

	// 1. First acquire should succeed
	g1, meta1, err := Acquire(dbPath, 8080)
	if err != nil {
		t.Fatalf("expected acquire success, got %v", err)
	}
	if meta1 != nil {
		t.Fatalf("expected nil metadata for first acquire")
	}

	// 2. Second acquire on same dbPath must fail with ErrAlreadyRunning
	g2, meta2, err := Acquire(dbPath, 8081)
	if err != ErrAlreadyRunning {
		t.Fatalf("expected ErrAlreadyRunning, got %v", err)
	}
	if g2 != nil {
		t.Fatalf("expected nil guard for second acquire")
	}
	if meta2 == nil {
		t.Fatalf("expected metadata for second acquire")
	}
	if meta2.PID != os.Getpid() {
		t.Errorf("expected PID %d, got %d", os.Getpid(), meta2.PID)
	}
	if meta2.Port != 8080 {
		t.Errorf("expected Port 8080, got %d", meta2.Port)
	}

	// 3. Update port
	if err := g1.UpdatePort(8085); err != nil {
		t.Fatalf("failed to update port: %v", err)
	}

	_, meta3, err := Acquire(dbPath, 8081)
	if err != ErrAlreadyRunning {
		t.Fatalf("expected ErrAlreadyRunning, got %v", err)
	}
	if meta3.Port != 8085 {
		t.Errorf("expected updated Port 8085, got %d", meta3.Port)
	}

	// 4. Acquire on different dbPath should succeed independently
	otherDB := filepath.Join(tempDir, "other.db")
	gOther, _, err := Acquire(otherDB, 9000)
	if err != nil {
		t.Fatalf("expected acquire on other DB to succeed, got %v", err)
	}
	_ = gOther.Close()

	// 5. Close first guard and verify subsequent acquire succeeds
	if err := g1.Close(); err != nil {
		t.Fatalf("failed to close guard: %v", err)
	}

	g3, _, err := Acquire(dbPath, 8080)
	if err != nil {
		t.Fatalf("expected acquire to succeed after first guard closed, got %v", err)
	}
	_ = g3.Close()
}
