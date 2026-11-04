package web

import (
	"strings"
	"testing"
)

func TestHostSwitcherAndMetricsShortcutUI(t *testing.T) {
	html := string(embeddedHTML)

	// 1. Verify CSS styles for host switcher & metrics button
	expectedCSS := []string{
		".btn-host-metrics",
		".host-nav-btn",
		".host-switcher-trigger",
		".host-switcher-dropdown",
		".host-switch-header",
		".host-switch-row",
		".host-switch-col",
	}
	for _, css := range expectedCSS {
		if !strings.Contains(html, css) {
			t.Errorf("expected CSS class %q in index.html", css)
		}
	}

	// 2. Verify modal header controls (Host Switcher, Prev, Next buttons, Searchable Popover)
	headerElements := []string{
		`id="btnHostNavPrev"`,
		`onclick="navigateDetailHost(-1, event)"`,
		`id="btnHostNavNext"`,
		`onclick="navigateDetailHost(1, event)"`,
		`id="hostSwitcherTriggerBtn"`,
		`id="switcherCurrentHostName"`,
		`onclick="toggleHostSwitcherDropdown(event)"`,
		`id="hostSwitcherDropdown"`,
		`id="hostSwitcherSearch"`,
		`id="hostSwitchList"`,
		`id="hostSwitcherCount"`,
	}
	for _, elem := range headerElements {
		if !strings.Contains(html, elem) {
			t.Errorf("expected header element %q in index.html", elem)
		}
	}

	// 3. Verify original left-side objects remain untouched and intact
	leftElements := []string{
		`id="detailHostName"`,
		`id="detailHostBadge"`,
		`id="inspectBtnModal"`,
	}
	for _, elem := range leftElements {
		if !strings.Contains(html, elem) {
			t.Errorf("expected left-side element %q in index.html", elem)
		}
	}

	// 4. Verify 1-click Telemetry shortcut button (.btn-host-metrics)
	metricButtons := []string{
		`class="btn btn-sm btn-host-metrics"`,
		`onclick="openHostDetail(${h.id}, 'metrics')"`,
	}
	for _, btn := range metricButtons {
		if !strings.Contains(html, btn) {
			t.Errorf("expected metric button attribute %q in index.html", btn)
		}
	}

	// 5. Verify JavaScript functions for Host Switcher
	expectedJS := []string{
		"function getNavigableHosts()",
		"async function switchDetailHost(",
		"function navigateDetailHost(",
		"function toggleHostSwitcherDropdown(",
		"function closeHostSwitcherDropdown()",
		"function renderHostSwitcherList(",
		"function filterHostSwitcherList(",
		"function handleHostSwitcherKeydown(",
	}
	for _, js := range expectedJS {
		if !strings.Contains(html, js) {
			t.Errorf("expected JavaScript function %q in index.html", js)
		}
	}

	// 6. Verify keyboard shortcut navigation [ and ]
	if !strings.Contains(html, `e.key === '['`) || !strings.Contains(html, `navigateDetailHost(-1)`) {
		t.Errorf("expected [ shortcut for navigateDetailHost(-1)")
	}
	if !strings.Contains(html, `e.key === ']'`) || !strings.Contains(html, `navigateDetailHost(1)`) {
		t.Errorf("expected ] shortcut for navigateDetailHost(1)")
	}

	// 7. Verify i18n keys
	i18nKeys := []string{
		`"btn_metrics_telemetry"`,
		`"host_switcher_title"`,
		`"host_switcher_search_ph"`,
		`"prev_host"`,
		`"next_host"`,
		`"no_hosts_found"`,
	}
	for _, key := range i18nKeys {
		// Must appear at least twice (en and id)
		count := strings.Count(html, key)
		if count < 2 {
			t.Errorf("expected i18n key %q to appear at least twice (en and id), found %d", key, count)
		}
	}
}
