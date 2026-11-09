package web

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/pkg/sftp"

	"pantau/internal/inspector"
	"pantau/internal/sshrunner"
	"pantau/internal/store"
)

func TestFileManagerActionButtonsGrid(t *testing.T) {
	html := string(embeddedHTML)

	// Check action-grid CSS rule
	// The action row in file manager has 6 elements:
	// 1. Terminal button (icon)
	// 2. Edit button (text or placeholder)
	// 3. Duplicate button (icon)
	// 4. Download button (icon)
	// 5. Copy to host button (text)
	// 6. Delete button (icon)
	gridRegex := regexp.MustCompile(`\.action-grid\s*\{([^}]+)\}`)
	match := gridRegex.FindStringSubmatch(html)
	if len(match) < 2 {
		t.Fatalf(".action-grid CSS rule not found in index.html")
	}
	cssBody := match[1]

	// Must define 6 columns in grid-template-columns so the 6 action buttons don't wrap to row 2
	colRegex := regexp.MustCompile(`grid-template-columns:\s*([^;]+);`)
	colMatch := colRegex.FindStringSubmatch(cssBody)
	if len(colMatch) < 2 {
		t.Fatalf("grid-template-columns not found in .action-grid CSS: %s", cssBody)
	}

	cols := strings.Fields(strings.TrimSpace(colMatch[1]))
	if len(cols) != 6 {
		t.Errorf("expected .action-grid to have exactly 6 columns for the 6 action buttons (terminal, edit, duplicate, download, copy, delete), but found %d columns: %v", len(cols), cols)
	}

	// First column is Terminal icon (should be ~34-38px, not 65px)
	if len(cols) >= 1 && cols[0] != "38px" && cols[0] != "34px" && cols[0] != "36px" {
		t.Errorf("column 1 is terminal icon, expected ~34-38px, got %s", cols[0])
	}

	// Second column is Edit button (text ~65px)
	if len(cols) >= 2 && cols[1] != "65px" && cols[1] != "60px" && cols[1] != "70px" {
		t.Errorf("column 2 is edit button, expected ~65px, got %s", cols[1])
	}

	// Third column is Duplicate icon button (should be ~34-38px)
	if len(cols) >= 3 && cols[2] != "38px" && cols[2] != "34px" && cols[2] != "36px" {
		t.Errorf("column 3 is duplicate icon, expected ~34-38px, got %s", cols[2])
	}

	// Fourth column is Download icon button (should be ~34-38px, not auto)
	if len(cols) >= 4 && cols[3] != "38px" && cols[3] != "34px" && cols[3] != "36px" {
		t.Errorf("column 4 is download icon, expected ~34-38px, got %s", cols[3])
	}

	// Fifth column is Copy to Host (text button, should be auto or minmax or ~120px)
	if len(cols) >= 5 && cols[4] != "auto" && cols[4] != "max-content" && !strings.Contains(cols[4], "px") {
		t.Errorf("column 5 is copy to host button, expected auto or max-content, got %s", cols[4])
	}

	// Sixth column is Delete button (icon ~34-38px)
	if len(cols) >= 6 && cols[5] != "38px" && cols[5] != "34px" && cols[5] != "36px" {
		t.Errorf("column 6 is delete icon, expected ~34-38px, got %s", cols[5])
	}
}

