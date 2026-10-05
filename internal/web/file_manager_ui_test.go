package web

import (
	"regexp"
	"strings"
	"testing"
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

func TestSponsorshipUI(t *testing.T) {
	html := string(embeddedHTML)

	// Check Saweria link presence in footer and settings modal
	saweriaURL := "https://saweria.co/herliansyah26"
	if !strings.Contains(html, saweriaURL) {
		t.Errorf("expected Saweria link %q to be present in index.html", saweriaURL)
	}

	// Verify footer sponsor pill
	if !strings.Contains(html, "footer-pill-sponsor") {
		t.Errorf("expected footer-pill-sponsor class to be defined and present in index.html")
	}

	// Verify i18n keys exist in both EN and ID dictionaries
	sponsorKeys := []string{
		`"footer_sponsor":`,
		`"footer_sponsor_title":`,
		`"settings_sponsor_title":`,
		`"settings_sponsor_desc":`,
		`"btn_sponsor":`,
	}
	for _, key := range sponsorKeys {
		cnt := strings.Count(html, key)
		if cnt < 2 {
			t.Errorf("expected i18n key %q to exist in at least 2 dictionaries, found %d", key, cnt)
		}
	}
}

