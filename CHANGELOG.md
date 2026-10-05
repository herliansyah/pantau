# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [0.18.0] - 2026-10-05
### Added
- Local File & Folder Duplication: instant cloning of files and directories on the same host via native remote `cp -a` with default timestamp naming (`<name>_<YYYYMMDDHHmmss>.<ext>` or `<name>_<YYYYMMDDHHmmss>`).
- Disk Space Safety Check: pre-execution verification against remote partition free space via `df -PB1` with a 100 MB safety buffer to protect host disks from exhaustion (HTTP 507 Insufficient Storage).
- Duplicate Confirmation Modal: dialog displaying item size, partition available disk, 500 MB threshold warning badge, pre-filled editable target name, and collision prevention rejecting existing target names (HTTP 409 Conflict).
- Editor Modal Maximize: full-viewport maximize toggle button (`⛶` / `🗗`) and double-click modal header shortcut with automatic CodeMirror canvas and gutter re-alignment.
- Modern VS Code Dark+ Theme: embedded self-contained dark theme for CodeMirror (`cm-s-vscode-dark`) with modern monospace font stack (`JetBrains Mono`, `Fira Code`, `Consolas`) and dynamic syntax highlight mode detection by file extension (`getEditorModeForPath`).
- Interactive Breadcrumb Navigation: clickable hierarchical path bar (`file-breadcrumb-bar`) in SFTP File Manager for instant jumping to any parent directory.
- File Manager UI Polish: contextual file icons by extension (`getFileIcon`), streamlined 6-column action grid with tooltips, and sticky table headers (`files-table-container`).
- Saweria Project Sponsorship: added Saweria funding configuration (`.github/FUNDING.yml`), sponsor badges in READMEs, sponsor footer pill, and settings modal card with bilingual support (EN/ID).
- Architectural Decision Record: [ADR-0028](docs/adr/0028-file-duplication-safety-editor-maximize-and-ui-polish.md).
### Changed
- Preset Manager Layout: widened preset modal dialog to `modal-dialog-lg` with horizontal scroll containers to prevent action and scope column squishing.
### Refactored
- Simplified version parsing in `updater.CompareVersions` using standard integer conversions without redundant character scans.
- Replaced manual buffer read/write loops in `MockRunner.PipeCommand` and `MockRunner.Terminal` with standard library `io.Copy`.
- Enforced dark theme consistency across CodeMirror session restoration.

## [0.17.0] - 2026-09-30
### Added
- Port and Process Desired State Evaluators: automated agentless compliance checks for listening network ports and running daemon processes with portable OS command fallbacks (`ss`, `netstat`, `lsof`, `fuser`, `pgrep`, `pidof`, `ps`).
- Desired State Rule Creation Hints: dynamic form guidance, validation hints, and default values for `port`, `process`, `cron`, and `backup` rules in the manual rule editor.
- Sub-Header Workspace Shelf: adaptive navigation shelf beneath main header holding minimized Host Detail and Code Editor sessions for instant 1-click restore without layout shifts or floating clutter.
- Smart Resume: automatically restores last navigated tab and SFTP File Manager directory when reopening hosts from dashboard host cards.
- Code Editor In-Memory Retention & Dirty Indicator: preserves unsaved file edits across minimization with visual dirty dot (`•`) and explicit close confirmation dialog.
- Window Modal Controls: standardized minimize button (`—`) alongside close button (`✕`) on Workspace Modals with safe `Escape` minimization and `Alt+1` to `Alt+5` quick-switching shortcuts.
- Workspace Modal Maximize: full-viewport maximize toggle button (`⛶` / `🗗`) and modal header double-click shortcut.
- Contextual Terminal Launcher from SFTP: launch interactive terminal session directly into the active directory from SFTP File Manager toolbar and table rows via `dir` query parameter.
- Terminal Clipboard Bridge & Multiline Paste Safety: context-aware `Ctrl+C` (copy on selection, SIGINT on idle), `Ctrl+Shift+C`, `Ctrl+V` bridge for non-HTTPS/IP origins, and 3-way multiline paste safety modal with single-line flattening (`Paste as Single Line`).
- Terminal Tab Host Filter: instant search box in Terminal Dock new tab menu with host name and IP filtering, auto-focus, and keyboard navigation (`Escape`, `Enter`).
- Architectural Decision Records: [ADR-0025](docs/adr/0025-desired-state-rule-taxonomy-and-evaluators.md), [ADR-0026](docs/adr/0026-sub-header-workspace-shelf-and-smart-resume.md), and [ADR-0027](docs/adr/0027-terminal-clipboard-bridge-and-multiline-paste-safety.md).
### Changed
- Static HTML compression: utilizes `gzip.BestCompression` for in-memory HTML pre-compression at startup.
- SFTP File Manager layout polish: consistent action button grid column widths and download button styling.
### Refactored
- Deduplicated 40-column SQL queries in host store and terminal preset scan logic.
- Replaced external terminal tty check dependency with standard library `os.Stdout.Stat`.
- Replaced custom HTML escaping helper with standard library `html.EscapeString`.

## [0.16.0] - 2026-09-26
### Added
- Hardware Commission Date Override: manual configuration of server commission date (`commission_date`) on Host entities to accurately assess Productive Lifespan for New Old Stock (NOS) or refurbished physical hardware with older motherboard BIOS dates.
- Immediate Lifecycle Recalculation: instant re-evaluation of Lifecycle Score and diagnostic breakdown upon updating host commission dates without requiring a new SSH inspection run.
- Commission Date validation: enforces non-future date bounds and prevents dates earlier than the physical motherboard BIOS release date.
- Dedicated quick modal and action shortcut (`openCommissionDateModal`) directly on the Lifecycle Assessment card in Host Detail Workspace Modal.
- Architectural Decision Record [ADR-0024](docs/adr/0024-hardware-commission-date-override.md) and domain term preservation in `CONTEXT.md`.

## [0.15.0] - 2026-09-25
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
