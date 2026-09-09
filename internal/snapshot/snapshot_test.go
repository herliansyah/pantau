package snapshot_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"pantau/internal/snapshot"
	"pantau/internal/store"
)

func TestSnapshotEncryptionDecryption(t *testing.T) {
	payload := &store.SnapshotPayload{
		Version:   1,
		CreatedAt: time.Now().UTC(),
		Settings: map[string]string{
			"telegram_token":   "tok123",
			"webhook_url":      "https://example.com/hook",
			"ssh_public_key":   "ssh-rsa AAAA...",
			"telegram_chat_id": "999888",
		},
		Hosts: []store.Host{
			{
				ID:   1,
				Name: "Web-01",
				Host: "192.168.1.10",
				Port: 22,
				User: "root",
			},
			{
				ID:   2,
				Name: "DB-01",
				Host: "192.168.1.20",
				Port: 2222,
				User: "ubuntu",
			},
		},
		DesiredRules: []store.DesiredRule{
			{
				ID:       10,
				HostID:   1,
				Kind:     "container",
				Target:   "nginx",
				Expected: "running",
				Enabled:  true,
			},
			{
				ID:       11,
				HostID:   2,
				Kind:     "service",
				Target:   "mysql",
				Expected: "active",
				Enabled:  true,
			},
		},
	}

	passphrase := "MySecretPassphrase123!"

	// Encrypt
	enc, err := snapshot.Encrypt(payload, passphrase)
	if err != nil {
		t.Fatalf("encrypt failed: %v", err)
	}
	if len(enc) == 0 {
		t.Fatalf("expected non-empty encrypted data")
	}

	// Decrypt with correct passphrase
	dec, err := snapshot.Decrypt(enc, passphrase)
	if err != nil {
		t.Fatalf("decrypt failed: %v", err)
	}

	if dec.Version != 1 {
		t.Fatalf("expected version 1, got %d", dec.Version)
	}
	if len(dec.Hosts) != 2 {
		t.Fatalf("expected 2 hosts, got %d", len(dec.Hosts))
	}
	if dec.Hosts[0].Name != "Web-01" || dec.Hosts[1].Host != "192.168.1.20" {
		t.Fatalf("hosts mismatch: %+v", dec.Hosts)
	}
	if len(dec.DesiredRules) != 2 {
		t.Fatalf("expected 2 rules, got %d", len(dec.DesiredRules))
	}
	if dec.Settings["telegram_token"] != "tok123" {
		t.Fatalf("settings mismatch: got %v", dec.Settings)
	}

	// Decrypt with wrong passphrase -> should fail
	_, err = snapshot.Decrypt(enc, "WrongPassphrase")
	if err == nil {
		t.Fatalf("expected error with wrong passphrase, got nil")
	}
	if err != snapshot.ErrPassphrase {
		t.Fatalf("expected ErrPassphrase, got %v", err)
	}

	// Corrupted payload -> should fail
	corrupted := append([]byte(nil), enc...)
	corrupted[10] ^= 0xFF
	_, err = snapshot.Decrypt(corrupted, passphrase)
	if err == nil {
		t.Fatalf("expected error on corrupted data, got nil")
	}
}

func TestDatabaseExportAndImportRoundtrip(t *testing.T) {
	// Source DB
	tmp1, _ := os.CreateTemp("", "pantau_src_*.db")
	tmp1Path := tmp1.Name()
	tmp1.Close()
	defer os.Remove(tmp1Path)

	db1, err := store.Open(tmp1Path)
	if err != nil {
		t.Fatalf("open db1: %v", err)
	}
	defer db1.Close()

	h1ID, err := db1.CreateHost(&store.Host{
		Name: "App-Server",
		Host: "10.0.0.5",
		Port: 22,
		User: "deploy",
	})
	if err != nil {
		t.Fatalf("create host: %v", err)
	}

	err = db1.SaveDesiredRule(&store.DesiredRule{
		HostID:   h1ID,
		Kind:     "disk",
		Target:   "/",
		Expected: "<80%",
		Enabled:  true,
	})
	if err != nil {
		t.Fatalf("save rule: %v", err)
	}

	_ = db1.SetSetting("telegram_chat_id", "445566")

	// Export from DB1
	payload, err := db1.ExportSnapshot()
	if err != nil {
		t.Fatalf("export snapshot: %v", err)
	}

	// Encrypt and Decrypt
	pass := "TestingVaultPassword!"
	enc, err := snapshot.Encrypt(payload, pass)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	decryptedPayload, err := snapshot.Decrypt(enc, pass)
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}

	// Destination DB (New PC / Fresh DB)
	tmp2, _ := os.CreateTemp("", "pantau_dst_*.db")
	tmp2Path := tmp2.Name()
	tmp2.Close()
	defer os.Remove(tmp2Path)

	db2, err := store.Open(tmp2Path)
	if err != nil {
		t.Fatalf("open db2: %v", err)
	}
	defer db2.Close()

	// Initial fresh check
	isFresh, err := db2.IsFresh()
	if err != nil || !isFresh {
		t.Fatalf("expected db2 to be fresh initially, isFresh=%v, err=%v", isFresh, err)
	}

	// Import into DB2
	if err := db2.ImportSnapshot(decryptedPayload); err != nil {
		t.Fatalf("import snapshot to db2: %v", err)
	}

	// Verify DB2 is no longer fresh
	isFreshAfter, _ := db2.IsFresh()
	if isFreshAfter {
		t.Fatalf("expected db2 to NOT be fresh after import")
	}

	// Verify hosts in DB2
	hosts2, err := db2.ListHosts()
	if err != nil || len(hosts2) != 1 {
		t.Fatalf("expected 1 host in db2, got %d, err=%v", len(hosts2), err)
	}
	if hosts2[0].Name != "App-Server" || hosts2[0].Host != "10.0.0.5" {
		t.Fatalf("host mismatch in db2: %+v", hosts2[0])
	}

	// Verify desired rules in DB2
	rules2, err := db2.ListDesiredRules(hosts2[0].ID)
	if err != nil || len(rules2) != 1 {
		t.Fatalf("expected 1 rule in db2, got %d, err=%v", len(rules2), err)
	}
	if rules2[0].Target != "/" || rules2[0].Expected != "<80%" {
		t.Fatalf("rule mismatch in db2: %+v", rules2[0])
	}

	// Verify settings in DB2
	chatVal, err := db2.GetSetting("telegram_chat_id")
	if err != nil || chatVal != "445566" {
		t.Fatalf("expected setting telegram_chat_id 445566, got %s", chatVal)
	}
}

