package web

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"pantau/internal/inspector"
	"pantau/internal/netdiag"
	"pantau/internal/sshrunner"
	"pantau/internal/store"
)

func TestNetDiagStreamAuthAndValidation(t *testing.T) {
	srv := &Server{}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/netdiag/stream", srv.authMiddleware(srv.handleNetDiagStream))
	ts := httptest.NewServer(mux)
	defer ts.Close()

	// 1. Unauthenticated request must return 401
	respUnauth, err := http.Get(ts.URL + "/api/netdiag/stream")
	if err != nil {
		t.Fatal(err)
	}
	respUnauth.Body.Close()
	if respUnauth.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", respUnauth.StatusCode)
	}

	// 2. Authenticated request with invalid source host
	sessionToken := "test_netdiag_token"
	srv.sessions.Store(sessionToken, time.Now().Add(1*time.Hour))

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	req, _ := http.NewRequestWithContext(ctx, "GET", ts.URL+"/api/netdiag/stream?source_host_id=-1", nil)
	req.AddCookie(&http.Cookie{
		Name:  "pantau_session",
		Value: sessionToken,
	})

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK for SSE stream, got %d", resp.StatusCode)
	}
	contentType := resp.Header.Get("Content-Type")
	if !strings.HasPrefix(contentType, "text/event-stream") {
		t.Fatalf("expected text/event-stream, got %s", contentType)
	}

	reader := bufio.NewReader(resp.Body)
	line, err := reader.ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(line, "data: ") {
		t.Fatalf("expected data prefix, got: %s", line)
	}

	var evt netdiag.ProbeEvent
	if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &evt); err != nil {
		t.Fatalf("failed to unmarshal SSE event: %v", err)
	}
	if evt.Type != "error" {
		t.Fatalf("expected error event for invalid source_host_id, got: %s", evt.Type)
	}
}

func TestNetDiagUIInIndexHTML(t *testing.T) {
	srv := NewServer(nil, nil, nil)
	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	body := w.Body.String()

	// 1. Check header button
	if !strings.Contains(body, `id="btnHeaderNetDiag"`) {
		t.Errorf("expected btnHeaderNetDiag in header")
	}

	// 2. Check tab in host detail
	if !strings.Contains(body, `data-tab="netdiag"`) {
		t.Errorf("expected data-tab=\"netdiag\" in hostDetailTabs")
	}

	// 3. Check Action Dialog modal
	if !strings.Contains(body, `id="netDiagModal"`) {
		t.Errorf("expected netDiagModal in HTML body")
	}

	// 4. Check translations
	if !strings.Contains(body, `"netdiag_title": "Network Diagnostics"`) {
		t.Errorf("expected EN netdiag_title translation")
	}
	if !strings.Contains(body, `"netdiag_title": "Diagnostik Jaringan"`) {
		t.Errorf("expected ID netdiag_title translation")
	}
	for _, key := range []string{"netdiag_hop", "netdiag_host_ip", "netdiag_latency", "netdiag_zone"} {
		if !strings.Contains(body, `"`+key+`":`) {
			t.Errorf("expected translation key %s in translations", key)
		}
	}

	// 5. Check JS functions
	for _, fn := range []string{
		"function openNetDiagModal",
		"function renderNetDiagTab",
		"function runNetDiag",
		"function handleNetDiagEvent",
		"function renderHopAnalysisBanner",
		"function renderPingResults",
		"function renderPortResults",
		"function renderDNSResults",
	} {
		if !strings.Contains(body, fn) {
			t.Errorf("expected %s in index.html JavaScript", fn)
		}
	}

	// 6. Verify modal is NOT nested inside presetManagerModal
	presetIdx := strings.Index(body, `id="presetManagerModal"`)
	netDiagIdx := strings.Index(body, `id="netDiagModal"`)
	if presetIdx != -1 && netDiagIdx != -1 && netDiagIdx > presetIdx {
		presetSub := body[presetIdx:netDiagIdx]
		openDivs := strings.Count(presetSub, "<div")
		closeDivs := strings.Count(presetSub, "</div>")
		if openDivs != closeDivs {
			t.Errorf("netDiagModal is improperly nested inside presetManagerModal (open=%d close=%d)", openDivs, closeDivs)
		}
	}

	// 7. Verify symmetrical layout alignment in renderNetDiagUI
	if !strings.Contains(body, `height: 28px; display: flex; align-items: center; margin-bottom: 8px;`) {
		t.Errorf("expected aligned 28px header row for Source Host in renderNetDiagUI")
	}
	if !strings.Contains(body, `height: 28px; display: flex; justify-content: space-between; align-items: center; margin-bottom: 8px;`) {
		t.Errorf("expected aligned 28px header row for Target Mode in renderNetDiagUI")
	}

	// 8. Verify modal-dialog-xl on netDiagModal
	if !strings.Contains(body, `id="netDiagModal" class="modal">`+"\n"+`  <div class="modal-content modal-dialog-xl"`) {
		t.Errorf("expected modal-dialog-xl on netDiagModal")
	}

	// 9. Verify nowrap on hop zone badge and raw pre
	if !strings.Contains(body, `white-space: nowrap;">🏠 ${t('netdiag_zone_private')}`) {
		t.Errorf("expected nowrap on netdiag zone badge")
	}
	if !strings.Contains(body, `overflow: auto; color: #94a3b8; white-space: pre; line-height: 1.45;`) {
		t.Errorf("expected white-space: pre on raw CLI pre block")
	}
}

