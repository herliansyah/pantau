# 0004. Agentless Network & Security Observability

Date: 2026-09-08

## Status

Accepted

## Context

Users need real-time insight into server network activity and security posture:
1. Inbound/Outbound throughput (bandwidth speed in KB/s or MB/s) and total data transferred.
2. Connection provenance: which external IP addresses are connected, to which ports, and via what services.
3. Internet egress health: whether the server has active connectivity to the public internet and latency metrics.
4. Attack surface exposure: auditing listening ports (publicly bound `0.0.0.0` vs local `127.0.0.1`), highlighting high-risk exposed database or internal ports, and tracking failed login attempts.

Installing heavy monitoring agents or packet-capture daemons (e.g. nethogs, tcpdump, vnstat) violates Pantau's core principle of zero-dependency agentless SSH management.

## Decision

Implement lightweight, single-roundtrip agentless inspection using native Linux kernel interfaces and core POSIX utilities:
1. **Bandwidth & Transfer Tracking**: Sample `/proc/net/dev` across non-loopback interfaces to calculate inbound (`rx_speed_bps`) and outbound (`tx_speed_bps`) speeds via time deltas, plus cumulative bytes since boot.
2. **Internet Egress & Latency**: Probe public DNS (`1.1.1.1` or `8.8.8.8`) via ICMP/TCP ping and discover public IP via lightweight HTTP query.
3. **Socket Profiling & Top Remote IPs**: Parse `ss -nt state established` to summarize top external client IPs, destination ports, and total active sockets.
4. **Port Exposure & Security Audit**: Parse `ss -tlpn` to distinguish public interfaces (`0.0.0.0`, `*`, `[::]`) from loopback (`127.0.0.1`), raising high-risk flags for sensitive services (MySQL 3306, Postgres 5432, Redis 6379, Mongo 27017, Elasticsearch 9200) bound to public interfaces.
5. **Brute-Force Warning**: Count recent failed authentication attempts from system auth logs (`/var/log/auth.log` or `/var/log/secure`).

## Consequences

- **Zero Agent Overhead**: No daemon or third-party binary installed on target hosts.
- **Sub-10ms Execution**: Bundled into the existing inspection SSH batch command with negligible CPU and RAM impact.
- **Immediate Visual Feedback**: High-level badges on the Host card (Internet Online status and Live RX/TX speeds), and deep diagnostics in a dedicated "Network & Security" detail tab.
