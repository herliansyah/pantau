package web

import (
	"strings"
	"testing"
)

func TestTerminalLinkAndTunnelUIInIndexHTML(t *testing.T) {
	html := string(embeddedHTML)

	// 1. Must define link provider setup and activation handler
	if !strings.Contains(html, "setupTerminalLinkProvider(term, tab,") {
		t.Errorf("expected index.html to call setupTerminalLinkProvider for terminals")
	}
	if !strings.Contains(html, "function setupTerminalLinkProvider(") {
		t.Errorf("expected index.html to define setupTerminalLinkProvider")
	}
	if !strings.Contains(html, "registerLinkProvider") {
		t.Errorf("expected index.html to use term.registerLinkProvider")
	}
	if !strings.Contains(html, "handleTerminalLinkActivation(") {
		t.Errorf("expected index.html to define handleTerminalLinkActivation")
	}

	// 2. Modifier key enforcement (Ctrl+Click / Cmd+Click)
	if !strings.Contains(html, "event.ctrlKey || event.metaKey") {
		t.Errorf("expected link activation to require Ctrl or Cmd modifier key")
	}

	// 3. Loopback port detection and tunnel confirmation modal
	if !strings.Contains(html, "showTunnelConfirmationModal(") {
		t.Errorf("expected index.html to have showTunnelConfirmationModal")
	}
	if !strings.Contains(html, `id="tunnelModal"`) {
		t.Errorf("expected index.html to contain tunnelModal")
	}
	if !strings.Contains(html, "openTunnelInNewTab()") {
		t.Errorf("expected index.html to contain openTunnelInNewTab handler")
	}

	// 4. Ports dropdown menu in terminal toolbar
	if !strings.Contains(html, "terminal-ports-wrapper") {
		t.Errorf("expected terminal toolbar to contain terminal-ports-wrapper")
	}
	if !strings.Contains(html, "portsDropdownMenu_") {
		t.Errorf("expected terminal toolbar to contain portsDropdownMenu_")
	}
	if !strings.Contains(html, "openCustomTunnelForTab(") {
		t.Errorf("expected index.html to define openCustomTunnelForTab")
	}

	// 5. Check closing dropdown on outside click
	if !strings.Contains(html, ".terminal-ports-wrapper") {
		t.Errorf("expected window click listener to include .terminal-ports-wrapper")
	}
}
