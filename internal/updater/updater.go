package updater

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	defaultCheckInterval = 12 * time.Hour
	defaultCacheTTL      = 6 * time.Hour
	defaultCheckTimeout  = 5 * time.Second
)

// GitHubRelease represents the minimal structure returned by GitHub's release API.
type GitHubRelease struct {
	TagName     string        `json:"tag_name"`
	Body        string        `json:"body"`
	HTMLURL     string        `json:"html_url"`
	PublishedAt string        `json:"published_at"`
	Assets      []GitHubAsset `json:"assets"`
}

// GitHubAsset represents a single downloadable asset in a GitHub release.
type GitHubAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
	Size               int64  `json:"size"`
}

// CheckResult represents the outcome of checking for updates.
type CheckResult struct {
	Available      bool      `json:"available"`
	CurrentVersion string    `json:"current_version"`
	LatestVersion  string    `json:"latest_version"`
	ReleaseURL     string    `json:"release_url"`
	ReleaseNotes   string    `json:"release_notes"`
	PublishedAt    string    `json:"published_at"`
	IsDocker       bool      `json:"is_docker"`
	CheckedAt      time.Time `json:"checked_at"`
	Error          string    `json:"error,omitempty"`
}

// Manager manages update checking, verification, and self-update execution.
type Manager struct {
	repoOwner      string
	repoName       string
	currentVersion string
	apiURL         string // ponytail: configurable for unit tests without hitting github
	disabled       bool
	client         *http.Client
	shutdownFn     func()

	mu          sync.RWMutex
	lastCheck   time.Time
	cachedCheck *CheckResult
}

// NewManager creates a new update Manager instance.
func NewManager(currentVersion string, disabled bool) *Manager {
	return &Manager{
		repoOwner:      "herliansyah",
		repoName:       "pantau",
		currentVersion: currentVersion,
		disabled:       disabled,
		client: &http.Client{
			Timeout: 60 * time.Second,
		},
	}
}

// SetAPIURL overrides the GitHub API endpoint (primarily for testing).
func (m *Manager) SetAPIURL(url string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.apiURL = url
}

// SetShutdownFunc registers a callback invoked before server restarts.
func (m *Manager) SetShutdownFunc(fn func()) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.shutdownFn = fn
}

// IsDocker detects if Pantau is executing within a Docker container.
func IsDocker() bool {
	if _, err := os.Stat("/.dockerenv"); err == nil {
		return true
	}
	data, err := os.ReadFile("/proc/1/cgroup")
	if err == nil && (strings.Contains(string(data), "docker") || strings.Contains(string(data), "containerd")) {
		return true
	}
	return false
}

// CleanOldArtifacts cleans any leftover .old binary files from previous updates.
func CleanOldArtifacts() {
	execPath, err := os.Executable()
	if err != nil {
		return
	}
	execPath, err = filepath.EvalSymlinks(execPath)
	if err != nil {
		return
	}
	oldPath := execPath + ".old"
	if _, err := os.Stat(oldPath); err == nil {
		_ = os.Remove(oldPath)
	}
}

// StartBackgroundTicker runs a periodic update checker every 12 hours.
func (m *Manager) StartBackgroundTicker(ctx context.Context) {
	if m.disabled {
		return
	}
	// Initial check after a short delay so boot isn't impacted
	go func() {
		time.Sleep(3 * time.Second)
		_, _ = m.Check(context.Background(), false)
	}()

	ticker := time.NewTicker(defaultCheckInterval)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				_, _ = m.Check(context.Background(), false)
			}
		}
	}()
}

// Check inspects GitHub Releases for a newer version.
// If force is false, cached results within defaultCacheTTL are reused.
func (m *Manager) Check(ctx context.Context, force bool) (*CheckResult, error) {
	m.mu.RLock()
	if !force && m.cachedCheck != nil && time.Since(m.lastCheck) < defaultCacheTTL {
		res := *m.cachedCheck
		m.mu.RUnlock()
		return &res, nil
	}
	m.mu.RUnlock()

	res := &CheckResult{
		CurrentVersion: m.currentVersion,
		IsDocker:       IsDocker(),
		CheckedAt:      time.Now(),
	}

	if m.disabled {
		res.Error = "Update check is disabled"
		m.storeCache(res)
		return res, nil
	}

	apiURL := m.apiURL
	if apiURL == "" {
		apiURL = fmt.Sprintf("https://api.github.com/repos/%s/%s/releases/latest", m.repoOwner, m.repoName)
	}

	reqCtx, cancel := context.WithTimeout(ctx, defaultCheckTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, apiURL, nil)
	if err != nil {
		res.Error = err.Error()
		m.storeCache(res)
		return res, err
	}
	req.Header.Set("Accept", "application/vnd.github.v3+json")
	req.Header.Set("User-Agent", "Pantau-Updater/"+m.currentVersion)

	resp, err := m.client.Do(req)
	if err != nil {
		// Fail-silent in airgapped environments: record error quietly
		res.Error = fmt.Sprintf("Unable to connect to release upstream: %v", err)
		m.storeCache(res)
		return res, nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		res.Error = fmt.Sprintf("GitHub API returned status: %s", resp.Status)
		m.storeCache(res)
		return res, nil
	}

	var rel GitHubRelease
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		res.Error = fmt.Sprintf("Failed to parse release metadata: %v", err)
		m.storeCache(res)
		return res, nil
	}

	res.LatestVersion = rel.TagName
	res.ReleaseURL = rel.HTMLURL
	res.ReleaseNotes = rel.Body
	res.PublishedAt = rel.PublishedAt
	res.Available = CompareVersions(rel.TagName, m.currentVersion) > 0

	m.storeCache(res)
	return res, nil
}

