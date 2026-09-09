package snapshot

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/pbkdf2"
	"pantau/internal/store"
)

var (
	MagicHeader   = []byte("PANTAU_ENC_V1\n")
	ErrCorrupt    = errors.New("invalid or corrupted snapshot file")
	ErrPassphrase = errors.New("invalid snapshot passphrase or corrupted file")
)

const (
	saltLen  = 16
	nonceLen = 12
	kdfIter  = 100_000
	keyLen   = 32
)

// Encrypt serializes the payload to JSON and encrypts it with AES-256-GCM.
func Encrypt(payload *store.SnapshotPayload, passphrase string) ([]byte, error) {
	if strings.TrimSpace(passphrase) == "" {
		return nil, errors.New("passphrase cannot be empty")
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal payload: %w", err)
	}

	salt := make([]byte, saltLen)
	if _, err := io.ReadFull(rand.Reader, salt); err != nil {
		return nil, fmt.Errorf("generate salt: %w", err)
	}

	key := pbkdf2.Key([]byte(passphrase), salt, kdfIter, keyLen, sha256.New)
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("new cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("new gcm: %w", err)
	}

	nonce := make([]byte, nonceLen)
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("generate nonce: %w", err)
	}

	sealed := gcm.Seal(nil, nonce, raw, nil)

	buf := bytes.NewBuffer(make([]byte, 0, len(MagicHeader)+saltLen+nonceLen+len(sealed)))
	buf.Write(MagicHeader)
	buf.Write(salt)
	buf.Write(nonce)
	buf.Write(sealed)

	return buf.Bytes(), nil
}

// Decrypt verifies and decrypts a snapshot file, returning the parsed payload.
func Decrypt(data []byte, passphrase string) (*store.SnapshotPayload, error) {
	if strings.TrimSpace(passphrase) == "" {
		return nil, errors.New("passphrase cannot be empty")
	}
	if len(data) < len(MagicHeader)+saltLen+nonceLen {
		return nil, ErrCorrupt
	}
	if !bytes.HasPrefix(data, MagicHeader) {
		return nil, ErrCorrupt
	}

	offset := len(MagicHeader)
	salt := data[offset : offset+saltLen]
	offset += saltLen
	nonce := data[offset : offset+nonceLen]
	offset += nonceLen
	ciphertext := data[offset:]

	key := pbkdf2.Key([]byte(passphrase), salt, kdfIter, keyLen, sha256.New)
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("new cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("new gcm: %w", err)
	}

	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, ErrPassphrase
	}

	var payload store.SnapshotPayload
	if err := json.Unmarshal(plaintext, &payload); err != nil {
		return nil, fmt.Errorf("unmarshal decrypted payload: %w", err)
	}

	return &payload, nil
}

// GitHubClient handles communication with the GitHub REST API.
type GitHubClient struct {
	client  *http.Client
	BaseURL string
}

func NewGitHubClient(client *http.Client) *GitHubClient {
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	return &GitHubClient{client: client}
}

func (g *GitHubClient) baseURL() string {
	if g.BaseURL != "" {
		return strings.TrimSuffix(g.BaseURL, "/")
	}
	return "https://api.github.com"
}

func CleanRepo(repo string) string {
	r := strings.TrimSpace(repo)
	r = strings.TrimPrefix(r, "https://github.com/")
	r = strings.TrimPrefix(r, "http://github.com/")
	r = strings.TrimSuffix(r, ".git")
	return strings.Trim(r, "/")
}

