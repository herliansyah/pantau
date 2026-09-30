package web

import (
	"strings"
	"testing"
)

func TestTerminalClipboardBridgeInIndexHTML(t *testing.T) {
	html := string(embeddedHTML)

	// 1. Must define clipboard helper with fallback for non-secure HTTP contexts
	if !strings.Contains(html, "copyTextToClipboard") {
		t.Errorf("expected index.html to contain copyTextToClipboard helper")
	}
	if !strings.Contains(html, "execCommand('copy')") && !strings.Contains(html, `execCommand("copy")`) {
		t.Errorf("expected index.html to have document.execCommand('copy') fallback for plain HTTP")
	}

	// 2. Must attach custom key event handler for context-aware Ctrl+C and Ctrl+V
	if !strings.Contains(html, "attachCustomKeyEventHandler") {
		t.Errorf("expected index.html to use term.attachCustomKeyEventHandler for terminal shortcuts")
	}
	if !strings.Contains(html, "hasSelection()") {
		t.Errorf("expected index.html to check term.hasSelection() for context-aware Ctrl+C")
	}

	// 3. Must handle Ctrl+Shift+C, Ctrl+V, and Ctrl+Shift+V
	if !strings.Contains(html, "handleTerminalPaste") {
		t.Errorf("expected index.html to have handleTerminalPaste function")
	}

	// 4. Must implement multiline paste safety check
	if !strings.Contains(html, "includes('\\n')") && !strings.Contains(html, `includes("\n")`) {
		t.Errorf("expected handleTerminalPaste to check for multiline newline characters")
	}
	if !strings.Contains(html, "confirm_multiline_paste") {
		t.Errorf("expected handleTerminalPaste to invoke confirmation dialog for multiline commands")
	}

	// 5. Must support copy-on-select
	if !strings.Contains(html, "onSelectionChange") {
		t.Errorf("expected index.html to support copy-on-select via onSelectionChange")
	}

	// 6. Must support right-click paste
	if !strings.Contains(html, "contextmenu") {
		t.Errorf("expected index.html to handle contextmenu / right-click paste on terminal")
	}

	// 7. Check i18n keys for multiline paste confirmation in both EN and ID
	requiredKeys := []string{
		`"multiline_paste_title":`,
		`"confirm_multiline_paste":`,
		`"paste_http_notice":`,
	}
	for _, k := range requiredKeys {
		if strings.Count(html, k) < 2 {
			t.Errorf("expected i18n key %q to exist in at least 2 dictionaries (EN and ID), found %d", k, strings.Count(html, k))
		}
	}
}

func TestCtrlVDoesNotSuppressNativePasteEvent(t *testing.T) {
	html := string(embeddedHTML)

	// Check that container paste listener uses capture phase (true)
	if !strings.Contains(html, "container.addEventListener('paste',") && !strings.Contains(html, `container.addEventListener("paste",`) {
		t.Fatalf("expected container paste listener in index.html")
	}
	if !strings.Contains(html, ", true);") && !strings.Contains(html, ", true)") {
		t.Errorf("expected container paste event listener to use capture phase (true) to intercept native paste before xterm raw handler")
	}

	// Check that Ctrl+V does NOT unconditionally call e.preventDefault()
	if strings.Contains(html, "// 2. Paste: Ctrl+V or Ctrl+Shift+V\n    if ((e.ctrlKey || e.metaKey) && (e.key === 'v' || e.key === 'V')) {\n      if (e.type === 'keydown') {\n        handleTerminalPaste(term, ws);\n      }\n      e.preventDefault();\n      return false;\n    }") {
		t.Errorf("bug detected: Ctrl+V unconditionally calls e.preventDefault(), suppressing native DOM paste event and failing on HTTP")
	}
}

func TestMultilinePasteSingleLineFlattening(t *testing.T) {
	html := string(embeddedHTML)

	// 1. Must define flattenToSingleLine function in index.html
	if !strings.Contains(html, "function flattenToSingleLine(") {
		t.Errorf("expected index.html to contain flattenToSingleLine helper function")
	}

	// 2. confirmModal must have extra button for secondary action (Single-Line Flatten)
	if !strings.Contains(html, `id="confirmModalExtraBtn"`) {
		t.Errorf("expected confirmModal in index.html to contain confirmModalExtraBtn")
	}

	// 3. handleTerminalPaste must offer extraBtnText and handle result === 'single'
	if !strings.Contains(html, "extraBtnText: t('paste_single_line'") && !strings.Contains(html, `extraBtnText: t("paste_single_line"`) {
		t.Errorf("expected handleTerminalPaste to specify extraBtnText for single line flatten")
	}
	if !strings.Contains(html, "flattenToSingleLine(") {
		t.Errorf("expected handleTerminalPaste to call flattenToSingleLine when single line option selected")
	}

	// 4. i18n keys for single line flatten and paste as is
	requiredKeys := []string{
		`"paste_single_line":`,
		`"paste_as_is":`,
	}
	for _, k := range requiredKeys {
		if strings.Count(html, k) < 2 {
			t.Errorf("expected i18n key %q to exist in at least 2 dictionaries (EN and ID), found %d", k, strings.Count(html, k))
		}
	}
}
