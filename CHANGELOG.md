# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [0.32.1] - 2026-11-09
### Added
- Manual Storage Path Monitoring & Capacity Thresholds (#87):
  - Register custom file or directory paths for ongoing storage monitoring across Linux and Windows hosts.
  - Configurable optional capacity limits with visual progress bars and warning / over-limit threshold alerts.
  - Direct 1-click target pinning from Space Hogs and Top Files directly into storage monitors.
  - Monitored paths injected into Linux and Windows triage probe scripts with dedicated on-demand inspection API endpoints.
- Batch File Deletion Support in File Manager (#86):
  - Multi-select item checkboxes, 'Select All' toggle, and sticky bulk action bar in remote File Manager.
  - Multi-path `/files/delete` API endpoint with path safety checks preventing accidental selection or deletion of protected system paths.

### Fixed
- Storage Triage Monitoring & File Deletion Hardening (#88):
  - Normalized Windows and Unix path keys across storage monitors to prevent duplicate entries and separator mismatches.
  - Process-isolated temporary directories and forced cleanup for Windows PowerShell triage probes.
  - Path control character validation and structured JSON error responses across monitoring APIs.
  - File deletion safety enhancements with path deduplication, execution error checking, and safer path encoding in the web UI.

## [0.32.0] - 2026-11-09
### Added
- Agentless Windows Target Observability via Native OpenSSH (#82):
  - Comprehensive zero-agent monitoring, desired state verification, and management for Windows targets (Windows Server 2019/2022/2025 and Windows 10/11) over native OpenSSH without remote agents or third-party background daemons.
  - In-Band OS Auto-Detection: lightweight non-compound probe (`uname -s` with fallback to `cmd.exe /c ver`) automatically classifying hosts into `linux` or `windows`, persisted to `hosts.os_type`.
  - PowerShell Base64 Encoded Execution: UTF-16LE Base64-encoded execution (`powershellEncodedCmd`) eliminating shell quoting anomalies, newline mangling, and escaping divergence between `cmd.exe` and `powershell.exe`.
  - Core System Telemetry: collects CPU load, RAM usage, System Drive (`C:`) capacity, network interface RX/TX metrics, active TCP listening ports, and top processes by memory and CPU.
  - Native Windows Desired State Evaluators: evaluation of Windows services (`Get-Service`, normalized to `active`/`stopped`), fixed disk capacity (`Win32_LogicalDisk`), TCP listening ports (`Get-NetTCPConnection`), running processes (`Get-Process`), scheduled tasks (`Get-ScheduledTask`), containers (`docker ps`), and backup file freshness (`Get-Item`).
  - Windows OpenSSH Key Provisioning: automated public key placement for standard users (`%USERPROFILE%\.ssh\authorized_keys`) and Administrator accounts (`%ProgramData%\ssh\administrators_authorized_keys`) with strict ACL inheritance and universal SIDs (`*S-1-5-18` SYSTEM and `*S-1-5-32-544` Administrators) using `icacls`.
  - Drive-Aware Path Navigation: seamless path normalization and boundary enforcement across drive letters (`C:/`, `D:/`) and Git Bash paths (`/c/`, `/d/`) in remote File Manager and interactive terminal sessions.
  - Windows Application Probes: built-in CIM telemetry presets for IIS Web Service (`Win32_PerfFormattedData_W3SVC_WebService`) and MSSQL Server General Statistics (`Win32_PerfFormattedData_MSSQLSERVER_SQLServerGeneralStatistics`).
  - EOL Lifecycle Assessment: added Windows 10 (EOL Oct 2025) and Windows Server 2016 (mainstream ended Jan 2022) to lifecycle longevity scoring.
  - Architectural Decision Record: [ADR-0040](docs/adr/0040-agentless-windows-target-observability.md).
- Agentless Windows Storage Triage (PowerShell-Native) (#83):
  - Extended Storage Triage capabilities to Windows target hosts via agentless SSH and UTF-16LE Base64 PowerShell execution without requiring remote agents.
  - Windows Drive & Space Breakdown: native CIM `Win32_LogicalDisk` capacity metrics for System Drive (`C:`), top level-1 directory space hogs discovery via zero-copy `robocopy /L` list-only mode with recursive PowerShell fallback and dynamic non-system directory discovery, and top 15 largest files (>15MB).
  - Single-Pass Runaway File Sensing: unified traversal sensing active log and text files (`.log`, `.txt`, `.err`, `.out`, `.dump`, `.etl`, and rotated `.log.*`) modified within the last 60 minutes and exceeding 15MB, with strict exclusion of database files (`.mdf`, `.ldf`, `.ndf`, `.edb`, `.dit`, `.sqlite*`, `.vhdx`, `.avhd`, `.avhdx`, `.bak`), archives (`.7z`, `.rar`, `.xz`, `.bz2`, `.cab`), and configuration/code assets.
  - Curated Windows Safe Reclaim Catalog: one-click safe cleanup actions for Windows Update Download Cache (`SoftwareDistribution\Download`), System & User Temp (`C:\Windows\Temp`, `%TEMP%`, `C:\Temp`), Error Reporting & Memory Dumps (`WER`, `Minidump`, `CrashDumps`, `MEMORY.DMP`), Recycle Bin (`Clear-RecycleBin`), old IIS access logs (>7 days in `inetpub\logs`), and Docker on Windows.
  - Safe In-Place Windows Log Truncation: zero-byte log emptying via `Clear-Content -LiteralPath '...' -Force` with strict security boundaries protecting Windows system directories (`System32`, `SysWOW64`, `WinSxS`, `Boot`, `Recovery`), swap/pagefiles (`pagefile.sys`, `swapfile.sys`, `hiberfil.sys`, `dumpstack.log`), directories, and database files.
  - Graceful Windows Service Reload: remediation hook using `Restart-Service -Name '...' -Force` with strict alphanumeric command injection validation.
  - Architectural Decision Record: [ADR-0039 Section 7](docs/adr/0039-agentless-storage-triage-runaway-detection-and-safe-reclaim.md).
- Rendered Markdown Preview for Host Notes & Global Notes (#77):
  - Display rendered Markdown preview by default when opening host or global note dialogs, with in-place Edit button to toggle editing mode and return to preview immediately upon save.
  - Zero-dependency XSS-safe Markdown micro-parser for headings, code blocks, lists, bold/italic, and links.
  - Expanded modal width to `modal-dialog-lg` with comfortable minimum height (240px).

### Fixed
- Windows Storage Triage Reliability & Process Cleanup (#85):
  - Refactored SSH command execution timeout with robust channel-based cancellation preventing hung sessions.
  - Optimized PowerShell storage triage probe with dynamic depth limits, PID tracking, and scratch path management to prevent recursion and process leakage.
  - Process tree cleanup terminating lingering robocopy and child PowerShell probes upon timeout or cancellation.
  - Cleaned CLIXML header artifacts from PowerShell output streams and enhanced ACL elevation handling during log truncation.

## [0.31.1] - 2026-11-04
### Added
- Host Switcher & Telemetry Shortcut: quick metrics shortcut button on host cards and table rows, host switcher dropdown and prev/next controls in host details modal, and keyboard navigation shortcuts (`[` and `]`) for switching hosts. (#81)

### Fixed
- Host List Action Button CSS & Alignment: synchronized compact button height (24px) for `.btn-host-metrics` alongside `.btn-host-view` and `.btn-host-terminal`.

## [0.31.0] - 2026-11-03
### Added
- Agentless Storage Triage & Runaway Detection: on-demand diagnostic and remediation suite for diagnosing disk saturation, uncovering space hogs, and safely recovering storage capacity over SSH without remote agents.
  - Tiered Storage Discovery: 1st-level directory space breakdown (`du -kx --max-depth=1`), top large files (>30MB), and detection of active runaway files modified within 60 minutes correlated with Disk I/O Write Rate.
  - Ghost File Detection: identifies unlinked deleted file descriptors still held open by running processes (`lsof +L1` and `/proc/*/fd` inspection), displaying process name, PID, held bytes, and one-click graceful service reload.
  - Curated Reclaimable Storage Catalog: distro-aware detection and one-click remediation for systemd journals (`journalctl --vacuum-time=3d`), package manager caches (APT, DNF, Pacman, APK), old rotated logs (`/var/log/*.gz`, `*.1`), coredumps, and Docker system prune.
  - Safe Log Truncation: in-place 0-byte truncation (`truncate -s 0 <path>`) for runaway log files to prevent ghost files and protect application file descriptors, with strict protected system path rejection.
  - Triage Concurrency Guard & Timeout Protection: singleflight lock per host preventing parallel disk scan thrashing and 20s timeout wrapper mitigating hung network/NFS mounts.
  - Host Detail Storage Triage Tab: interactive 4-panel visual dashboard with partition usage bars, reclaimable storage summaries, alert badges, and directory navigation links.
  - Alert Banner & Overview Deep Links: direct one-click triage navigation from disk saturation alerts in Alert Banner and Root Storage widget in Overview tab.
  - Bilingual i18n Localization: full bilingual support (Indonesian & English) integrated with language switcher across all Storage Triage components, modals, alerts, and tooltips.
  - Local & LAN Network Binding: automatic LAN IP discovery and CLI banner display (`-host` / `PANTAU_HOST` env support) allowing seamless browser access from network devices.
  - Storage Triage Load-Aware Throttling & Cooldown Guard: protects remote hosts from disk I/O saturation and self-inflicted DoS through in-memory tiered rate limiting and telemetry load sensing.
    - Tiered Cooldowns: enforces 2-minute cooldown on healthy hosts, 5-minute cooldown on busy hosts (Disk Write 15-40 MB/s), and blocks scans on very busy hosts (Disk Write >40 MB/s or CPU Load >2.0x core count).
    - Backend API Enforcement: `GET /api/hosts/{id}/storage-triage` rejects premature scans with HTTP 429 Too Many Requests, returning remaining cooldown seconds and load metrics. Dedicated `/api/hosts/{id}/storage-triage-throttle` endpoint returns real-time permission status.
    - Emergency Force Scan: supports `?force=true` override parameter with risk confirmation modal in the UI allowing operators to bypass limits during critical incidents.
    - Interactive Countdown & Load Badge: dynamic button countdown timer (`Tersedia dlm 01:45`), load state badges (`🟢 Normal`, `🟡 Sibuk`, `🔴 Sangat Sibuk`), and full bilingual localization.
  - Architectural Decision Record: [ADR-0039](docs/adr/0039-agentless-storage-triage-runaway-detection-and-safe-reclaim.md).

### Changed
- Pure On-Demand Storage Triage: removed automatic scan execution on disk saturation >= 85% in `renderStorageTriageTab`, ensuring triage probes run purely when explicitly triggered via 'Run Storage Triage' to prevent unexpected remote SSH disk I/O load.
- Storage Triage Snapshot Display: implemented persistent snapshot view when opening the Storage Triage tab. Displays the most recent scan snapshot immediately (backed by in-memory cache and new `/api/hosts/{id}/storage-triage-latest` backend API) with clean timestamp badge (`Dipindai: [Waktu]`) and direct one-click rescan button, without any intrusive disclaimer cards.
- Docker System Prune Full Image Cleanup: updated `docker system prune` command from `-f` to `-af` in `ValidateReclaimRequest` and `ReclaimItem.Command`. Docker's default prune without `-a` only removes untagged (dangling) images, causing unused tagged images to remain untouched (0B reclaimed). `-af` thoroughly removes all unused images, inactive containers, networks, and build cache safely without touching persistent docker volumes.
- Automatic Throttle Reset on Reclaim: automatically clears scan cooldown when a safe reclaim action succeeds, allowing immediate post-remediation rescanning to reflect recovered disk space.

### Fixed
- Storage Triage Force Scan Modal Stacking: fixed modal overlay appearing behind the active Host Detail modal by dynamically allocating `z-index` above `modalZIndexCounter` (`>= 200000`).
- Storage Triage Directory & File Navigation: fixed issue where clicking on top level-1 directories (e.g. `/var`, `/tmp`) or files in File Manager or Terminal opened `/` (root) instead of the actual target directory. `openFileManagerAtPath` now sets `_lastPath` before switching tabs, and `openTerminalForPath` strips filenames to target the enclosing directory cleanly with enhanced PTY readiness delay.
- Storage Triage Zsh Word-Splitting & Probe Resiliency: added `setopt SH_WORD_SPLIT` for hosts where the default SSH user shell is Zsh, resolving an issue where unquoted variable expansions (`$RUN_TIMED`) failed silently with `command not found: timeout 14s stdbuf -oL` causing Top level-1 directory and Top 15 largest files cards to return empty.
- Storage Triage Per-Directory Traversal: replaced monolithic multi-directory `du` execution with isolated per-directory iteration and cache pruning (`.cache`, `.npm`, `go/pkg/mod`, `.cargo`), ensuring large or slow directory trees do not starve other root partitions or exceed probe timeouts.
- Storage Triage Scanning State Resilience: fixed race condition where `refreshActiveHostDetailQuietly` background polling every 1.5s called `switchTab` and wiped the active scanning spinner/prompt back to idle while the SSH triage probe was still running in the background.
- Scoped Host Detail Fast Poll: restricted `refreshActiveHostDetailQuietly` to only update tabs backed by `/api/hosts/{id}` payload (`overview`, `network`, `docker`, `timeline`, `runs`, `desired`), preserving interactive client-side tabs (`storage`, `files`, `metrics`, `netdiag`, `packages`).
- Triage Scan In-Flight Rendering: updated `renderStorageTriageTab` to detect active scan state and render the running spinner, active SSH command card, and disabled button across tab switches and language toggles.
- Path Traversal & System Protection in Log Truncation: sanitized paths using `path.Clean`, strictly rejected relative traversal segments (`..`), and blocked truncation against root and critical system directories (`/etc`, `/usr`, `/boot`, `/lib`, `/dev`, `/proc`, `/sys`).
- Ghost File Size & Path Detection: fixed procfs fallback to compute unlinked file descriptor byte sizes accurately using `stat -L -c %s "$fd"` and resolved `lsof` awk parsing dropping the original path when handling `(deleted)` status tags.
- Space Hogs Directory Name Whitespace: preserved directory names containing spaces during POSIX `du` output parsing.
- IPv6 Socket Binding in Port Auto-Scan: used `net.JoinHostPort` in `tryListen` preventing `too many colons in address` parse errors when binding IPv6 addresses.
- WebSocket Terminal Initial Directory Path: preserved full directory names containing dot characters when launching terminal sessions into specific folder paths.
- Inline Handler Special Character Escaping: safeguarded inline `onclick` arguments using URI encoding/decoding preventing syntax errors on complex hostnames or directory paths.

## [0.30.0] - 2026-11-02
### Added
- Zero-Trust Public Status Page & Uptime Rollups: public-facing standalone status page (`/status`) with strict zero-trust boundary isolating internal infrastructure topology and sensitive server telemetry from public visitors.
  - Public & Admin Data Isolation: public status endpoints exclusively expose explicit opt-in targets with customizable display names and masked IDs, completely hiding internal host IDs, hostnames, IP addresses, credentials, and probe commands.
  - Per-IP Rate Limiting: in-memory token bucket rate limiter protecting public endpoints against denial-of-service and enumeration scanning.
  - In-Memory Snapshot Cache: background-refreshed cached JSON snapshots preventing public traffic spikes from saturating SQLite database locks.
  - Daily Uptime Rollup Engine: daily aggregation worker computing historical uptime percentages and incident duration across 90-day rolling windows.
  - Interactive 90-Day Uptime Visualization: SVG/CSS interactive uptime bars for each published service target with tooltip hover states and day-by-day availability logs.
  - Incident Management System: administrative workflow to publish, update, resolve, and manage service maintenance announcements and outage incidents.
  - Architectural Decision Record: [ADR-0038](docs/adr/0038-public-status-page-zero-trust-isolation-and-uptime-rollups.md).
- Extended Metric Observability Ranges:
  - 30-Day Metric Range: preset time filter with dynamic 2-hour downsampling buckets in API and metrics tab.
  - Custom Date Range Picker: custom start and end timestamp filters (date down to hour:minute) with automatic bucket downsampling.
  - Disk Chart Clamping: fixed Y-axis scale to [0, 100] for Disk Storage Capacity charts to eliminate micro-variation visual jitter.

### Changed
- Settings Modal Layout: modernized settings dialog from wrapped horizontal tabs into an ergonomic split sidebar layout with a 200px vertical navigation rail and 1040px wide modal canvas, automatically adapting to a swipeable horizontal tab bar on mobile viewports (<768px).

## [0.29.0] - 2026-10-30
### Added
- Time-Series Metric Observability & Application Telemetry: continuous agentless recording of system resource metrics and application performance telemetry into embedded SQLite with configurable rolling retention.
  - SQLite Time-Series Engine: flat table `metric_samples` with composite index `(host_id, metric, recorded_at)` and index `(recorded_at)` for high-performance range queries and rapid data pruning.
  - Automatic Rolling Retention & Prune Worker: hourly background worker (`startMetricsPruneWorker`) automatically enforcing retention policy (`metric_retention_days`, default 7 days) without manual intervention.
  - Tri-State Collection Controls: global toggle (`metrics_enabled`) and per-host override (`hosts.metrics_enabled`: `-1` Inherit Global, `1` Force ON, `0` Force OFF).
  - Agentless Application Metric Probes: execution of custom and preset application metric probes combined into existing SSH inspection sessions, with isolated subshell timeouts (`timeout 3s`) and graceful silent fallback.
  - Built-in Probe Presets: ready-to-use presets with auto-detection for Nginx Stub Status, MySQL Status (`mysqladmin`), PHP-FPM Status (JSON & native ps fallback), Redis Info (`redis-cli` with auth/socket support), and PostgreSQL Stats (`pg_stat_activity` with peer auth).
  - High-Performance Columnar Time-Series Query API: `GET /api/hosts/{id}/metrics?range=24h&category=all|hardware|applications` returning columnar arrays with dynamic SQL bucketing downsampling (`step=0` for 1h/6h, `step=300s` for 24h, `step=1800s` for 7d).
  - Dual-Tier Zero-Dependency Frontend Visualization:
    - Host Detail Modal `[📊 Metrics & Telemetry]` tab powered by locally vendored **uPlot** (`~48 KB` minified, HTML5 Canvas 2D, zero CDN dependencies) with synchronized crosshair cursor (`uPlot.sync()`), zoom/pan, time-range selectors (1h, 6h, 24h, 7d), and category toggles.
    - Handcrafted pure SVG mini-sparklines (~15 lines vanilla JS, zero dependencies) on Host Cards for instantaneous 1-hour CPU and RAM trends.
  - Probe Manager UI: modal interface for creating, editing, testing, and toggling custom probes and adopting presets.
- Markdown Preview Modal for Host and Global Notes: rendered Markdown preview by default with in-place edit toggle and zero-dependency micro-parser for rich text formatting in host and global notes.
- Architectural Decision Record: [ADR-0037](docs/adr/0037-metric-samples-application-telemetry-and-uplot-observability.md).

### Changed
- Dashboard Header: removed redundant active alert badge cluster from fixed header to streamline visual focus on the primary alert banner.

## [0.28.1] - 2026-10-29
### Fixed
- Native Terminal Launch on Windows Terminal: avoid command splitting error 2147942402 (0x80070002) by replacing semicolon chaining in remote `cd` navigation with POSIX-standard `|| true &&`, and escaping semicolons as `\;` when launching Windows Terminal (`wt.exe`).

## [0.28.0] - 2026-10-29
### Added
- Native Terminal Bridge: feature to launch OS native terminal emulators (Windows Terminal, GNOME Terminal, iTerm2, Alacritty, etc.) connected to monitored Linux hosts without re-entering passwords.
  - Ephemeral RSA 2048-bit SSH Keypair Injection: in-memory key generation without touching Pantau server disk, universal compatibility with legacy OpenSSH 5.3+ (CentOS 6) and modern servers, atomic append to `~/.ssh/authorized_keys` with unique comment metadata, and two-tier auto-cleanup (proactive Go timer and reactive sweep via POSIX `awk`).
  - Remote Web Launcher Script & Command Generator: single-use download token (burn-after-read, 60s TTL) serving self-extracting `.cmd` (Windows with user-profile key isolation) and `.command` / `.sh` (macOS/Linux with `trap` cleanup), plus streaming one-click command modal.
  - Custom URL Protocol (`pantau://`): seamless 1-click terminal launch directly from browser without downloading files or manual copy-pasting, executing in-memory via PowerShell/bash to pass antivirus heuristics.
  - OS Terminal Detection & Interactive Shell Retention: automatic cascade detection across Windows, macOS, and Linux with interactive shell chaining (`exec bash`, `cmd /k`, `powershell -NoExit`) so terminal windows remain open and interactive if SSH fails or disconnects.
  - Process Detachment: non-blocking detached process spawning in Go (`Setpgid` on POSIX, `CREATE_NEW_PROCESS_GROUP | DETACHED_PROCESS` on Windows).
  - Terminal Settings & Custom Template: support for power-user terminal command templates (e.g. `alacritty -e %cmd%`) with pre-flight binary lookpath and command injection prevention.
  - UI Integrations: Native Terminal button on Terminal Toolbar and "Native Terminal" dropdown item on Host Cards (Grid and List views), dedicated "Native Terminal" action in SFTP File Manager toolbar, and quick native terminal launch for pinned files/folders in Global Bookmarks modal.
  - Active Path Navigation: automatic synchronization of terminal active working directory (OSC 7 / prompt tracking in Terminal Tabs, current folder in SFTP File Manager, or target directory of pinned bookmarks) when opening native desktop terminals, with graceful fallback to user home directory.
- Architectural Decision Record: [ADR-0036](docs/adr/0036-native-terminal-bridge-and-ephemeral-key-injector.md).

## [0.27.0] - 2026-10-27
### Added
- Flat Host View Option ("No Grouping"): explicit filter option to display all registered hosts in a flat list or card grid without group headers.

### Changed
- Streamlined Host List View: optimized table width from 12 columns to 7–8 columns for higher scannability and reduced visual clutter; merged host address and OS info into a two-line cell, displayed operational notes as inline badges with direct edit access, removed redundant traffic throughput columns, and enforced `white-space: nowrap` on metric columns.
- Terminal Button Badges & Action Ergonomics: applied fixed dimensions and corner overlay counter badges to terminal buttons across headers and cards, standardized high-contrast badge colors, and relocated manual host refresh trigger to the action menu.

### Fixed
- Remote SSH Process & Tunnel Connection Leaks: immediate SSH runner abortion upon WebSocket disconnection for interactive terminals and container log streaming to terminate orphaned remote processes (e.g. `top`, `tail`, `docker logs`), added background idle reaper for remote port tunnels, and handled cancellation during directory streaming downloads.
- Action Dropdown Menu Header Clipping: added dynamic viewport collision detection for host list action dropdowns to toggle opening upward or downward, preventing menu items from clipping behind the fixed header and toolbar.

## [0.26.0] - 2026-10-26
### Added
- Pre-flight SSH Connection Testing: ad-hoc connection test endpoint `POST /api/hosts/test-connection` and `⚡ Test Connection` button in the host registration modal verifying host address, SSH port, authentication (one-time password or private key), and remote architecture (`uname -srm; uptime`) prior to saving.
- Immediate Asynchronous Initial Inspection: automated dispatch of single-host background inspection immediately upon host creation via `POST /api/hosts`, eliminating the 30-second delay previously caused by the periodic worker sweep ticker.
- Realtime Onboarding Fast-Poll Engine: client-side reactive polling loop (`scheduleFastPollIfNeeded`) executing at 1.5-second intervals until the newly registered host completes its initial inspection, auto-deactivating immediately once Actual State metrics arrive.
- Transparent "Initial Sync" Card & Detail States: distinct loading indicators and informative status messages for newly registered hosts (`last_inspected == nil`) on both Grid and List views, eliminating the previous confusion of static 0% metrics.
- Synchronized Docker & Overview Empty States: intelligent detection of in-progress initial sync in Host Detail tabs (Overview, Docker, Services), preventing false "No containers found" empty states while discovery is actively running.
- Network Diagnostic Probe Target Presets: domain entity to save unmanaged routers and remote endpoints (`internal/store`), backed by SQLite with seed defaults, snapshot export/import, and REST API endpoints (`/api/netdiag/presets`) with IPv4/IPv6 and FQDN validation. Features a 3-way target selector, optgroup preset dropdown, preset manager modal, and quick bookmarking in the web UI.
- Automated Network Triage Matrix: diagnostic triage pipeline executing host self-checks, route discovery, and MTU sweep tests with deterministic verdict classification across 7 status categories (`HEALTHY`, `HIGH_LATENCY`, `LOCAL_GATEWAY_DOWN`, `UPSTREAM_DROP`, `DESTINATION_UNREACHABLE`, `MTU_BLACK_HOLE`, `DNS_FAILURE`), background auto-triage scheduler, triage notification alerts, and SQLite incident storage.
- Markdown Incident Ticket Generator: one-click copy and export of comprehensive incident triage reports formatted in Markdown for ticketing systems and escalation channels.
- Architectural Decision Record: [ADR-0035](docs/adr/0035-automated-branch-triage-matrix-and-hybrid-network-observability.md).

### Changed
- Network Diagnostics Workspace Modal: promoted netdiag dialog to a standardized workspace modal (85vw × 84vh) with fixed top-anchored alignment, eliminating cumulative layout shifts (CLS), and internal scrolling across probe and triage views.
- Host Modal Layout: refactored host modal into a 2-column layout with 3-tier vertical credential fields.

### Fixed
- Duplicate Host Registration Prevention: reject host creation and updates with duplicate IP/domain and port combinations with a 409 Conflict, providing bilingual error feedback (EN/ID).
- Network Diagnostics Hop & Latency Wrapping: enforce `white-space: nowrap` on hop badges, status, latency, and DNS IP tags, truncate long FQDN hostnames with tooltip titles, and preserve raw terminal output formatting.

## [0.25.0] - 2026-10-21
### Added
- Interactive Terminal Link Detection: native `term.registerLinkProvider` integration in xterm.js matching RFC 3986 URLs and remote loopback addresses, with `Ctrl+Click` (Linux/Windows) and `Cmd+Click` (macOS) interaction to prevent accidental terminal text selection conflicts.
- Remote Web Port Tunneling: agentless HTTP and WebSocket reverse proxy (`/api/tunnel/:hostId/:port/*`) streaming over OpenSSH RFC 4254 `direct-tcpip` channels (`golang.org/x/crypto/ssh` `DialContext`) without requiring public firewall port openings or remote daemons.
- High-Performance Connection Pooling: per-host SSH transport caching eliminating 100–300ms connection handshakes across sequential asset requests (CSS, JS, images), featuring transparent auto-reconnect fallback on idle connection dropouts.
- Native WebSocket Hijacking: full bidirectional streaming over SSH channels supporting protocol status `101 Switching Protocols` for real-time development servers and interactive web services.
- SSRF & Port Security Enforcement: strict hardcoded remote loopback binding (`127.0.0.1:<port>`), mandatory `pantau_session` authentication, port range boundaries (1-65535), and blacklisting of the target host's own active SSH port.
- Interactive Remote Web Service Modal: confirmation dialog on clicking remote localhost URLs in terminal output with 1-click tunnel launch and link copying.
- Terminal Ports Dropdown Menu: dedicated `🌐 Ports` toolbar menu in the terminal dock for manual port forwarding entry and persistent recent port history (`localStorage`).
- Zero-Configuration Remote Browser Bridge: automated OSC 1337 (`OpenURL`) and OSC 777 escape sequence receiver routing CLI tool browser requests (`gh auth login`, `xdg-open`, Python `webbrowser`, AWS SSO, `npm login`) directly to the client's desktop browser without headless text-browser (`w3m`/`lynx`) traps.
- Silent Shell Environment Auto-Provisioning: non-intrusive provisioning of `/tmp/pantau-browser` and silent environment injection (`BROWSER`, `GH_BROWSER`) across login shells with zero console echo.
- Architectural Decision Record: [ADR-0034](docs/adr/0034-terminal-link-detection-remote-browser-bridge-and-port-tunnel.md).

## [0.24.0] - 2026-10-16
### Added
- Live Search in Terminal Presets: real-time search filtering in the terminal presets dropdown by preset title, command, or tmux session tag (`t:tag`), enabling instant command dispatch across extensive preset catalogs.
- Terminal Pinned Paths Dropdown: quick-access dropdown (`📌`) in the terminal toolbar displaying all pinned files and directories for the active host with real-time query filtering by path and operator notes.
- Instant Path Actions: single-click directory navigation (`cd '<path>'`) and quoted path injection into the active terminal shell session without executing extraneous commands.
- Quick Jump to File Manager: one-click toolbar button (`📁 Files`) that switches context seamlessly from the active terminal tab to the Host File Manager focused on the current working directory.
- Working Directory Synchronization: automated tracking of remote shell working directory via terminal window titles and OSC 7 ANSI escape sequences.

## [0.23.0] - 2026-10-16
### Added
- Multi-Host Side-by-Side Comparison: interactive evaluation matrix comparing 2 to 4 hosts simultaneously inside a Workspace Modal (75vw × 80vh) without additional backend endpoints.
- Best-in-Class Head-to-Head Scoring: transparent scoring across 4 standardized dimensions (Compute, Storage & I/O, Network Health, System Hygiene & Lifecycle) with a 5% deadband noise filter and soft penalty for unreachable/stale hosts.
- Executive Decision Narrative Generator: automated decision recommendation synthesis highlighting winner strengths, workload placement recommendations, and risk warnings for saturated hosts.
- Markdown Report Export: one-click copy to clipboard formatting full comparative scoreboard, standings, and takeaway summary for documentation and stakeholder meetings.
- Global Header Launcher: dedicated ⚖️ Host Comparison button in header navigation cluster.
- File Pinning & Operational Notes: pin mission-critical configuration files and log directories per host with custom operational notes and note badges in file manager.
- Quick Access Bar: visual pinned toolbar positioned above file manager directory listing for instant 1-click navigation to pinned files and folders.
- Global Bookmarks Modal: dedicated `🔖 Bookmarks` modal accessible globally from header navigation cluster with real-time search, file viewer/editor launcher, inline note editing, and one-click Terminal Dock launch into parent directories.
- File Pins Persistence & REST API: backed by SQLite `host_file_pins` table and dedicated endpoints (`GET/POST/DELETE /api/hosts/{id}/pins`, `GET /api/pins`) with automatic cascade cleanup on host removal.

## [0.22.0] - 2026-10-15
### Added
- Agentless Network Diagnostics: on-demand diagnostic probe tools over SSH (Ping, Traceroute with intelligent tracepath fallback, TCP Port Probe, and DNS Resolve) with real-time Server-Sent Events (SSE) streaming.
- Intelligent Hop Analysis: heuristic categorization of network hops into Local/Private VPC, ISP Transit, and Target Destination zones, with automated blackhole packet drop pinpointing and latency spike alerts (>60ms).
- Zero-Dependency TCP Port Probe: direct `/dev/tcp/<target>/<port>` socket testing in remote bash, eliminating external utility dependencies like `nc`, `telnet`, or `nmap`.
- Singleflight Concurrency Guard: per-host diagnostic locking using `sync.Map` to prevent command stacking and redundant SSH connection exhaustion.
- Dual Access UI: dedicated "Network Diagnostics" tab in Host Workspace Modal and global quick-access launcher in header navbar cluster with offline target protection.
- Architectural Decision Record: [ADR-0033](docs/adr/0033-agentless-network-diagnostics-and-hop-analysis.md).

## [0.21.0] - 2026-10-15
### Added
- Process Telemetry: real-time agentless extraction of top 5 resource-consuming processes (Top CPU, Top RAM/RSS, and Top Swap) on every inspection cycle without remote daemon overhead.
- Top Resource Consumers Widget: instant multi-column process observability card in the Host Detail Overview tab with direct one-click Terminal Dock launcher.
- Agentless Package Inventory: polyglot multi-distro software inventory discovery across Debian/Ubuntu (`dpkg`), RHEL/CentOS/AlmaLinux/Rocky (`rpm`), Alpine (`apk`), and Arch (`pacman`).
- Software Hygiene & Dormant Package Detection: automatic classification of Manual Install vs Base System packages and heuristic detection of dormant packages with inactive/failed/disabled systemd services.
- Packages Tab in Workspace Modal: dedicated searchable package management tab with instant filtering (`Manual Install`, `Dormant Packages`, `All Packages`), disk size sorting, and on-demand rescan trigger.
- Architectural Decision Record: [ADR-0032](docs/adr/0032-process-telemetry-and-package-inventory.md).
- TOFU (Trust On First Use) Host Key Verification: implemented `TOFUHostKeyCallback` in SSH runner, automatically accepting host keys upon initial connection while strictly verifying fingerprints on subsequent connections to prevent active Machine-in-the-Middle (MITM) attacks.
- OpenSSH 4.x & CentOS 5 Host Key Support: added `ssh-dss` (`ssh.KeyAlgoDSA`) to supported host key algorithms in `configureLegacyCompatibleSSH` alongside `ssh-rsa` and legacy ciphers/kex (`3des-cbc`, `diffie-hellman-group1-sha1`).
- Universal POSIX Single-Quoting (`shellQuote`): standard Bourne shell escaping (`'...'`) across all remote command interpolations (file manager, cross-host transfer, container inspection, service diagnostics, and backup freshness), eliminating command injection vulnerabilities across all Linux kernel and Bash versions.
- Architectural Decision Record: [ADR-0031](docs/adr/0031-security-hardening-and-legacy-os-compatibility.md).

### Changed
- CLI Console Log Level & Noise Reduction: defaulted terminal console output to `ERROR` level (`-log-level=error`), keeping runtime CLI output completely quiet while preserving full live telemetry in Web UI Activity Console.
- CLI Logging Flags: added `-log-level` (`PANTAU_LOG_LEVEL`) supporting `debug`, `info`, `warn`, `error`, `off` and `-q`/`-quiet` shorthand for error-only output.
- Startup & Shutdown Log Deduplication: removed redundant standard library `log.Printf` statements that duplicated `logger.Info` events at startup, shutdown, and scheduled backup.

### Fixed
- Swap Telemetry Overhead: conditioned per-process swap probe strictly on actual swap utilization (`SwapTotal - SwapFree > 0`), eliminating `/proc/*/status` scanning on zero-swap hosts.
- Multi-Distro Package Inventory: expanded package parser to support 3-column outputs for Alpine Linux (`apk`) and Arch Linux (`pacman`) with human-readable unit conversion.
- Systemd Service Status Parsing: handled bullet and symbol prefixes (`●`, `*`) in `systemctl list-units` to guarantee accurate dormant package identification for failed or inactive units.

### Security
- WebSocket Authentication & CSWSH Protection: wrapped `/ws/terminal` and `/ws/docker/logs` with `authMiddleware` and enforced same-origin matching (`strings.EqualFold(u.Host, r.Host)`) in `websocket.Upgrader.CheckOrigin`, preventing Cross-Site WebSocket Hijacking while preserving full plain HTTP accessibility.
- Cryptographic Token Generation (`crypto/rand`): replaced timestamp-based token derivation with 32-byte hex cryptographic random tokens in `issueSession` and 16-byte hex tokens for 2FA challenges. Retained `Secure: false` to allow seamless operation over plain HTTP and private LAN IPs.
- Container Name Regex Sanitization: enforced strict regex validation (`^[a-zA-Z0-9][a-zA-Z0-9_.-]+$`) and shell quoting on container parameters in `/ws/docker/logs` and inspector actions.
- Desired Rule Metacharacter Filtering: added strict rejection of shell metacharacters (`;`, `&`, `|`, `$`, `` ` ``, `\n`, `\r`) in `POST /api/hosts/{id}/rules` and port number range validation (`1-65535`).
- Bootstrap & Snapshot Protection: updated `IsFresh()` in store to verify whether `admin_password_hash` has been customized from default, preventing unauthenticated callers from triggering snapshot import or database wipe on initialized instances.

## [0.20.0] - 2026-10-14
### Added
- In-App Activity Console & Live Event Stream: real-time in-app visibility of all daemon and subsystem runtime activities (`inspect`, `ssh`, `transfer`, `terminal`, `snapshot`, `system`, and `auth`) delivered via zero-dependency Go stdlib `log/slog` and Server-Sent Events (`GET /api/logs/stream`).
- Telemetry Ring Buffer: in-memory fixed-capacity ring buffer (100 records) and non-blocking broadcaster providing instant context upon client connection.
- Redaction Filter: deterministic boundary filter automatically sanitizing SSH passwords, private keys, authentication tokens, passphrases, and 2FA secrets into `[REDACTED]`.
- Interactive Activity Console Drawer: collapsible bottom drawer docked above footer with 5-column running table (`Time`, `Level`, `Module`, `Host`, `Message & Details`), Smart Auto-Scroll, pause/resume freeze toggle, severity level filtering (`ALL`, `ERROR`, `WARN`, `INFO`), instant search, and client-side `.log` / `.json` export.
- Footer Status Trigger & Shortcut: footer button `⚡ Activity` with live pulse indicator, unread error badge counter, and global keyboard shortcut `Ctrl + \`` (backtick).
- Rendering Throttling: 100% skipped DOM rendering when drawer is closed and coalesced updates via `requestAnimationFrame` when open to eliminate CPU waste and layout thrashing.
- CLI Update Checker Release Notes Display: `pantau -check-update` displays formatted release notes extracted from upstream release payload in terminal output.
- Automated Release Notes Extraction: GitHub Actions release workflow (`release.yml`) automatically extracts tag-specific release notes directly from `CHANGELOG.md` for GitHub Releases distribution.
- Architectural Decision Record: [ADR-0030](docs/adr/0030-in-app-activity-console-and-live-event-stream.md).
### Fixed
- Instance Guard Graceful Handover: added `AcquireWithTimeout` (up to 3 seconds retry window) and explicit lock cleanup/database closure on server shutdown to allow graceful lock handover during restart without premature child exit.
### Removed
- Project Sponsorship Elements: removed Saweria funding badge, sponsor button in footer, and Settings sponsorship card to maintain a clean, distraction-free interface.


## [0.19.1] - 2026-10-12
### Fixed
- Port Auto-Scan IPv4 Loopback Probe: actively probe `127.0.0.1` via `net.DialTimeout` and verify IPv4 loopback binding (`tcp4`) before binding wildcard listeners, preventing dual-stack IPv6 phantom bindings on Windows Winsock when an existing IPv4 service occupies the port.

## [0.19.0] - 2026-10-11
### Added
- Real-Time Disk I/O Observability & Speedometer: periodic extraction of host-level disk read/write throughput rates (bytes/s) via agentless POSIX `/proc/diskstats` whitelist filtering (`sd*`, `vd*`, `nvme*n*`, `xvd*`, `mmcblk*`) multiplied by 512-byte sector size.
- Reboot Protection & Counter Rollover Guard: automatically detects host reboot/rollover counter drops (`delta < 0`), resetting rate to 0 bps to prevent erroneous negative spikes and establishing the new reading as baseline.
- Disk I/O Load Status Badges: qualitative color-coded throughput status pills (Optimal `< 10 MB/s`, Active `10–80 MB/s`, High I/O Load `≥ 80 MB/s`) on Host Cards, Table View, and Host Detail modals with bilingual support (EN/ID).
- Architectural Decision Record: [ADR-0029](docs/adr/0029-realtime-disk-io-observability-and-speedometer.md).

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
