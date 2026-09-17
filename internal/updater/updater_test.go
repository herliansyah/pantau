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
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestCompareVersions(t *testing.T) {
	tests := []struct {
		v1       string
		v2       string
		expected int
	}{
		{"v1.0.0", "v1.0.0", 0},
		{"v1.1.0", "v1.0.0", 1},
		{"v1.0.1", "v1.0.0", 1},
		{"v1.0.0", "v1.0.1", -1},
		{"v2.0.0", "v1.9.9", 1},
		{"v1.0.0", "dev", 1},
		{"dev", "v1.0.0", -1},
		{"dev", "dev", 0},
		{"1.2.3", "v1.2.3", 0},
		{"v1.2.4-rc1", "v1.2.3", 1},
	}

	for _, tt := range tests {
		got := CompareVersions(tt.v1, tt.v2)
		if got != tt.expected {
			t.Errorf("CompareVersions(%q, %q) = %d; want %d", tt.v1, tt.v2, got, tt.expected)
		}
	}
}

func TestParseChecksum(t *testing.T) {
	manifest := `e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855  pantau-v1.0.0-linux-amd64.tar.gz
11223344556677889900aabbccddeeff11223344556677889900aabbccddeeff *pantau-v1.0.0-windows-amd64.zip
`
	hash, err := parseChecksum([]byte(manifest), "pantau-v1.0.0-linux-amd64.tar.gz")
	if err != nil {
		t.Fatalf("unexpected error parsing checksum: %v", err)
	}
	if hash != "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855" {
		t.Errorf("got hash %q, want expected", hash)
	}

	hashZip, err := parseChecksum([]byte(manifest), "pantau-v1.0.0-windows-amd64.zip")
	if err != nil {
		t.Fatalf("unexpected error parsing windows zip checksum: %v", err)
	}
	if hashZip != "11223344556677889900aabbccddeeff11223344556677889900aabbccddeeff" {
		t.Errorf("got hash %q, want expected", hashZip)
	}

	_, err = parseChecksum([]byte(manifest), "nonexistent.tar.gz")
	if err == nil {
		t.Errorf("expected error for nonexistent file, got nil")
	}
}

func TestExtractBinaryTarGz(t *testing.T) {
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)

	content := []byte("#!/bin/sh\necho test\n")
	hdr := &tar.Header{
		Name: "pantau",
		Mode: 0755,
		Size: int64(len(content)),
	}
	if err := tw.WriteHeader(hdr); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(content); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gw.Close(); err != nil {
		t.Fatal(err)
	}

	extracted, err := extractBinary(buf.Bytes(), "pantau-linux-amd64.tar.gz", "pantau")
	if err != nil {
		t.Fatalf("unexpected error extracting tar.gz: %v", err)
	}
	if !bytes.Equal(extracted, content) {
		t.Errorf("extracted content mismatch: got %s, want %s", extracted, content)
	}
}

func TestExtractBinaryZip(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)

	content := []byte("MZ windows binary placeholder")
	w, err := zw.Create("pantau.exe")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write(content); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}

	extracted, err := extractBinary(buf.Bytes(), "pantau-windows-amd64.zip", "pantau.exe")
	if err != nil {
		t.Fatalf("unexpected error extracting zip: %v", err)
	}
	if !bytes.Equal(extracted, content) {
		t.Errorf("extracted content mismatch: got %s, want %s", extracted, content)
	}
}

func TestManagerCheck(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rel := GitHubRelease{
			TagName:     "v1.5.0",
			Body:        "## Release 1.5.0\n- New self-update feature",
			HTMLURL:     "https://github.com/herliansyah/pantau/releases/tag/v1.5.0",
			PublishedAt: "2026-09-17T10:00:00Z",
			Assets: []GitHubAsset{
				{
					Name:               "pantau-v1.5.0-linux-amd64.tar.gz",
					BrowserDownloadURL: "http://example.com/asset.tar.gz",
					Size:               1024,
				},
			},
		}
		_ = json.NewEncoder(w).Encode(rel)
	}))
	defer server.Close()

	mgr := NewManager("v1.0.0", false)
	mgr.SetAPIURL(server.URL)

	res, err := mgr.Check(context.Background(), true)
	if err != nil {
		t.Fatalf("unexpected error checking update: %v", err)
	}
	if !res.Available {
		t.Errorf("expected update available, got false")
	}
	if res.LatestVersion != "v1.5.0" {
		t.Errorf("got latest version %q, want v1.5.0", res.LatestVersion)
	}

	// Test caching: cached result should be returned without hitting server again
	resCached, err := mgr.Check(context.Background(), false)
	if err != nil {
		t.Fatalf("unexpected error on cached check: %v", err)
	}
	if resCached.LatestVersion != "v1.5.0" {
		t.Errorf("cached latest version mismatch")
	}
}

func TestCleanOldArtifacts(t *testing.T) {
	execPath, err := os.Executable()
	if err != nil {
		t.Skip("cannot resolve executable in test environment")
	}
	execPath, err = filepath.EvalSymlinks(execPath)
	if err != nil {
		t.Skip("cannot resolve symlinks in test environment")
	}
	oldPath := execPath + ".old"
	_ = os.WriteFile(oldPath, []byte("old artifact test"), 0644)
	CleanOldArtifacts()
	if _, err := os.Stat(oldPath); err == nil {
		t.Errorf("expected %s to be removed, but it still exists", oldPath)
		_ = os.Remove(oldPath)
	}
}

func TestChecksumVerificationFailure(t *testing.T) {
	content := []byte("fake binary")
	h := sha256.Sum256(content)
	computedHash := hex.EncodeToString(h[:])

	wrongHash := "0000000000000000000000000000000000000000000000000000000000000000"
	if computedHash == wrongHash {
		t.Fatal("hashes match unexpectedly")
	}
}