func TestFileManagerDuplicateAndEditorMaximizeUI(t *testing.T) {
	html := string(embeddedHTML)

	// Check Duplicate Modal and functions
	duplicateElements := []string{
		"duplicateModal",
		"duplicateSourcePath",
		"duplicateItemSize",
		"duplicateDiskAvail",
		"duplicateWarningBadge",
		"duplicateErrorBadge",
		"duplicateNameInput",
		"btnConfirmDuplicate",
		"openDuplicateModal",
		"executeDuplicate",
		"generateDuplicateName",
	}
	for _, el := range duplicateElements {
		if !strings.Contains(html, el) {
			t.Errorf("expected index.html to contain duplicate element %q", el)
		}
	}

	// Check Breadcrumbs & UI polish elements
	breadcrumbElements := []string{
		"file-breadcrumb-bar",
		"file-breadcrumb-item",
		"file-breadcrumb-separator",
		"renderBreadcrumbs",
		"getFileIcon",
		"files-table-container",
	}
	for _, el := range breadcrumbElements {
		if !strings.Contains(html, el) {
			t.Errorf("expected index.html to contain breadcrumb/polish element %q", el)
		}
	}

	// Check Editor Modal Maximize and VS Code Dark theme
	editorElements := []string{
		"btnEditorMax",
		"toggleEditorMaximize",
		"applyEditorMaximizeState",
		"cm-s-vscode-dark",
		"vscode-dark",
		"getEditorModeForPath",
	}
	for _, el := range editorElements {
		if !strings.Contains(html, el) {
			t.Errorf("expected index.html to contain editor element %q", el)
		}
	}

	// Check Duplicate i18n keys exist in both EN and ID dictionaries
	dupI18nKeys := []string{
		`"duplicate":`,
		`"duplicate_modal_title":`,
		`"duplicate_source_label":`,
		`"duplicate_name_label":`,
		`"duplicate_size_label":`,
		`"duplicate_avail_label":`,
		`"duplicate_btn":`,
		`"duplicating":`,
		`"duplicate_success":`,
		`"duplicate_conflict":`,
		`"duplicate_large_warn":`,
		`"duplicate_disk_insufficient":`,
	}
	for _, key := range dupI18nKeys {
		cnt := strings.Count(html, key)
		if cnt < 2 {
			t.Errorf("expected i18n key %q to exist in at least 2 dictionaries, found %d", key, cnt)
		}
	}
}

func TestFileManagerDownloadButton(t *testing.T) {
	html := string(embeddedHTML)

	// Check that a.btn or .btn has text-decoration: none to prevent underline glitch on link buttons
	if !strings.Contains(html, "a.btn") && !strings.Contains(html, ".btn {") {
		t.Fatalf(".btn styling not found")
	}
	if !strings.Contains(html, "text-decoration: none") {
		t.Errorf("expected text-decoration: none for button links")
	}

	// Download button should have consistent title i18n
	if strings.Contains(html, `title="Download Folder Archive"`) {
		t.Errorf("download folder button title should use i18n t(...) instead of hardcoded English")
	}
	if strings.Contains(html, `title="Download File"`) {
		t.Errorf("download file button title should use i18n t(...) instead of hardcoded English")
	}
}

func TestFileManagerPinsAndGlobalBookmarksUI(t *testing.T) {
	html := string(embeddedHTML)

	// Check elements
	requiredElements := []string{
		"btnHeaderBookmarks",
		"globalPinsModal",
		"globalPinsSearchInput",
		"globalPinsList",
		"filePinModal",
		"pinModalPath",
		"pinModalNote",
		"btnPinModalUnpin",
		"file-quick-access",
		"file-pin-chip",
		"btn-file-pin",
		"file-pin-badge",
		"openGlobalPinsModal",
		"loadGlobalPins",
		"onGlobalPinsSearch",
		"renderGlobalPinsList",
		"deleteGlobalPin",
		"jumpToHostFile",
		"openPinModal",
		"submitSavePin",
		"submitUnpin",
		"loadHostFilePins",
		"renderQuickAccessBar",
		"openTerminalFromGlobalPin",
		"openNativeTerminalFromGlobalPin",
	}

	for _, el := range requiredElements {
		if !strings.Contains(html, el) {
			t.Errorf("expected index.html to contain file pin/bookmark element %q", el)
		}
	}

	// Check i18n keys exist in at least 2 dictionaries (EN and ID)
	i18nKeys := []string{
		`"global_pins_title":`,
		`"global_pins_heading":`,
		`"quick_access":`,
		`"pin_file_modal_title":`,
		`"btn_save_pin":`,
		`"btn_unpin":`,
		`"pin_saved":`,
		`"pin_removed":`,
		`"confirm_unpin_item":`,
	}

	for _, k := range i18nKeys {
		cnt := strings.Count(html, k)
		if cnt < 2 {
			t.Errorf("expected i18n key %q to exist in at least 2 dictionaries, found %d", k, cnt)
		}
	}
}

