package inspector

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"pantau/internal/sshrunner"
	"pantau/internal/store"
)

func TestParseFree_ModernWithSwap(t *testing.T) {
	out := `               total        used        free      shared  buff/cache   available
Mem:     16573849600  4823449600  8923449600   523449600  2826950400 11226950400
Swap:     2147479552   536870912  1610608640`

	ramUsed, ramTot, swpUsed, swpTot := parseFree(out)
	if ramTot != 16573849600 {
		t.Fatalf("expected ramTot 16573849600, got %d", ramTot)
	}
	if ramUsed != 4823449600 {
		t.Fatalf("expected ramUsed 4823449600, got %d", ramUsed)
	}
	if swpTot != 2147479552 {
		t.Fatalf("expected swpTot 2147479552, got %d", swpTot)
	}
	if swpUsed != 536870912 {
		t.Fatalf("expected swpUsed 536870912, got %d", swpUsed)
	}
}

func TestParseFree_WithoutSwap(t *testing.T) {
	out := `Mem: 16777216000 8388608000 8388608000`
	ramUsed, ramTot, swpUsed, swpTot := parseFree(out)
	if ramTot != 16777216000 || ramUsed != 8388608000 {
		t.Fatalf("unexpected ram values: used=%d, tot=%d", ramUsed, ramTot)
	}
	if swpTot != 0 || swpUsed != 0 {
		t.Fatalf("expected 0 swap, got used=%d, tot=%d", swpUsed, swpTot)
	}
}

func TestParseFree_LegacyBuffersCache(t *testing.T) {
	out := `             total       used       free     shared    buffers     cached
Mem:       1019624     648160     371464          0      68280     265432
-/+ buffers/cache:     314448     705176
Swap:      2097144          0    2097144`

	ramUsed, ramTot, swpUsed, swpTot := parseFree(out)
	if ramTot != 1019624 {
		t.Fatalf("expected ramTot 1019624, got %d", ramTot)
	}
	if ramUsed != 314448 {
		t.Fatalf("expected ramUsed 314448, got %d", ramUsed)
	}
	if swpTot != 2097144 || swpUsed != 0 {
		t.Fatalf("expected swap: used=0, tot=2097144, got used=%d, tot=%d", swpUsed, swpTot)
	}
}

func TestEvaluateRule_DiskHungNFSTimeout(t *testing.T) {
	mockRunner := sshrunner.NewMockRunner()
	mockRunner.DefaultExec = func(cmd string) (string, string, int, error) {
		if strings.Contains(cmd, "df ") {
			return "", "", 124, nil // Exit code 124 represents timeout (e.g. hung NFS)
		}
		return "", "", 0, nil
	}

	ins := &Inspector{}
	host := &store.Host{ID: 1, Host: "10.0.0.1"}
	rule := &store.DesiredRule{
		Kind:     "disk",
		Target:   "/mnt/nfs_backup",
		Expected: "<80%",
	}

	current, status, _, rootCause := ins.evaluateRule(host, mockRunner, rule)
	if status != "drift" {
		t.Fatalf("expected status 'drift', got %q", status)
	}
	if current != "unresponsive" {
		t.Fatalf("expected current 'unresponsive', got %q", current)
	}
	if !strings.Contains(rootCause, "timed out (possible hung NFS") {
		t.Fatalf("expected hung NFS root cause, got %q", rootCause)
	}
}

func TestInspectHost_InFlightGuard(t *testing.T) {
	ins := &Inspector{}
	ins.inFlight.Store(int64(42), struct{}{})

	err := ins.InspectHost(42)
	if !errors.Is(err, ErrAlreadyInspecting) {
		t.Fatalf("expected ErrAlreadyInspecting, got %v", err)
	}
}

func TestInspectHost_TimeoutAbortsRunner(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "pantau-ins-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	db, err := store.Open(filepath.Join(tmpDir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	hostID, err := db.CreateHost(&store.Host{
		Name: "Test Server",
		Host: "1.2.3.4",
		Port: 22,
		User: "root",
	})
	if err != nil {
		t.Fatal(err)
	}

	mockRunner := sshrunner.NewMockRunner()
	// Simulate a hanging command that blocks until runner is closed
	mockRunner.DefaultExec = func(cmd string) (string, string, int, error) {
		for i := 0; i < 20; i++ {
			time.Sleep(10 * time.Millisecond)
			if mockRunner.Closed {
				return "", "", -1, errors.New("use of closed network connection")
			}
		}
		return "", "", 0, nil
	}

	factory := func(host string, port int, user, key string) (sshrunner.Runner, error) {
		return mockRunner, nil
	}

	ins := New(db, factory, nil)

	// Run inspection with tiny timeout of 30ms
	err = ins.InspectHostWithTimeout(hostID, 30*time.Millisecond)
	if err == nil {
		t.Fatal("expected inspection to fail with timeout error, got nil")
	}

	// Verify runner was closed
	if !mockRunner.Closed {
		t.Fatal("expected mockRunner.Closed to be true after timeout")
	}

	// Verify host status updated to down or degraded with timeout noted
	h, err := db.GetHost(hostID)
	if err != nil {
		t.Fatal(err)
	}
	if h.Status != "down" && h.Status != "degraded" {
		t.Fatalf("expected host status 'down' or 'degraded', got %q", h.Status)
	}
}