func (m *Manager) storeCache(res *CheckResult) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cachedCheck = res
	m.lastCheck = time.Now()
}

// CompareVersions compares two semver tags (e.g. "v1.2.3" vs "v1.2.0").
// Returns:
//   1 if v1 > v2
//  -1 if v1 < v2
//   0 if v1 == v2
func CompareVersions(v1, v2 string) int {
	v1Clean := strings.TrimPrefix(strings.TrimSpace(v1), "v")
	v2Clean := strings.TrimPrefix(strings.TrimSpace(v2), "v")

	// If either is "dev", treat tagged release as newer
	if v2Clean == "dev" || v2Clean == "" {
		if v1Clean != "" && v1Clean != "dev" {
			return 1
		}
		return 0
	}
	if v1Clean == "dev" || v1Clean == "" {
		return -1
	}

	p1 := strings.Split(v1Clean, ".")
	p2 := strings.Split(v2Clean, ".")

	maxLen := len(p1)
	if len(p2) > maxLen {
		maxLen = len(p2)
	}

	for i := 0; i < maxLen; i++ {
		var n1, n2 int
		if i < len(p1) {
			// strip pre-release suffixes if any
			s := strings.Split(p1[i], "-")[0]
			n1, _ = strconv.Atoi(s)
		}
		if i < len(p2) {
			s := strings.Split(p2[i], "-")[0]
			n2, _ = strconv.Atoi(s)
		}
		if n1 > n2 {
			return 1
		}
		if n1 < n2 {
			return -1
		}
	}
	return 0
}

// ApplyUpdate executes the self-update process:
// 1. Rejects if in Docker container.
// 2. Downloads candidate release archive and checksums.txt.
// 3. Verifies SHA-256 hash against manifest.
// 4. Extracts candidate binary to adjacent temporary file.
// 5. Pre-flight smoke tests the candidate binary (`pantau.tmp -v`).
// 6. Atomically replaces current executable.
func (m *Manager) ApplyUpdate(ctx context.Context) error {
	if IsDocker() {
		return errors.New("self-update is disabled in Docker container; pull the new container image instead")
	}

	apiURL := m.apiURL
	if apiURL == "" {
		apiURL = fmt.Sprintf("https://api.github.com/repos/%s/%s/releases/latest", m.repoOwner, m.repoName)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return fmt.Errorf("failed to prepare request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github.v3+json")
	req.Header.Set("User-Agent", "Pantau-Updater/"+m.currentVersion)

	resp, err := m.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to query latest release: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status code from release API: %s", resp.Status)
	}

	var rel GitHubRelease
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return fmt.Errorf("failed to decode release payload: %w", err)
	}

	targetOS := runtime.GOOS
	targetArch := runtime.GOARCH

	// Expected archive naming: pantau-<tag>-<os>-<arch>.(tar.gz|zip)
	var expectedExt string
	if targetOS == "windows" {
		expectedExt = ".zip"
	} else {
		expectedExt = ".tar.gz"
	}

	var assetURL string
	var assetName string
	var checksumsURL string

	for _, a := range rel.Assets {
		if a.Name == "checksums.txt" || a.Name == "sha256sums.txt" {
			checksumsURL = a.BrowserDownloadURL
		}
		if strings.Contains(a.Name, targetOS) && strings.Contains(a.Name, targetArch) && strings.HasSuffix(a.Name, expectedExt) {
			assetURL = a.BrowserDownloadURL
			assetName = a.Name
		}
	}

	if assetURL == "" {
		return fmt.Errorf("no compatible release asset found for %s/%s with %s", targetOS, targetArch, expectedExt)
	}
	if checksumsURL == "" {
		return errors.New("release manifest checksums.txt not found in upstream release")
	}

	// 1. Download checksums.txt
	checksumsData, err := m.downloadBytes(ctx, checksumsURL)
	if err != nil {
		return fmt.Errorf("failed to download checksums: %w", err)
	}
	expectedHash, err := parseChecksum(checksumsData, assetName)
	if err != nil {
		return fmt.Errorf("failed to parse checksum for %s: %w", assetName, err)
	}

	// 2. Download release asset
	archiveData, err := m.downloadBytes(ctx, assetURL)
	if err != nil {
		return fmt.Errorf("failed to download asset %s: %w", assetName, err)
	}

	// 3. Verify SHA-256
	h := sha256.Sum256(archiveData)
	computedHash := hex.EncodeToString(h[:])
	if !strings.EqualFold(computedHash, expectedHash) {
		return fmt.Errorf("checksum verification failed for %s (expected %s, got %s)", assetName, expectedHash, computedHash)
	}

	// 4. Extract executable from archive
	binaryName := "pantau"
	if targetOS == "windows" {
		binaryName = "pantau.exe"
	}

	binaryBytes, err := extractBinary(archiveData, assetName, binaryName)
	if err != nil {
		return fmt.Errorf("failed to extract executable from archive: %w", err)
	}

	// 5. Write to temporary file adjacent to current executable (same filesystem to allow atomic rename)
	execPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to locate current executable path: %w", err)
	}
	execPath, err = filepath.EvalSymlinks(execPath)
	if err != nil {
		return fmt.Errorf("failed to resolve symlinks for executable: %w", err)
	}

	execDir := filepath.Dir(execPath)
	tempFilePath := filepath.Join(execDir, fmt.Sprintf(".pantau-candidate-%d.tmp", time.Now().UnixNano()))
	defer func() {
		// Clean candidate temp file if it still exists
		_ = os.Remove(tempFilePath)
	}()

	if err := os.WriteFile(tempFilePath, binaryBytes, 0755); err != nil {
		return fmt.Errorf("failed to write candidate binary (check write permissions): %w", err)
	}

	// 6. Pre-flight smoke test: run `candidate -v`
	testCtx, testCancel := context.WithTimeout(ctx, 5*time.Second)
	defer testCancel()

	cmd := exec.CommandContext(testCtx, tempFilePath, "-v")
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("pre-flight smoke test of new binary failed: %w", err)
	}

	// 7. Atomic Swap
	if targetOS == "windows" {
		oldPath := execPath + ".old"
		_ = os.Remove(oldPath) // remove old artifact if exists
		if err := os.Rename(execPath, oldPath); err != nil {
			return fmt.Errorf("failed to rename running executable on Windows: %w", err)
		}
		if err := os.Rename(tempFilePath, execPath); err != nil {
			// rollback
			_ = os.Rename(oldPath, execPath)
			return fmt.Errorf("failed to install new executable on Windows: %w", err)
		}
	} else {
		if err := os.Rename(tempFilePath, execPath); err != nil {
			return fmt.Errorf("failed to replace executable: %w", err)
		}
	}

	return nil
}

