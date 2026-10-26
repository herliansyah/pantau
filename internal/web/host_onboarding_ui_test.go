package web

import (
	"strings"
	"testing"
)

func TestHostOnboardingAndSyncUIInIndexHTML(t *testing.T) {
	html := string(embeddedHTML)

	// 1. Host modal must have Test Connection button and result container
	if !strings.Contains(html, `id="btnTestHostModal"`) {
		t.Errorf("expected index.html to contain btnTestHostModal")
	}
	if !strings.Contains(html, "testConnectionInModal()") {
		t.Errorf("expected index.html to have testConnectionInModal handler")
	}
	if !strings.Contains(html, `id="hostModalTestResult"`) {
		t.Errorf("expected index.html to contain hostModalTestResult element")
	}

	// 2. Must call /api/hosts/test-connection
	if !strings.Contains(html, "/api/hosts/test-connection") {
		t.Errorf("expected index.html to call /api/hosts/test-connection")
	}

	// 3. Fast polling and reactive sync logic
	if !strings.Contains(html, "scheduleFastPollIfNeeded()") {
		t.Errorf("expected index.html to define scheduleFastPollIfNeeded")
	}
	if !strings.Contains(html, "refreshActiveHostDetailQuietly(") {
		t.Errorf("expected index.html to define refreshActiveHostDetailQuietly")
	}

	// 4. Initial sync card and docker empty-state awareness
	if !strings.Contains(html, "initial_sync_title") {
		t.Errorf("expected index.html to contain initial_sync_title i18n key")
	}
	if !strings.Contains(html, "docker_sync_title") {
		t.Errorf("expected index.html to contain docker_sync_title i18n key")
	}

	// 5. Two-column horizontal modal layout without text wrapping
	if !strings.Contains(html, "modal-dialog-host") {
		t.Errorf("expected index.html to use modal-dialog-host class for widened host modal")
	}
	if !strings.Contains(html, "host-form-grid") {
		t.Errorf("expected index.html to use host-form-grid 2-column layout")
	}
	if !strings.Contains(html, "resetHostModalTestResult()") {
		t.Errorf("expected index.html to call resetHostModalTestResult()")
	}
	if !strings.Contains(html, "ssh_pass_hint") {
		t.Errorf("expected index.html to contain ssh_pass_hint")
	}
	if !strings.Contains(html, "custom_key_hint") {
		t.Errorf("expected index.html to contain custom_key_hint")
	}
	if !strings.Contains(html, "test_status_label") {
		t.Errorf("expected index.html to contain test_status_label")
	}
}