// Pull downloads the content of the file from GitHub.
func (g *GitHubClient) Pull(ctx context.Context, repo, token, branch, path string) ([]byte, string, error) {
	repo = CleanRepo(repo)
	if path == "" {
		path = "pantau-state.enc"
	}
	apiURL := fmt.Sprintf("%s/repos/%s/contents/%s", g.baseURL(), repo, path)
	if branch != "" {
		apiURL += "?ref=" + branch
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("Accept", "application/vnd.github.v3+json")
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(token))
	resp, err := g.client.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("github api get: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, "", fmt.Errorf("file %s not found in repo %s", path, repo)
	}
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return nil, "", fmt.Errorf("github api error (%d): %s", resp.StatusCode, string(b))
	}

	var fileResp struct {
		SHA     string `json:"sha"`
		Content string `json:"content"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&fileResp); err != nil {
		return nil, "", fmt.Errorf("decode github response: %w", err)
	}

	cleanedContent := strings.ReplaceAll(fileResp.Content, "\n", "")
	cleanedContent = strings.ReplaceAll(cleanedContent, "\r", "")
	data, err := base64.StdEncoding.DecodeString(cleanedContent)
	if err != nil {
		return nil, "", fmt.Errorf("decode base64 content: %w", err)
	}

	return data, fileResp.SHA, nil
}

// Push uploads or updates the file in the GitHub repository.
func (g *GitHubClient) Push(ctx context.Context, repo, token, branch, path string, data []byte, commitMsg string) error {
	repo = CleanRepo(repo)
	if path == "" {
		path = "pantau-state.enc"
	}
	if commitMsg == "" {
		commitMsg = fmt.Sprintf("backup: pantau state snapshot (%s)", time.Now().UTC().Format(time.RFC3339))
	}
	_, existingSHA, _ := g.Pull(ctx, repo, token, branch, path)
	return g.pushWithSHA(ctx, repo, token, branch, path, data, commitMsg, existingSHA, true)
}

func (g *GitHubClient) pushWithSHA(ctx context.Context, repo, token, branch, path string, data []byte, commitMsg, sha string, retryOnConflict bool) error {
	apiURL := fmt.Sprintf("%s/repos/%s/contents/%s", g.baseURL(), repo, path)
	encoded := base64.StdEncoding.EncodeToString(data)
	payload := map[string]interface{}{
		"message": commitMsg,
		"content": encoded,
	}
	if sha != "" {
		payload["sha"] = sha
	}
	if branch != "" {
		payload["branch"] = branch
	}

	reqBody, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPut, apiURL, bytes.NewReader(reqBody))
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/vnd.github.v3+json")
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(token))
	req.Header.Set("Content-Type", "application/json")

	resp, err := g.client.Do(req)
	if err != nil {
		return fmt.Errorf("github api put: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusConflict && retryOnConflict {
		// SHA mismatch, pull latest sha and retry once
		_, latestSHA, err := g.Pull(ctx, repo, token, branch, path)
		if err == nil && latestSHA != "" {
			return g.pushWithSHA(ctx, repo, token, branch, path, data, commitMsg, latestSHA, false)
		}
	}

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("github api put error (%d): %s", resp.StatusCode, string(b))
	}

	return nil
}

// Manager orchestrates automated backups and on-demand sync operations.
type Manager struct {
	db     *store.DB
	gh     *GitHubClient
	mu     sync.Mutex
}

func NewManager(db *store.DB, ghClient *GitHubClient) *Manager {
	if ghClient == nil {
		ghClient = NewGitHubClient(nil)
	}
	return &Manager{
		db: db,
		gh: ghClient,
	}
}

// Start runs the periodic background backup check.
func (m *Manager) Start(ctx context.Context) {
	ticker := time.NewTicker(2 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.checkAndBackup(ctx)
		}
	}
}

func (m *Manager) checkAndBackup(ctx context.Context) {
	enabled, err := m.db.GetSetting("github_backup_enabled")
	if err != nil || enabled != "1" {
		return
	}

	repo, _ := m.db.GetSetting("github_repo")
	token, _ := m.db.GetSetting("github_token")
	passphrase, _ := m.db.GetSetting("snapshot_passphrase")
	if repo == "" || token == "" || passphrase == "" {
		return
	}

	intervalHoursStr, _ := m.db.GetSetting("backup_interval_hours")
	intervalHours, _ := strconv.Atoi(intervalHoursStr)
	if intervalHours <= 0 {
		intervalHours = 24
	}

	lastBackupStr, _ := m.db.GetSetting("last_backup_time")
	if lastBackupStr != "" {
		lastBackup, err := time.Parse(time.RFC3339, lastBackupStr)
		if err == nil {
			if time.Since(lastBackup) < time.Duration(intervalHours)*time.Hour {
				return
			}
		}
	}

	log.Printf("[Snapshot] Running scheduled automated backup to GitHub (%s)...", CleanRepo(repo))
	if err := m.SyncNow(ctx); err != nil {
		log.Printf("[Snapshot] Scheduled backup failed: %v", err)
	} else {
		log.Printf("[Snapshot] Scheduled backup completed successfully.")
	}
}

// SyncNow executes an immediate export, encryption, and push to GitHub.
func (m *Manager) SyncNow(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	repo, _ := m.db.GetSetting("github_repo")
	token, _ := m.db.GetSetting("github_token")
	branch, _ := m.db.GetSetting("github_branch")
	path, _ := m.db.GetSetting("github_file_path")
	passphrase, _ := m.db.GetSetting("snapshot_passphrase")

	if repo == "" || token == "" || passphrase == "" {
		return errors.New("github repository, token, and snapshot passphrase must be configured")
	}
	if path == "" {
		path = "pantau-state.enc"
	}

	payload, err := m.db.ExportSnapshot()
	if err != nil {
		m.recordStatus("export_error: " + err.Error())
		return fmt.Errorf("export snapshot: %w", err)
	}

	encrypted, err := Encrypt(payload, passphrase)
	if err != nil {
		m.recordStatus("encrypt_error: " + err.Error())
		return fmt.Errorf("encrypt snapshot: %w", err)
	}

	commitMsg := fmt.Sprintf("backup: pantau state snapshot (%s)", time.Now().UTC().Format(time.RFC3339))
	if err := m.gh.Push(ctx, repo, token, branch, path, encrypted, commitMsg); err != nil {
		m.recordStatus("github_push_error: " + err.Error())
		return fmt.Errorf("push to github: %w", err)
	}

	nowStr := time.Now().UTC().Format(time.RFC3339)
	_ = m.db.SetSetting("last_backup_time", nowStr)
	_ = m.db.SetSetting("last_backup_status", "ok")
	return nil
}

func (m *Manager) recordStatus(status string) {
	_ = m.db.SetSetting("last_backup_status", status)
}

// RestoreFromGitHub pulls the snapshot from GitHub, decrypts it, and imports it into the database.
func (m *Manager) RestoreFromGitHub(ctx context.Context, repo, token, branch, path, passphrase string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if repo == "" || token == "" || passphrase == "" {
		return errors.New("github repository, token, and passphrase are required")
	}
	if path == "" {
		path = "pantau-state.enc"
	}

	data, _, err := m.gh.Pull(ctx, repo, token, branch, path)
	if err != nil {
		return fmt.Errorf("pull from github: %w", err)
	}

	payload, err := Decrypt(data, passphrase)
	if err != nil {
		return fmt.Errorf("decrypt snapshot: %w", err)
	}

	if err := m.db.ImportSnapshot(payload); err != nil {
		return fmt.Errorf("import snapshot: %w", err)
	}

	// Update local github backup settings to match credentials used to restore
	_ = m.db.SetSetting("github_backup_enabled", "1")
	_ = m.db.SetSetting("github_repo", CleanRepo(repo))
	_ = m.db.SetSetting("github_token", token)
	if branch != "" {
		_ = m.db.SetSetting("github_branch", branch)
	}
	_ = m.db.SetSetting("github_file_path", path)
	_ = m.db.SetSetting("snapshot_passphrase", passphrase)
	_ = m.db.SetSetting("last_backup_status", "restored from github")

	return nil
}
