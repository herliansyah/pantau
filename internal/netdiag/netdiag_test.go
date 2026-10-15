package netdiag

import (
	"strings"
	"testing"
)

func TestClassifyIPZone(t *testing.T) {
	cases := []struct {
		ip       string
		target   string
		expected string
	}{
		{"192.168.1.1", "1.1.1.1", "private"},
		{"10.0.0.5", "8.8.8.8", "private"},
		{"172.16.5.2", "google.com", "private"},
		{"100.64.0.1", "1.1.1.1", "private"}, // CGNAT
		{"127.0.0.1", "1.1.1.1", "private"},
		{"1.1.1.1", "1.1.1.1", "destination"},
		{"8.8.8.8", "8.8.8.8", "destination"},
		{"103.200.1.1", "1.1.1.1", "transit"},
		{"*", "1.1.1.1", "timeout"},
		{"no reply", "1.1.1.1", "timeout"},
	}

	for _, c := range cases {
		got := ClassifyIPZone(c.ip, c.target)
		if got != c.expected {
			t.Errorf("ClassifyIPZone(%q, %q) = %q; want %q", c.ip, c.target, got, c.expected)
		}
	}
}

func TestParseTracepathOrTracerouteLine(t *testing.T) {
	// 1. Tracepath normal hop
	line1 := " 1:  192.168.1.1                                           1.234ms"
	hop1 := ParseTracepathOrTracerouteLine(line1, "1.1.1.1")
	if hop1 == nil || hop1.Hop != 1 || hop1.Host != "192.168.1.1" || hop1.RTTMs != 1.234 || hop1.Zone != "private" || hop1.Status != "ok" {
		t.Fatalf("unexpected hop1: %+v", hop1)
	}

	// 2. Tracepath reached hop
	line2 := " 4:  1.1.1.1                                              12.345ms reached"
	hop2 := ParseTracepathOrTracerouteLine(line2, "1.1.1.1")
	if hop2 == nil || hop2.Hop != 4 || hop2.Zone != "destination" {
		t.Fatalf("unexpected hop2: %+v", hop2)
	}

	// 3. Tracepath no reply
	line3 := " 3:  no reply"
	hop3 := ParseTracepathOrTracerouteLine(line3, "1.1.1.1")
	if hop3 == nil || hop3.Hop != 3 || hop3.Status != "timeout" || hop3.Zone != "timeout" {
		t.Fatalf("unexpected hop3: %+v", hop3)
	}

	// 4. Traceroute standard
	line4 := " 2  gateway (10.0.0.1)  2.34 ms"
	hop4 := ParseTracepathOrTracerouteLine(line4, "1.1.1.1")
	if hop4 == nil || hop4.Hop != 2 || hop4.IP != "10.0.0.1" || hop4.Zone != "private" {
		t.Fatalf("unexpected hop4: %+v", hop4)
	}

	// 5. Traceroute asterisk timeout
	line5 := " 5  *"
	hop5 := ParseTracepathOrTracerouteLine(line5, "1.1.1.1")
	if hop5 == nil || hop5.Hop != 5 || hop5.Status != "timeout" {
		t.Fatalf("unexpected hop5: %+v", hop5)
	}
}

func TestAnalyzeHops_Optimal(t *testing.T) {
	hops := []*HopInfo{
		{Hop: 1, Host: "192.168.1.1", IP: "192.168.1.1", RTTMs: 1.2, Zone: "private", Status: "ok"},
		{Hop: 2, Host: "103.10.10.1", IP: "103.10.10.1", RTTMs: 4.5, Zone: "transit", Status: "ok"},
		{Hop: 3, Host: "1.1.1.1", IP: "1.1.1.1", RTTMs: 12.4, Zone: "destination", Status: "ok"},
	}

	analysis := AnalyzeHops(hops, "1.1.1.1", true)
	if analysis.Status != "optimal" {
		t.Errorf("expected optimal, got %s", analysis.Status)
	}
	if !analysis.DestinationFound {
		t.Errorf("expected destination found")
	}
	if !strings.Contains(analysis.Summary, "Route healthy and optimal") {
		t.Errorf("unexpected summary: %s", analysis.Summary)
	}
	if !strings.Contains(analysis.SummaryID, "Rute sehat dan optimal") {
		t.Errorf("unexpected summary ID: %s", analysis.SummaryID)
	}
}