func (m *Manager) downloadBytes(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Pantau-Updater/"+m.currentVersion)

	resp, err := m.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %s for %s", resp.Status, url)
	}

	return io.ReadAll(resp.Body)
}

func parseChecksum(data []byte, filename string) (string, error) {
	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) >= 2 {
			hash := fields[0]
			fn := filepath.Base(fields[1])
			if fn == filename || fn == "*"+filename {
				return hash, nil
			}
		}
	}
	return "", fmt.Errorf("no checksum entry found for %s", filename)
}

func extractBinary(archiveData []byte, assetName, binaryName string) ([]byte, error) {
	if strings.HasSuffix(assetName, ".zip") {
		r, err := zip.NewReader(bytes.NewReader(archiveData), int64(len(archiveData)))
		if err != nil {
			return nil, err
		}
		for _, f := range r.File {
			if filepath.Base(f.Name) == binaryName {
				rc, err := f.Open()
				if err != nil {
					return nil, err
				}
				defer rc.Close()
				return io.ReadAll(rc)
			}
		}
		return nil, fmt.Errorf("file %s not found in zip archive", binaryName)
	}

	// Assume tar.gz
	gr, err := gzip.NewReader(bytes.NewReader(archiveData))
	if err != nil {
		return nil, err
	}
	defer gr.Close()

	tr := tar.NewReader(gr)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		if filepath.Base(hdr.Name) == binaryName {
			return io.ReadAll(tr)
		}
	}
	return nil, fmt.Errorf("file %s not found in tar.gz archive", binaryName)
}

// Restart executes a graceful handover to the new binary.
func (m *Manager) Restart() error {
	m.mu.RLock()
	shutdownFn := m.shutdownFn
	m.mu.RUnlock()

	execPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to find executable: %w", err)
	}
	execPath, err = filepath.EvalSymlinks(execPath)
	if err != nil {
		return fmt.Errorf("failed to evaluate symlinks: %w", err)
	}

	cmd := exec.Command(execPath, os.Args[1:]...)
	cmd.Dir, _ = os.Getwd()
	cmd.Env = os.Environ()
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start new process: %w", err)
	}

	if shutdownFn != nil {
		go func() {
			time.Sleep(100 * time.Millisecond)
			shutdownFn()
			os.Exit(0)
		}()
	} else {
		go func() {
			time.Sleep(100 * time.Millisecond)
			os.Exit(0)
		}()
	}

	return nil
}
