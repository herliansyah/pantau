# 12 — Network & Security Observability (Bandwidth, Active Sockets, Egress, Port Exposure)

**What to build:** Comprehensive agentless network and security monitoring: real-time inbound/outbound bandwidth (KB/s, MB/s) via `/proc/net/dev` sampling, internet egress connectivity & latency probe, public IP discovery, active socket profiling (top connected external IPs and destination ports), listening ports public exposure audit with risk classification, and failed login attempt counting. Visualized via overview badges on Host cards and a deep "Network & Security" tab in the Host detail view.

**Blocked by:** 02 — Periodic Inspection & Lifecycle Score.

**Status:** resolved

- [x] Add network and security metric columns to `store.Host` with idempotent SQLite schema migration.
- [x] Extend Inspector's single-shot SSH batch command to collect `/proc/net/dev`, ping latency, public IP, `ss` established sockets, `ss` listening ports, and failed logins.
- [x] Calculate live bandwidth throughput speeds (RX/TX bps) and classify listening port risk (public vs local).
- [x] Update Host overview card with live Internet connectivity badge and RX/TX traffic throughput.
- [x] Add dedicated "Network & Security" tab in Host details modal displaying bandwidth stats, top remote connections, open ports exposure table, and brute-force indicators.
- [x] Automated tests covering network scraping, port exposure audit, egress latency, and API payload.
