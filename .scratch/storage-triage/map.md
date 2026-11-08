# Map: Agentless Storage Triage & Runaway Detection

Status: resolved

## Destination

Menghadirkan kapabilitas investigasi on-demand berbasis SSH tanpa agen untuk mendiagnosis saturasi ruang disk (*disk exhaustion*), memetakan direktori penyerap kapasitas (*Space Hog*), mendeteksi berkas terhapus tertahan proses (*Ghost File*), melacak lonjakan berkas liar (*Runaway File*), serta menyediakan katalog pembersihan aman terkurasi (*Reclaimable Storage*) dan pengosongan log in-place (*Log Truncation*).

## Notes

- Domain: Agentless Linux Storage Telemetry, SSH Execution, Safety Whitelists.
- ADR: [0039-agentless-storage-triage-runaway-detection-and-safe-reclaim.md](../../docs/adr/0039-agentless-storage-triage-runaway-detection-and-safe-reclaim.md)
- Standing preferences: Prinsip Ponytail — batch POSIX commands via SSH, timeout ketat (20–40s), singleflight concurrency lock, in-place zero-byte truncate (`truncate -s 0`), zero background recurring poll.

## Decisions so far

<!-- the index — one line per closed ticket -->

- [01 — Backend Storage Triage Probe](issues/01-backend-storage-triage-probe.md) — Ekstraksi via batch POSIX script: `df`, `du -kx --max-depth=1`, `find` files >15MB, `find` mmin -60 (eksklusi DBMS), `lsof` / `/proc/*/fd` ghost files, dan katalog safe reclaim.
- [02 — API Endpoints and Reclaim Executor](issues/02-api-endpoints-and-reclaim-executor.md) — Rute `GET /api/hosts/{id}/storage-triage` dan `POST /api/hosts/{id}/storage-reclaim` dengan validasi perintah ketat dan penolakan injeksi shell.
- [03 — Frontend UI Tab and Visual Panels](issues/03-frontend-ui-tab-and-visual-panels.md) — Tab `🧹 Storage Triage` di Host Detail Workspace Modal, ringkasan kartu disk, panel aksi safe reclaim transparan, bar chart space hogs, runaway/ghost alerts, dan deep-link dari Alert Banner.
- [04 — Load Throttling, Cooldown, and Security Hardening](issues/04-load-throttling-cooldown-and-security-hardening.md) — Pembatasan eksekusi berbasis I/O load (2m/5m/blocked), emergency force scan (`?force=true`), auto-reset cooldown pasca-reclaim, `stat -L` ghost file inspect, dan proteksi path traversal `TruncateLog`.
- [05 — Agentless Windows Storage Triage and Safe Reclaim](issues/05-agentless-windows-storage-triage.md) — Ekstensi triage untuk target Windows Server via UTF-16LE Base64 PowerShell: CIM disk metrics, zero-copy `robocopy /L` space hogs, single-pass runaway sensing (eksklusi DBMS/arsip), katalog safe reclaim Windows, dan `Clear-Content` log truncation.

## Out of scope

- Background recurring disk scans / polling (merusak performa I/O host target).
- Penghapusan otomatis isi direktori `/tmp` (risiko memutus domain socket dan file sesi aktif).
- Eksekusi `rm` pada file log aktif (diganti dengan `truncate -s 0`).