func TestFileManagerUpButtonPosition(t *testing.T) {
	html := string(embeddedHTML)
	upIdx := strings.Index(html, `onclick="loadFiles('${escapeHtml(parent)}')"` )
	if upIdx == -1 {
		upIdx = strings.Index(html, `onclick="loadFiles('${parent}')"`)
	}
	breadIdx := strings.Index(html, `class="file-breadcrumb-bar"`)
	if upIdx == -1 || breadIdx == -1 {
		t.Fatalf("up button or breadcrumb bar not found in index.html")
	}
	if upIdx > breadIdx {
		t.Errorf("expected Up button (index %d) to appear before file-breadcrumb-bar (index %d)", upIdx, breadIdx)
	}
}

func TestFileManagerCheckboxAndBatchDeleteUI(t *testing.T) {
	html := string(embeddedHTML)

	// Verify bulk action bar and selection elements exist
	requiredElements := []string{
		"fileBulkActionBar",
		"fileBulkCountLabel",
		"btnBulkDeleteFiles",
		"btnBulkDeleteLabel",
		"selectAllFilesCheckbox",
		"file-select-cb",
		"file-bulk-bar",
		"file-checkbox",
		"onFileCheckboxChange",
		"toggleSelectAllFiles",
		"clearFileSelection",
		"updateFileSelectionUI",
		"deleteSelectedFiles",
	}
	for _, el := range requiredElements {
		if !strings.Contains(html, el) {
			t.Errorf("expected index.html to contain file manager bulk selection element %q", el)
		}
	}

	// Verify that protected items render lock icon and NEVER render the selectable checkbox
	if !strings.Contains(html, `isItemProtected`) {
		t.Fatalf("expected index.html to check isItemProtected")
	}
	// Check that lock icon is shown when isItemProtected is true
	if !strings.Contains(html, `t('cannot_delete_protected')`) {
		t.Errorf("expected index.html to use cannot_delete_protected tooltip for protected items")
	}

	// Check that empty table handles 6 columns
	if !strings.Contains(html, `colspan="6"`) {
		t.Errorf("expected index.html to have colspan=\"6\" for empty file list")
	}

	// Verify i18n keys exist in at least 2 dictionaries (EN and ID)
	i18nKeys := []string{
		`"select_all":`,
		`"deselect_all":`,
		`"delete_selected":`,
		`"confirm_delete_multiple":`,
		`"selected_items_count":`,
		`"batch_delete_success":`,
		`"cannot_delete_protected":`,
	}
	for _, key := range i18nKeys {
		cnt := strings.Count(html, key)
		if cnt < 2 {
			t.Errorf("expected i18n key %q to exist in at least 2 dictionaries, found %d", key, cnt)
		}
	}
}

func TestBatchDeleteProtectionSafety(t *testing.T) {
	// Dangerous paths that must NEVER be allowed in batch delete
	dangerousPaths := []string{
		"/",
		".",
		"/bin",
		"/bin/sh",
		"/etc",
		"/etc/passwd",
		"/usr/lib",
		"/boot/vmlinuz",
		"/sys",
		"/proc",
		"C:",
		"C:/",
		"C:/Windows",
		"C:/Windows/System32",
		"C:/Program Files",
		"C:/ProgramData",
	}

	for _, p := range dangerousPaths {
		if !isProtectedPath(p, nil) {
			t.Errorf("critical safety check failed: expected %q to be protected from deletion", p)
		}
	}

	// Safe paths that are permissible
	safePaths := []string{
		"/tmp/app.log",
		"/home/user/downloads/archive.tar.gz",
		"/var/www/html/site/temp.txt",
		"C:/Users/Ian/Desktop/notes.txt",
		"D:/data/backup/old.sql",
	}

	for _, p := range safePaths {
		if isProtectedPath(p, nil) {
			t.Errorf("expected safe path %q to NOT be protected", p)
		}
	}
}