func TestAnalyzeHops_BlackholeDrop(t *testing.T) {
	hops := []*HopInfo{
		{Hop: 1, Host: "192.168.1.1", IP: "192.168.1.1", RTTMs: 1.2, Zone: "private", Status: "ok"},
		{Hop: 2, Host: "103.10.10.1", IP: "103.10.10.1", RTTMs: 4.5, Zone: "transit", Status: "ok"},
		{Hop: 3, Host: "* * *", IP: "", RTTMs: 0, Zone: "timeout", Status: "timeout"},
		{Hop: 4, Host: "* * *", IP: "", RTTMs: 0, Zone: "timeout", Status: "timeout"},
	}

	analysis := AnalyzeHops(hops, "1.1.1.1", false)
	if analysis.Status != "failed" {
		t.Errorf("expected failed, got %s", analysis.Status)
	}
	if analysis.DropAtHop != 2 {
		t.Errorf("expected DropAtHop 2, got %d", analysis.DropAtHop)
	}
	if !strings.Contains(analysis.SummaryID, "terhenti setelah Hop 2") {
		t.Errorf("unexpected summary ID: %s", analysis.SummaryID)
	}
}

func TestAnalyzeHops_LatencySpike(t *testing.T) {
	hops := []*HopInfo{
		{Hop: 1, Host: "192.168.1.1", IP: "192.168.1.1", RTTMs: 2.0, Zone: "private", Status: "ok"},
		{Hop: 2, Host: "103.10.10.1", IP: "103.10.10.1", RTTMs: 120.0, Zone: "transit", Status: "warning"},
		{Hop: 3, Host: "1.1.1.1", IP: "1.1.1.1", RTTMs: 125.0, Zone: "destination", Status: "warning"},
	}

	analysis := AnalyzeHops(hops, "1.1.1.1", true)
	if analysis.Status != "degraded" {
		t.Errorf("expected degraded, got %s", analysis.Status)
	}
	if analysis.LatencySpikeHop != 2 {
		t.Errorf("expected LatencySpikeHop 2, got %d", analysis.LatencySpikeHop)
	}
	if !strings.Contains(analysis.SummaryID, "lonjakan latensi") {
		t.Errorf("unexpected summary ID: %s", analysis.SummaryID)
	}
}

func TestParsePingOutput(t *testing.T) {
	output := `PING 1.1.1.1 (1.1.1.1) 56(84) bytes of data.
64 bytes from 1.1.1.1: icmp_seq=1 ttl=57 time=13.2 ms
64 bytes from 1.1.1.1: icmp_seq=2 ttl=57 time=12.8 ms
64 bytes from 1.1.1.1: icmp_seq=3 ttl=57 time=14.1 ms
64 bytes from 1.1.1.1: icmp_seq=4 ttl=57 time=13.5 ms

--- 1.1.1.1 ping statistics ---
4 packets transmitted, 4 received, 0% packet loss, time 3004ms
rtt min/avg/max/mdev = 12.800/13.400/14.100/0.479 ms`

	stats := ParsePingOutput(output)
	if stats.PacketsTransmitted != 4 || stats.PacketsReceived != 4 {
		t.Fatalf("unexpected packet counts: %+v", stats)
	}
	if stats.PacketLossPct != 0.0 {
		t.Fatalf("expected 0 loss, got %f", stats.PacketLossPct)
	}
	if stats.AvgMs != 13.4 || stats.MinMs != 12.8 || stats.MaxMs != 14.1 {
		t.Fatalf("unexpected RTTs: min=%f avg=%f max=%f", stats.MinMs, stats.AvgMs, stats.MaxMs)
	}
	if stats.Status != "healthy" {
		t.Fatalf("expected healthy, got %s", stats.Status)
	}
}

func TestParsePortOutput(t *testing.T) {
	openStats := ParsePortOutput("some log\nPORT_OPEN\n", 443)
	if !openStats.Open || openStats.Status != "healthy" {
		t.Fatalf("expected open port: %+v", openStats)
	}

	closedStats := ParsePortOutput("some log\nPORT_CLOSED\n", 3306)
	if closedStats.Open || closedStats.Status != "down" {
		t.Fatalf("expected closed port: %+v", closedStats)
	}
}

func TestParseDNSOutput(t *testing.T) {
	output := `142.250.190.46  google.com`
	dns := ParseDNSOutput(output, "google.com")
	if dns.Status != "healthy" || len(dns.ResolvedIPs) == 0 || dns.ResolvedIPs[0] != "142.250.190.46" {
		t.Fatalf("unexpected DNS stats: %+v", dns)
	}
}

func TestValidateTarget(t *testing.T) {
	if err := ValidateTarget("1.1.1.1"); err != nil {
		t.Errorf("expected 1.1.1.1 to be valid: %v", err)
	}
	if err := ValidateTarget("google.com"); err != nil {
		t.Errorf("expected google.com to be valid: %v", err)
	}
	if err := ValidateTarget("host-01.internal.net"); err != nil {
		t.Errorf("expected domain to be valid: %v", err)
	}

	// Injections should be rejected
	if err := ValidateTarget("1.1.1.1; rm -rf /"); err == nil {
		t.Errorf("expected semicolon injection to fail")
	}
	if err := ValidateTarget("1.1.1.1 && whoami"); err == nil {
		t.Errorf("expected && injection to fail")
	}
	if err := ValidateTarget("`touch evil`"); err == nil {
		t.Errorf("expected backtick injection to fail")
	}
}
