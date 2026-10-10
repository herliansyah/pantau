# Map: Disk I/O & Throughput Rate Observability

Status: open

## Destination

Menghadirkan observabilitas real-time untuk laju Disk Read/Write (Speedometer delta/detik & bytes) pada arsitektur agentless SSH Pantau, melengkapi Network Rx/Tx live rate yang sudah ada, dengan ambang batas Drift pendeteksi spike I/O.

## Notes

- Domain: Agentless Linux Telemetry, Embedded SQLite, Responsive Web UI.
- Skills to consult: `/grilling`, `/domain-modeling`, `/codebase-design`.
- Standing preferences: Prinsip Ponytail (Lazy senior dev) — gunakan native Linux standard (`/proc/diskstats`), satu batch command via SSH, zero daemon/agent baru, hindari `iotop`/`iostat` dependency eksternal.

## Decisions so far

<!-- the index — one line per closed ticket -->

- [01 — Format Pengambilan Telemetri Disk I/O Lintas Kernel & Distro](issues/01-disk-io-telemetry-extraction.md) — Ekstraksi via `/proc/diskstats` dengan POSIX `awk` whitelist block device fisik (`sd*`, `vd*`, `nvme*n*`, `xvd*`, `mmcblk*`) dikali 512 bytes sektor.
- [02 — Desain Skema Database & Kalkulasi Delta Rate Speedometer](issues/02-database-schema-and-speedometer-delta.md) — 4 kolom baru di `hosts` (`disk_read_bytes`, `disk_write_bytes`, `disk_read_speed_bps`, `disk_write_speed_bps`), kalkulasi delta per detik, dan fallback 0 bps saat reboot.
- [03 — Representasi UI Dashboard & Komponen Speedometer](issues/03-dashboard-ui-speedometer-layout.md) — Tata letak compact 4-kolom bersebelahan di Host Card (Net ⬇️/⬆️ + Disk 📖/✍️) dan card observabilitas khusus di Host Detail Modal.


## Not yet specified

- Per-process atau per-container Disk I/O breakdown (apakah diperlukan atau cukup agregat host untuk menjaga kesederhanaan agentless).
- Visual sparkline / time-series trend mini-chart di UI (apakah cukup indikator speedometer / delta rate per detik saat ini).

## Out of scope

- Monitoring granular disk per partisi virtual mount/loop device (Snap/SquashFS).
- Block layer tracing via eBPF/blktrace (melanggar prinsip agentless dan butuh root kernel privilege khusus).