func TestBatchDeleteAPIEndpoint(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "pantau-web-batch-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	db, err := store.Open(filepath.Join(tmpDir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	mockRunner := sshrunner.NewMockRunner()
	mockRunner.SFTPProvider = func() (*sftp.Client, error) {
		sConn, cConn := net.Pipe()
		server, err := sftp.NewServer(sConn)
		if err != nil {
			return nil, err
		}
		go func() {
			_ = server.Serve()
		}()
		return sftp.NewClientPipe(cConn, cConn)
	}

	ins := inspector.New(db, func(h string, p int, u, k string) (sshrunner.Runner, error) {
		return mockRunner, nil
	}, nil)

	srv := NewServer(db, ins, nil)
	token := "test_batch_token"
	srv.sessions.Store(token, time.Now().Add(time.Hour))
	authCookie := &http.Cookie{Name: "pantau_session", Value: token}

	hostID, err := db.CreateHost(&store.Host{
		Name: "Batch Delete Host",
		Host: "127.0.0.1",
		Port: 22,
		User: "root",
	})
	if err != nil {
		t.Fatal(err)
	}

	// 1. Missing path and paths -> 400 Bad Request JSON
	reqEmpty, _ := json.Marshal(map[string]interface{}{})
	rec1 := httptest.NewRecorder()
	r1 := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/hosts/%d/files/delete", hostID), bytes.NewReader(reqEmpty))
	r1.AddCookie(authCookie)
	srv.mux.ServeHTTP(rec1, r1)
	if rec1.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for empty delete request, got %d", rec1.Code)
	}
	var res1 map[string]interface{}
	_ = json.Unmarshal(rec1.Body.Bytes(), &res1)
	if res1["status"] != "error" || res1["error"] == nil {
		t.Errorf("expected JSON error response for missing paths, got %v", rec1.Body.String())
	}

	// 2. Protected system path in batch -> 403 Forbidden JSON
	reqProtected, _ := json.Marshal(map[string]interface{}{
		"paths": []string{"/tmp/safe.txt", "/etc/passwd"},
	})
	rec2 := httptest.NewRecorder()
	r2 := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/hosts/%d/files/delete", hostID), bytes.NewReader(reqProtected))
	r2.AddCookie(authCookie)
	srv.mux.ServeHTTP(rec2, r2)
	if rec2.Code != http.StatusForbidden {
		t.Errorf("expected 403 for protected path in batch delete, got %d", rec2.Code)
	}
	var res2 map[string]interface{}
	_ = json.Unmarshal(rec2.Body.Bytes(), &res2)
	if res2["status"] != "error" || !strings.Contains(fmt.Sprint(res2["error"]), "protected path") {
		t.Errorf("expected JSON error response citing protected path, got %v", rec2.Body.String())
	}

	// 3. Successful batch delete of real files with deduplication
	fileA := filepath.Join(tmpDir, "file_a.txt")
	fileB := filepath.Join(tmpDir, "file_b.txt")
	_ = os.WriteFile(fileA, []byte("data a"), 0644)
	_ = os.WriteFile(fileB, []byte("data b"), 0644)

	reqBatch, _ := json.Marshal(map[string]interface{}{
		"paths": []string{fileA, fileB, fileA}, // fileA included twice to test deduplication
	})
	rec3 := httptest.NewRecorder()
	r3 := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/hosts/%d/files/delete", hostID), bytes.NewReader(reqBatch))
	r3.AddCookie(authCookie)
	srv.mux.ServeHTTP(rec3, r3)
	if rec3.Code != http.StatusOK {
		t.Errorf("expected 200 OK for batch delete, got %d: %s", rec3.Code, rec3.Body.String())
	}
	var res3 struct {
		Status       string   `json:"status"`
		DeletedCount int      `json:"deleted_count"`
		Errors       []string `json:"errors"`
	}
	_ = json.Unmarshal(rec3.Body.Bytes(), &res3)
	if res3.Status != "deleted" || res3.DeletedCount != 2 || len(res3.Errors) > 0 {
		t.Errorf("unexpected batch delete result: %+v", res3)
	}
	if _, err := os.Stat(fileA); !os.IsNotExist(err) {
		t.Errorf("expected fileA to be deleted")
	}
	if _, err := os.Stat(fileB); !os.IsNotExist(err) {
		t.Errorf("expected fileB to be deleted")
	}
}





