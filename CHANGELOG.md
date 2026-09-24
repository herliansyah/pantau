# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]
### Added
- Floating Alert Summary Bar: non-blocking summary banner (`⚠️ {count} Active Alerts ({hostCount} Hosts)`) with zero layout shift, collapsible floating overlay panel, full ISO datetime tooltips, inline Root Cause Excerpts, and direct `[Open Host]` shortcuts.
- Alert Bulk Dismissal & Auto-Resolution: `Dismiss All` action button (`POST /api/alerts/ack-all`) and automatic resolution of active alerts for a host when inspection verifies system returned to healthy desired state.
- Dual-Bucket Inspection Runs Retention: SQLite inline auto-pruning preserving up to 100 latest OK runs and 100 latest issue runs (`drift`, `down`, `error`, `degraded`) per host, guaranteeing long-term audit trail preservation without disk bloat.
- Segmented Audit Trail Sub-Tabs: interactive filter switcher in Workspace Modal Inspection Runs tab (`[ 📋 All History ]` vs `[ ⚠️ Last 100 Issues (Audit Trail) ]`) with auto-expanded diagnostic details and precise timestamps.
- Background Inspection Disable Switch: setting `poll_interval_sec` to `0` in Settings > General halts the background inspection worker for on-demand only operations, while preserving manual "Inspect" and "Inspect All" functionality.
- UI status indicators for paused background inspection: amber pause badge in the dashboard toolbar (`⏸️ Background Inspection Paused`) and engine status indicator in the application footer (`Engine Active (Inspection Paused)`).
- Automatic suppression of Stale Inspection warnings: suppresses false-positive `⚠️ Stale Data (>10m)` warnings across host cards, list views, and details when background inspection is intentionally disabled.

## [0.14.0] - 2026-09-22
### Added
- Instance Guard: single-instance protection mechanism using OS kernel advisory locks (`flock` on Unix / `LockFileEx` on Windows) bound to the database file, preventing duplicate workers, reporting active port & PID, and launching the active browser URL before exiting gracefully.
- Directory Filter: reactive real-time client-side search and category filtering (`All`, `Folders`, `Files`) in SFTP File Manager with keyboard shortcuts (`Escape` to clear).
- File Sorting: interactive column sorting (Name, Size, Modified) with visual direction indicators (`▲` / `▼`) and persistent `Folders First` hierarchy across folder navigation in SFTP File Manager.

## [0.13.0] - 2026-09-20
### Added
- Structured Inspection Runs: execution audit history tracking started timestamp, duration (ms), status, human-readable summary, and failure diagnostics.
- Automatic inline rolling prune maintaining strict 100 runs limit per Host in SQLite without background scheduler overhead.
- Inspection Runs history tab in Host Detail Workspace Modal with interactive status badges and diagnostic expanders.
- Real-time execution roundtrip duration display (`last_duration_ms`) on Host cards, list view, and overview detail.
- Anti-flood inspection cooldown: 15-second minimum wait window per host for manual inspections returning `HTTP 429 Too Many Requests`.
- Explicit in-flight concurrency guard returning `HTTP 409 Conflict` when an inspection is already actively running on the target host.
- Stale and hung network mount (NFS/CIFS) protection isolating local partitions (`df -lPk /`) and enforcing 5s hard timeouts on disk checks.
- Bounded concurrency worker pool (maximum 5 concurrent hosts) and global in-flight lock for mass inspection (`Inspect All`).
- Enforced 45-second total hard timeout budget per inspection run.
- In-app Changelog viewer integrated into Documentation Modal.
- Footer version link and Settings update card link to view Changelog.
### Changed
- Polished dashboard header with responsive Inspect All button layout and distinct amber warning toast styling.

## [0.12.1] - 2026-09-18
### Fixed
- Terminal preset scope host binding and modal layering z-index.
- Synchronized bilingual documentation and user guides with v0.12 architecture.

## [0.12.0] - 2026-09-18
### Added
- Hardware specs and capacity metrics: CPU cores, load average, absolute RAM (GB/MB), Swap capacity, and root disk capacity.
- Terminal Header Launcher: global action button in header with real-time reactive active shell session count badges.
- Reactive host session badges on host cards.

## [0.11.0] - 2026-09-18
### Added
- Multi-arch Docker images (`linux/amd64` and `linux/arm64`) automatically built and pushed to GitHub Container Registry (GHCR).
### Changed
- Inspector I/O optimization: removed heavy periodic recursive `du` scanning to prevent disk thrashing.
- Added SSH command execution timeout guards and inspection concurrency guard to eliminate inspection stampedes.

## [0.10.0] - 2026-09-17
### Added
- Application version display on initial setup and login screens.
### Changed
- Reorganized Settings dialog layout for clearer navigation.

## [0.9.1] - 2026-09-17
### Fixed
- Release test update pipeline and verification assets.

## [0.9.0] - 2026-09-17
### Added
- Semi-automatic Update Checker and in-place Self-Update with SHA-256 integrity verification.
- Release checksums generation via GitHub Actions.

## [0.8.0] - 2026-09-17
### Added
- Mass host inspection ("Inspect All") for asynchronous bulk health refreshes.
- Multi-instance snapshot file path isolation.
- Windows executable icon embedding and favicon.
### Changed
- In-memory gzip compression and ETag conditional validation for web assets.

## [0.7.0] - 2026-09-16
### Added
- Airgapped TOTP two-factor authentication (2FA) with emergency recovery codes and CLI bypass flag.
- Global multi-tab Terminal Dock with collapsible floating dock bar.
- Embedded in-app Documentation Modal for airgapped bilingual guides (README and User Guide).
- Transparent 6-factor Lifecycle Assessment, productive lifespan estimation, and hardware refresh recommendations.
### Security
- Self-contained web assets with strict Content Security Policy (CSP), eliminating all external CDN dependencies.

## [0.6.0] - 2026-09-09
### Added
- Host grouping by logical environment/function.
- Terminal maximize mode for full-viewport shell observability.
- Port auto-scan during server startup to automatically select next available port if default is busy.
- CLI startup banner and `-v` version flag.
- Toast notifications and Promise-based confirmation dialogs replacing native browser popups.

## [0.5.1] - 2026-09-09
### Added
- Automated Windows binary release builds (`amd64` and `arm64`).

## [0.5.0] - 2026-09-09
### Added
- Bilingual interface support (English and Indonesian).
- Windows portable server compatibility with POSIX remote path enforcement.
- Encrypted System Snapshot backup (AES-256-GCM) with optional GitHub Private Repository sync.
- Terminal Presets for reusable quick command shortcuts.
- Protected filesystem paths prevention in file manager.
- Host notes, global operational notes, and custom manual host ordering.

## [0.4.0] - 2026-09-09
### Added
- Legacy Linux server compatibility (CentOS 6, Debian 7, pre-systemd environments) and universal SSH key support.

## [0.3.0] - 2026-09-09
### Added
- Agentless Network & Security Observability: bandwidth rate, egress latency, socket connection profiling, and port exposure auditing.

## [0.2.0] - 2026-09-08
### Added
- Piped cross-host direct file/directory transfers without intermediate disk caching.
- On-the-fly streaming folder downloads (`zip` / `tar.gz`).

## [0.1.0] - 2026-09-08
### Added
- Initial release of Pantau: agentless SSH server monitoring and management system.
- Desired state verification, automated drift detection, root cause excerpts, and terminal file manager.