func TestNetDiagExecutionFlowWithMockRunner(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "pantau-netdiag-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	db, err := store.Open(filepath.Join(tmpDir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	// Seed hosts
	id1, err := db.CreateHost(&store.Host{Name: "Host-A", Host: "10.0.0.1", Port: 22, User: "root"})
	if err != nil {
		t.Fatal(err)
	}
	h1, _ := db.GetHost(id1)
	h1.Status = "healthy"
	_ = db.UpdateHostInspection(h1)

	id2, err := db.CreateHost(&store.Host{Name: "Host-B", Host: "10.0.0.2", Port: 22, User: "root"})
	if err != nil {
		t.Fatal(err)
	}
	h2, _ := db.GetHost(id2)
	h2.Status = "healthy"
	_ = db.UpdateHostInspection(h2)

	idOff, err := db.CreateHost(&store.Host{Name: "Host-Offline", Host: "10.0.0.99", Port: 22, User: "root"})
	if err != nil {
		t.Fatal(err)
	}
	hOffline, _ := db.GetHost(idOff)
	hOffline.Status = "down"
	_ = db.UpdateHostInspection(hOffline)

	mockRunner := sshrunner.NewMockRunner()
	mockRunner.DefaultExec = func(cmd string) (string, string, int, error) {
		if strings.Contains(cmd, "ping") {
			return `PING 1.1.1.1 (1.1.1.1) 56(84) bytes of data.
64 bytes from 1.1.1.1: icmp_seq=1 ttl=57 time=12.2 ms
64 bytes from 1.1.1.1: icmp_seq=2 ttl=57 time=12.8 ms
64 bytes from 1.1.1.1: icmp_seq=3 ttl=57 time=12.1 ms
64 bytes from 1.1.1.1: icmp_seq=4 ttl=57 time=12.5 ms

--- 1.1.1.1 ping statistics ---
4 packets transmitted, 4 received, 0% packet loss, time 3004ms
rtt min/avg/max/mdev = 12.100/12.400/12.800/0.250 ms`, "", 0, nil
		}
		if strings.Contains(cmd, "tracepath") || strings.Contains(cmd, "traceroute") {
			return ` 1:  192.168.1.1   1.234ms
 2:  103.10.10.1   5.678ms
 3:  1.1.1.1   12.456ms reached
Resume: pmtu 1500 hops 3 back 3`, "", 0, nil
		}
		if strings.Contains(cmd, "/dev/tcp") {
			return "PORT_OPEN\n", "", 0, nil
		}
		if strings.Contains(cmd, "getent hosts") {
			return "142.250.190.46  google.com\n", "", 0, nil
		}
		return "", "", 0, nil
	}

	factory := func(host string, port int, user, key string) (sshrunner.Runner, error) {
		return mockRunner, nil
	}
	ins := inspector.New(db, factory, nil)
	srv := NewServer(db, ins, nil)

	ctx := context.Background()

	// 1. Test Ping Probe
	var pingEvts []netdiag.ProbeEvent
	err = srv.netdiagService.ExecuteProbe(ctx, netdiag.ProbeRequest{
		SourceHostID:  h1.ID,
		Type:          netdiag.ProbeTypePing,
		TargetType:    "custom",
		TargetAddress: "1.1.1.1",
	}, func(evt netdiag.ProbeEvent) {
		pingEvts = append(pingEvts, evt)
	})
	if err != nil {
		t.Fatalf("ping probe failed: %v", err)
	}
	var gotPingStats bool
	for _, e := range pingEvts {
		if e.Type == "ping_stats" && e.Ping != nil {
			gotPingStats = true
			if e.Ping.PacketLossPct != 0.0 || e.Ping.AvgMs != 12.4 {
				t.Errorf("unexpected ping stats: %+v", e.Ping)
			}
		}
	}
	if !gotPingStats {
		t.Errorf("expected ping_stats event")
	}

	// 2. Test Traceroute Probe
	var traceEvts []netdiag.ProbeEvent
	err = srv.netdiagService.ExecuteProbe(ctx, netdiag.ProbeRequest{
		SourceHostID:  h1.ID,
		Type:          netdiag.ProbeTypeTraceroute,
		TargetType:    "custom",
		TargetAddress: "1.1.1.1",
	}, func(evt netdiag.ProbeEvent) {
		traceEvts = append(traceEvts, evt)
	})
	if err != nil {
		t.Fatalf("traceroute probe failed: %v", err)
	}
	var gotHops int
	var gotAnalysis bool
	for _, e := range traceEvts {
		if e.Type == "hop" {
			gotHops++
		}
		if e.Type == "hop_analysis" && e.Analysis != nil {
			gotAnalysis = true
			if e.Analysis.Status != "optimal" {
				t.Errorf("expected optimal status, got %s", e.Analysis.Status)
			}
		}
	}
	if gotHops == 0 || !gotAnalysis {
		t.Errorf("expected hops and analysis, got hops=%d analysis=%v", gotHops, gotAnalysis)
	}

	// 3. Test Port Probe
	var portEvts []netdiag.ProbeEvent
	err = srv.netdiagService.ExecuteProbe(ctx, netdiag.ProbeRequest{
		SourceHostID:  h1.ID,
		Type:          netdiag.ProbeTypePort,
		TargetType:    "host",
		TargetHostID:  h2.ID,
		Port:          443,
	}, func(evt netdiag.ProbeEvent) {
		portEvts = append(portEvts, evt)
	})
	if err != nil {
		t.Fatalf("port probe failed: %v", err)
	}
	var gotPortStats bool
	for _, e := range portEvts {
		if e.Type == "port_stats" && e.Port != nil {
			gotPortStats = true
			if !e.Port.Open || e.Port.Port != 443 {
				t.Errorf("unexpected port stats: %+v", e.Port)
			}
		}
	}
	if !gotPortStats {
		t.Errorf("expected port_stats event")
	}

	// 4. Test DNS Resolve Probe
	var dnsEvts []netdiag.ProbeEvent
	err = srv.netdiagService.ExecuteProbe(ctx, netdiag.ProbeRequest{
		SourceHostID:  h1.ID,
		Type:          netdiag.ProbeTypeDNS,
		TargetType:    "custom",
		TargetAddress: "google.com",
	}, func(evt netdiag.ProbeEvent) {
		dnsEvts = append(dnsEvts, evt)
	})
	if err != nil {
		t.Fatalf("dns probe failed: %v", err)
	}
	var gotDNSEvt bool
	for _, e := range dnsEvts {
		if e.Type == "dns_stats" && e.DNS != nil {
			gotDNSEvt = true
			if len(e.DNS.ResolvedIPs) == 0 || e.DNS.ResolvedIPs[0] != "142.250.190.46" {
				t.Errorf("unexpected DNS stats: %+v", e.DNS)
			}
		}
	}
	if !gotDNSEvt {
		t.Errorf("expected dns_stats event")
	}

	// 5. Test Offline Target Host rejection
	err = srv.netdiagService.ExecuteProbe(ctx, netdiag.ProbeRequest{
		SourceHostID: h1.ID,
		Type:         netdiag.ProbeTypePing,
		TargetType:   "host",
		TargetHostID: hOffline.ID,
	}, nil)
	if err == nil || !strings.Contains(err.Error(), "offline and cannot be probed") {
		t.Fatalf("expected offline target rejection, got: %v", err)
	}
}
