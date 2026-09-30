package web

import (
	"regexp"
	"strings"
	"testing"
)

func TestFileManagerActionButtonsGrid(t *testing.T) {
	html := string(embeddedHTML)

	// Check action-grid CSS rule
	// The action row in file manager has 5 elements:
	// 1. Terminal button (icon)
	// 2. Edit button (text or placeholder)
	// 3. Download button (icon)
	// 4. Copy to host button (text)
	// 5. Delete button (icon)
	gridRegex := regexp.MustCompile(`\.action-grid\s*\{([^}]+)\}`)
	match := gridRegex.FindStringSubmatch(html)
	if len(match) < 2 {
		t.Fatalf(".action-grid CSS rule not found in index.html")
	}
	cssBody := match[1]

	// Must define 5 columns in grid-template-columns so the 5 action buttons don't wrap to row 2
	colRegex := regexp.MustCompile(`grid-template-columns:\s*([^;]+);`)
	colMatch := colRegex.FindStringSubmatch(cssBody)
	if len(colMatch) < 2 {
		t.Fatalf("grid-template-columns not found in .action-grid CSS: %s", cssBody)
	}

	cols := strings.Fields(strings.TrimSpace(colMatch[1]))
	if len(cols) != 5 {
		t.Errorf("expected .action-grid to have exactly 5 columns for the 5 action buttons (terminal, edit, download, copy, delete), but found %d columns: %v", len(cols), cols)
	}

	// First column is Terminal icon (should be ~34-38px, not 65px)
	if len(cols) >= 1 && cols[0] != "38px" && cols[0] != "34px" && cols[0] != "36px" {
		t.Errorf("column 1 is terminal icon, expected ~34-38px, got %s", cols[0])
	}

	// Second column is Edit button (text ~65px)
	if len(cols) >= 2 && cols[1] != "65px" && cols[1] != "60px" && cols[1] != "70px" {
		t.Errorf("column 2 is edit button, expected ~65px, got %s", cols[1])
	}

	// Third column is Download icon button (should be ~34-38px, not auto)
	if len(cols) >= 3 && cols[2] != "38px" && cols[2] != "34px" && cols[2] != "36px" {
		t.Errorf("column 3 is download icon, expected ~34-38px, got %s", cols[2])
	}

	// Fourth column is Copy to Host (text button, should be auto or minmax or ~120px)
	if len(cols) >= 4 && cols[3] != "auto" && cols[3] != "max-content" && !strings.Contains(cols[3], "px") {
		t.Errorf("column 4 is copy to host button, expected auto or max-content, got %s", cols[3])
	}

	// Fifth column is Delete button (icon ~34-38px)
	if len(cols) >= 5 && cols[4] != "38px" && cols[4] != "34px" && cols[4] != "36px" {
		t.Errorf("column 5 is delete icon, expected ~34-38px, got %s", cols[4])
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
