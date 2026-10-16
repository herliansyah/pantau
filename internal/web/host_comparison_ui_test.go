package web

import (
	"strings"
	"testing"
)

func TestHostComparisonUIInIndexHTML(t *testing.T) {
	html := string(embeddedHTML)

	// 1. Verify header button btnHeaderCompare exists with proper icon and onclick handler
	if !strings.Contains(html, `id="btnHeaderCompare"`) {
		t.Errorf("expected btnHeaderCompare in index.html")
	}
	if !strings.Contains(html, `onclick="openHostCompareModal()"`) {
		t.Errorf("expected onclick=\"openHostCompareModal()\" in index.html")
	}
	if !strings.Contains(html, `data-i18n-title="compare_hosts_title"`) {
		t.Errorf("expected data-i18n-title=\"compare_hosts_title\" on btnHeaderCompare")
	}

	// 2. Verify modal structure and classes
	if !strings.Contains(html, `id="hostCompareModal"`) {
		t.Errorf("expected hostCompareModal in index.html")
	}
	if !strings.Contains(html, `class="modal-content modal-workspace"`) {
		t.Errorf("expected modal-workspace class on modal content")
	}
	if !strings.Contains(html, `id="compareHostSelectorBar"`) {
		t.Errorf("expected compareHostSelectorBar in index.html")
	}
	if !strings.Contains(html, `id="compareSelectedPills"`) {
		t.Errorf("expected compareSelectedPills in index.html")
	}
	if !strings.Contains(html, `id="compareAddHostSelect"`) {
		t.Errorf("expected compareAddHostSelect in index.html")
	}
	if !strings.Contains(html, `id="compareModalBody"`) {
		t.Errorf("expected compareModalBody in index.html")
	}
	if !strings.Contains(html, `id="btnCopyCompareMD"`) {
		t.Errorf("expected btnCopyCompareMD button in index.html")
	}

	// 3. Verify CSS rules
	cssRules := []string{
		".compare-host-selector-bar",
		".compare-pill",
		".compare-host-card",
		".badge-winner",
		".compare-matrix-table",
		".compare-category-header",
		".cell-winner",
		".cell-stale",
		".compare-takeaway-card",
	}
	for _, rule := range cssRules {
		if !strings.Contains(html, rule) {
			t.Errorf("expected CSS rule %q in index.html", rule)
		}
	}

	// 4. Verify Translations (EN & ID)
	enKeys := []string{
		`"compare_hosts_title": "Host Comparison"`,
		`"compare_badge_side_by_side": "Side-by-Side Evaluation"`,
		`"compare_btn_copy_md": "📋 Copy Summary (MD)"`,
		`"compare_overall_winner": "Overall Winner"`,
		`"compare_cat_compute": "⚡ Compute Capacity & Load"`,
		`"compare_cat_storage": "💾 Storage Capacity & I/O"`,
		`"compare_cat_network": "🌐 Network Health & Security"`,
		`"compare_cat_hygiene": "🛡️ System Hygiene & Lifecycle"`,
	}
	for _, k := range enKeys {
		if !strings.Contains(html, k) {
			t.Errorf("expected EN translation %q in index.html", k)
		}
	}

	idKeys := []string{
		`"compare_hosts_title": "Komparasi Host"`,
		`"compare_badge_side_by_side": "Evaluasi Berdampingan"`,
		`"compare_btn_copy_md": "📋 Salin Ringkasan (MD)"`,
		`"compare_overall_winner": "Juara Keseluruhan"`,
		`"compare_cat_compute": "⚡ Kapasitas & Beban Komputasi"`,
		`"compare_cat_storage": "💾 Kapasitas Penyimpanan & I/O"`,
		`"compare_cat_network": "🌐 Keandalan Jaringan & Keamanan"`,
		`"compare_cat_hygiene": "🛡️ Higienitas Sistem & Siklus Hidup"`,
	}
	for _, k := range idKeys {
		if !strings.Contains(html, k) {
			t.Errorf("expected ID translation %q in index.html", k)
		}
	}

	// 5. Verify JavaScript Functions
	jsFunctions := []string{
		"function openHostCompareModal",
		"function closeHostCompareModal",
		"function onAddCompareHost",
		"function removeCompareHost",
		"function resetCompareHosts",
		"function calculateHostComparison",
		"function generateDecisionNarrative",
		"async function copyComparisonMarkdown",
		"function renderHostCompareView",
	}
	for _, fn := range jsFunctions {
		if !strings.Contains(html, fn) {
			t.Errorf("expected JS function %q in index.html", fn)
		}
	}

	// 6. Verify modal is not improperly nested inside another modal
	netDiagIdx := strings.Index(html, `id="netDiagModal"`)
	compareIdx := strings.Index(html, `id="hostCompareModal"`)
	if netDiagIdx != -1 && compareIdx != -1 && compareIdx > netDiagIdx {
		sub := html[netDiagIdx:compareIdx]
		openDivs := strings.Count(sub, "<div")
		closeDivs := strings.Count(sub, "</div>")
		if openDivs != closeDivs {
			t.Errorf("hostCompareModal is improperly nested inside netDiagModal (open=%d close=%d)", openDivs, closeDivs)
		}
	}
}

func TestCopyComparisonMarkdownClipboardInsecureContextFallback(t *testing.T) {
	html := string(embeddedHTML)

	// In non-secure contexts (HTTP over LAN IP, e.g. http://192.168.x.x:8080),
	// navigator.clipboard is undefined in all modern browsers.
	// copyComparisonMarkdown must use copyTextToClipboard or provide execCommand fallback,
	// and must NOT invoke navigator.clipboard.writeText without a null-check.
	copyFnIdx := strings.Index(html, "function copyComparisonMarkdown")
	if copyFnIdx == -1 {
		t.Fatalf("function copyComparisonMarkdown not found in index.html")
	}

	// Extract the body of copyComparisonMarkdown up to next function
	copyFnEnd := strings.Index(html[copyFnIdx:], "function renderHostCompareView")
	if copyFnEnd == -1 {
		copyFnEnd = len(html) - copyFnIdx
	}
	copyFnBody := html[copyFnIdx : copyFnIdx+copyFnEnd]

	if !strings.Contains(copyFnBody, "copyTextToClipboard") {
		t.Errorf("copyComparisonMarkdown must use copyTextToClipboard to support non-HTTPS LAN access (execCommand fallback), but got: %s", copyFnBody)
	}

	if strings.Contains(copyFnBody, "navigator.clipboard.writeText") && !strings.Contains(copyFnBody, "navigator.clipboard &&") {
		t.Errorf("copyComparisonMarkdown must not directly call navigator.clipboard.writeText without checking navigator.clipboard (causes TypeError in insecure contexts)")
	}
}