func TestGitHubMockSyncAndRestore(t *testing.T) {
	// Mock GitHub API Server
	var mu sync.Mutex
	var mockRemoteContent string
	mockSHA := "abcdef1234567890"

	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if !strings.HasPrefix(auth, "Bearer ") {
			http.Error(w, `{"message":"Bad credentials"}`, http.StatusUnauthorized)
			return
		}

		path := r.URL.Path
		if !strings.Contains(path, "/repos/myuser/pantau-backup/contents/pantau-state.enc") {
			http.Error(w, `{"message":"Not found"}`, http.StatusNotFound)
			return
		}

		mu.Lock()
		defer mu.Unlock()

		switch r.Method {
		case http.MethodGet:
			if mockRemoteContent == "" {
				http.Error(w, `{"message":"Not Found"}`, http.StatusNotFound)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]string{
				"sha":      mockSHA,
				"content":  mockRemoteContent,
				"encoding": "base64",
			})
		case http.MethodPut:
			var body struct {
				Message string `json:"message"`
				Content string `json:"content"`
				SHA     string `json:"sha"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				http.Error(w, `{"message":"bad request"}`, http.StatusBadRequest)
				return
			}
			mockRemoteContent = body.Content
			mockSHA = "updated_sha_" + time.Now().Format("150405")
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"content": map[string]string{"sha": mockSHA},
			})
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	}))
	defer mockServer.Close()

	ghClient := snapshot.NewGitHubClient(mockServer.Client())
	ghClient.BaseURL = mockServer.URL

	// Step 1: Create DB1 with host, configure backup to GitHub
	tmp1, _ := os.CreateTemp("", "pantau_gh_*.db")
	tmpPath1 := tmp1.Name()
	tmp1.Close()
	defer os.Remove(tmpPath1)

	db1, _ := store.Open(tmpPath1)
	defer db1.Close()

	_, _ = db1.CreateHost(&store.Host{
		Name: "Production-Cluster",
		Host: "cluster.internal",
		Port: 22,
		User: "admin",
	})
	_ = db1.SetSetting("github_backup_enabled", "1")
	_ = db1.SetSetting("github_repo", "myuser/pantau-backup")
	_ = db1.SetSetting("github_token", "ghp_dummy_token_12345")
	_ = db1.SetSetting("snapshot_passphrase", "SuperSecretPassphrase999")

	mgr1 := snapshot.NewManager(db1, ghClient)

	// Step 2: Trigger SyncNow to push to Mock GitHub
	err := mgr1.SyncNow(context.Background())
	if err != nil {
		t.Fatalf("SyncNow failed: %v", err)
	}

	mu.Lock()
	savedContent := mockRemoteContent
	mu.Unlock()
	if savedContent == "" {
		t.Fatalf("expected remote content to be uploaded to mock github")
	}

	// Verify last backup status is recorded as ok in DB1
	status, _ := db1.GetSetting("last_backup_status")
	if status != "ok" {
		t.Fatalf("expected last_backup_status 'ok', got '%s'", status)
	}

	// Step 3: Now simulate new PC / fresh DB2 restoring directly from GitHub!
	tmp2, _ := os.CreateTemp("", "pantau_gh_newpc_*.db")
	tmpPath2 := tmp2.Name()
	tmp2.Close()
	defer os.Remove(tmpPath2)

	db2, _ := store.Open(tmpPath2)
	defer db2.Close()

	isFresh, _ := db2.IsFresh()
	if !isFresh {
		t.Fatalf("expected db2 to be fresh")
	}

	mgr2 := snapshot.NewManager(db2, ghClient)
	err = mgr2.RestoreFromGitHub(context.Background(), "myuser/pantau-backup", "ghp_dummy_token_12345", "", "pantau-state.enc", "SuperSecretPassphrase999")
	if err != nil {
		t.Fatalf("RestoreFromGitHub failed: %v", err)
	}

	// Verify DB2 restored the host
	hosts2, err := db2.ListHosts()
	if err != nil || len(hosts2) != 1 {
		t.Fatalf("expected 1 host in restored db2, got %d", len(hosts2))
	}
	if hosts2[0].Name != "Production-Cluster" {
		t.Fatalf("host name mismatch in restored db2: %s", hosts2[0].Name)
	}

	// Verify DB2 is no longer fresh
	isFreshAfter, _ := db2.IsFresh()
	if isFreshAfter {
		t.Fatalf("expected restored db2 to not be fresh")
	}
}
