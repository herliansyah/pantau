package netdiag

import (
	"strings"
	"testing"
)

func TestParseCurlL7Output(t *testing.T) {
	// 1. Success HTTP 200 OK
	raw200 := "200|0.001|0.002|0.012|0.015\n"
	stats := ParseCurlL7Output(raw200, "http://127.0.0.1:8080/", "local_loopback")
	if stats.HTTPCode != 200 {
		t.Errorf("expected code 200, got %d", stats.HTTPCode)
	}
	if stats.Status != "healthy" {
		t.Errorf("expected healthy, got %s", stats.Status)
	}
	if stats.TTFBMs != 12.0 {
		t.Errorf("expected TTFB 12.0 ms, got %.2f", stats.TTFBMs)
	}

	// 2. Slow TTFB degraded
	rawSlow := "200|0.001|0.002|3.200|3.250\n"
	statsSlow := ParseCurlL7Output(rawSlow, "http://127.0.0.1:8080/reports", "local_loopback")
	if statsSlow.Status != "degraded" {
		t.Errorf("expected degraded for slow TTFB, got %s", statsSlow.Status)
	}

	// 3. HTTP 500 Server Error
	raw500 := "500|0.001|0.002|0.050|0.055\n"
	stats500 := ParseCurlL7Output(raw500, "http://127.0.0.1:8080/api", "local_loopback")
	if stats500.Status != "degraded" {
		t.Errorf("expected degraded for 500 error, got %s", stats500.Status)
	}

	// 4. Connection refused
	rawFail := "\n"
	statsFail := ParseCurlL7Output(rawFail, "http://127.0.0.1:8080/", "local_loopback")
	if statsFail.Status != "down" {
		t.Errorf("expected down for empty output, got %s", statsFail.Status)
	}
}

func TestParseRouteGetOutput(t *testing.T) {
	// WireGuard route output
	rawWG := "10.20.1.1 dev wg0 src 10.20.0.2 uid 1000\n    cache"
	route := ParseRouteGetOutput(rawWG, "10.20.1.1")
	if route.Interface != "wg0" {
		t.Errorf("expected wg0 interface, got %s", route.Interface)
	}
	if !route.IsPrivate {
		t.Errorf("expected 10.20.1.1 to be classified as private")
	}

	// Gateway with MTU
	rawGW := "192.168.1.1 via 192.168.1.254 dev eth0 mtu 1420\n    cache"
	routeGW := ParseRouteGetOutput(rawGW, "192.168.1.1")
	if routeGW.Interface != "eth0" {
		t.Errorf("expected eth0 interface, got %s", routeGW.Interface)
	}
	if routeGW.Gateway != "192.168.1.254" {
		t.Errorf("expected gateway 192.168.1.254, got %s", routeGW.Gateway)
	}
	if routeGW.MTU != 1420 {
		t.Errorf("expected MTU 1420, got %d", routeGW.MTU)
	}
}

func TestParseMTUPingSuccess(t *testing.T) {
	// Passed ping output
	passedPing := `PING 10.20.1.1 (10.20.1.1) 1200(1228) bytes of data.
72 bytes from 10.20.1.1: icmp_seq=1 ttl=64 time=12.3 ms
72 bytes from 10.20.1.1: icmp_seq=2 ttl=64 time=11.8 ms

--- 10.20.1.1 ping statistics ---
2 packets transmitted, 2 received, 0% packet loss, time 1001ms
rtt min/avg/max/mdev = 11.8/12.05/12.3/0.25 ms`
	if !ParseMTUPingSuccess(passedPing) {
		t.Errorf("expected ping to pass")
	}

	// Failed with Frag needed
	failedFrag := `PING 10.20.1.1 (10.20.1.1) 1472(1500) bytes of data.
From 10.20.0.1 icmp_seq=1 Frag needed and DF set (mtu = 1420)

--- 10.20.1.1 ping statistics ---
2 packets transmitted, 0 received, +1 errors, 100% packet loss, time 1002ms`
	if ParseMTUPingSuccess(failedFrag) {
		t.Errorf("expected ping to fail due to Frag needed")
	}
}

func TestFormatIncidentTicket(t *testing.T) {
	hr := &HostResourceStats{
		CPULoad:           0.15,
		RAMUsedPct:        42.5,
		ActiveConnections: 85,
		Status:            "healthy",
	}
	l7 := &L7ProbeStats{
		HTTPCode: 200,
		TTFBMs:   15.4,
		TotalMs:  22.1,
		Status:   "healthy",
	}
	route := &RouteInfo{
		Interface: "wg0",
		MTU:       1420,
	}
	ping := &PingStats{
		AvgMs:         14.2,
		PacketLossPct: 0.0,
		Status:        "healthy",
	}
	mtu := &MTUProbeStats{
		BaselineMTU:       1420,
		BlackholeDetected: false,
	}

	ticket := formatIncidentTicket("Host A (Production)", "Cabang Surabaya", "10.20.1.1", VerdictHealthy, "Semua layer beroperasi normal.", hr, l7, nil, 0, route, ping, mtu)

	if !strings.Contains(ticket, "LAPORAN TRIAGE") {
		t.Errorf("expected ticket to contain LAPORAN TRIAGE")
	}
	if !strings.Contains(ticket, "Cabang Surabaya") {
		t.Errorf("expected ticket to mention Cabang Surabaya")
	}
	if !strings.Contains(ticket, "VERDICT*: *HEALTHY*") {
		t.Errorf("expected verdict HEALTHY in ticket")
	}
}
