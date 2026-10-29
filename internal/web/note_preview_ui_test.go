package web

import (
	"strings"
	"testing"
)

func TestNotePreviewAndMarkdownUI(t *testing.T) {
	html := string(embeddedHTML)

	// 1. Modals must use modal-dialog-lg for spacious, comfortable reading/editing
	if !strings.Contains(html, `<div id="globalNotesModal" class="modal">`+"\n"+`  <div class="modal-content modal-dialog-lg">`) {
		t.Errorf("expected globalNotesModal to use modal-dialog-lg")
	}
	if !strings.Contains(html, `<div id="hostNoteModal" class="modal">`+"\n"+`  <div class="modal-content modal-dialog-lg">`) {
		t.Errorf("expected hostNoteModal to use modal-dialog-lg")
	}

	// 2. Note preview container styling class must exist
	if !strings.Contains(html, ".note-preview-content") || !strings.Contains(html, ".note-editor-textarea") {
		t.Errorf("expected CSS classes for .note-preview-content and .note-editor-textarea")
	}

	// 3. Both modals must have distinct view (preview) and edit sections
	requiredElements := []string{
		`id="globalNotesModalView"`,
		`id="globalNotesModalText" class="note-preview-content"`,
		`id="globalNotesModalEdit"`,
		`id="btnEditGlobalNotes"`,
		`id="hostNoteModalView"`,
		`id="hostNoteModalText" class="note-preview-content"`,
		`id="hostNoteModalEdit"`,
		`id="btnEditHostNote"`,
		`id="editNoteHostBadge"`,
	}
	for _, el := range requiredElements {
		if !strings.Contains(html, el) {
			t.Errorf("expected index.html to contain note UI element: %s", el)
		}
	}

	// 4. JavaScript functions for view/edit toggle, markdown rendering, and in-place preview
	requiredJS := []string{
		"function renderMarkdown(raw)",
		"function showHostNoteViewMode()",
		"function startEditHostNote()",
		"function cancelEditHostNote()",
		"function saveHostNote()",
		"function showGlobalNotesViewMode()",
		"function startEditGlobalNotesModal()",
		"function cancelEditGlobalNotesModal()",
		"function saveGlobalNotesModal()",
	}
	for _, fn := range requiredJS {
		if !strings.Contains(html, fn) {
			t.Errorf("expected index.html to contain function: %s", fn)
		}
	}

	// 5. In-place return to preview mode upon saving
	if !strings.Contains(html, "activeHostNoteData.notes = notes;\n      showHostNoteViewMode();") {
		t.Errorf("expected saveHostNote to return to preview mode via showHostNoteViewMode")
	}
	if !strings.Contains(html, "globalNotesData = notes;\n      showGlobalNotesViewMode();") {
		t.Errorf("expected saveGlobalNotesModal to return to preview mode via showGlobalNotesViewMode")
	}

	// 6. Translations in EN and ID
	requiredI18n := []string{
		`"host_note_empty": "No notes recorded for this host."`,
		`"host_note_empty": "Belum ada catatan untuk host ini."`,
		`"note_markdown_hint": "Supports Markdown: # header, **bold**, *italic*, ` + "`code`" + `, ` + "```block```" + `, lists, links"`,
		`"note_markdown_hint": "Mendukung Markdown: # heading, **tebal**, *miring*, ` + "`kode`" + `, ` + "```blok```" + `, daftar, tautan"`,
	}
	for _, i18n := range requiredI18n {
		if !strings.Contains(html, i18n) {
			t.Errorf("expected index.html to contain i18n key: %s", i18n)
		}
	}
}
