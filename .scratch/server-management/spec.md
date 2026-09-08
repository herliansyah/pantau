# Spec: Pantau — Agentless Server Monitoring & Management

**Status:** ready-for-agent

## Problem Statement

System administrators and developers managing multiple Linux VPS/servers lack a single, lightweight, zero-overhead tool to monitor server health, verify whether services and backups actually match expected conditions, diagnose failures immediately with root cause context, and manage files or terminal sessions directly from one place without heavy agent installation.

Existing tools are either too heavy (Prometheus/Grafana/Zabbix requiring agents and megabytes of RAM), fragmented (separate uptime monitor, separate Portainer for Docker, separate SSH client/terminal), or merely notify that a service is down without explaining why (missing exit code, logs, and drift cause).

## Solution

**Pantau** is a self-hosted, single-binary Go application providing agentless server management and monitoring over standard SSH. It periodically inspects registered Hosts, compares their **Actual State** against a declarative **Desired State** (auto-configured with a 1-click baseline), detects **Drift**, automatically captures a **Root Cause Excerpt** (error logs & exit codes), calculates a **Lifecycle Score** (hardware & OS retirement suitability), and provides hybrid alerting (in-app + Telegram/Webhook). Additionally, Pantau provides integrated interactive management tools: a browser-based Web SSH Terminal (xterm.js), an SFTP File Manager, a lightweight code/text editor with syntax highlighting for config and PHP files, and direct Docker container action controls.

## User Stories

1. As an administrator, I want to add a new Host by entering its hostname/IP, SSH port, and credentials, so that Pantau can establish an agentless SSH management session.
2. As an administrator, I want Pantau to generate a default SSH public key, so that I can easily copy it to `~/.ssh/authorized_keys` on any target Host.
3. As an administrator, I want to override SSH keys or credentials per Host, so that I can support hosts with unique keys or custom SSH ports.
4. As an administrator, I want Pantau to auto-detect the current state (running Docker containers, disk mounts, cron jobs, DB services) on initial connection, so that I can generate a Desired State baseline with a single click.
5. As an administrator, I want to toggle and customize rules in the Desired State checklist per Host, so that I define exactly what must stay alive or within thresholds.
6. As an administrator, I want Pantau to execute periodic inspections via SSH without installing any agent daemon on the target server, so that target servers remain clean.
7. As an administrator, I want to view Host OS details, kernel, system uptime, CPU load average, RAM usage, and disk space usage in a clear dashboard.
8. As an administrator, I want to monitor Docker containers on each Host, so that I know which containers are healthy, running, stopped, or restarting.
9. As an administrator, I want to execute quick actions (Start, Stop, Restart) on Docker containers from the UI, so that I can resolve container issues without manually opening a shell.
10. As an administrator, I want to stream live Docker container logs in the web interface, so that I can quickly debug runtime application issues.
11. As an administrator, I want to monitor database process availability (MySQL, PostgreSQL, etc. via systemd or Docker), so that I know immediately if a database goes down.
12. As an administrator, I want to monitor Backup Freshness by checking target file existence, size (> 0 bytes), and modification timestamp (< 24h), so that I can be certain backups are actually running and producing valid dumps.
13. As an administrator, I want to monitor crontab schedules, so that I am alerted if a scheduled task fails to register or run.
14. As an administrator, I want Pantau to automatically detect Drift whenever the Actual State violates the Desired State, so that unexpected changes are surfaced immediately.
15. As an administrator, I want Pantau to automatically capture a Root Cause Excerpt (container exit code, OOMKilled flag, last 50 lines of journalctl or container logs) whenever Drift is detected, so that I know *why* a failure occurred without manual triage.
16. As an administrator, I want to view a timeline log of state transitions and drift causes per Host, so that I have a reliable audit trail of past incidents.
17. As an administrator, I want to see a Lifecycle Score (0-100) for each Host based on OS EOL status, sustained load, and disk/hardware error indicators, so that I know when a server is ripe for hardware replacement or OS upgrade.
18. As an administrator, I want to open an interactive in-browser Web Terminal (xterm.js via WebSocket) to any Host, so that I can run terminal commands instantly without leaving the browser.
19. As an administrator, I want to browse directories and manage files on the Host via SFTP in the web interface, so that I can upload, download, rename, chmod, or delete files.
20. As an administrator, I want to edit configuration files, PHP scripts, and bash scripts directly in a lightweight web editor with syntax highlighting, so that I can make quick fixes on the server cleanly.
21. As an administrator, I want to receive instant drift alerts via Telegram Bot or Webhook, so that I am alerted to outages even when I am away from the web dashboard.
22. As an administrator, I want to see real-time alert badges in the web UI, so that active issues are prominently highlighted.
23. As an administrator, I want to run Pantau as a single binary with zero external service dependencies (embedded SQLite + embedded web assets), so that deployment and maintenance require minimal effort.

## Implementation Decisions

- **Single-Binary Go Application**: Built in Go using standard library `net/http`, official `golang.org/x/crypto/ssh`, and `github.com/pkg/sftp`. Frontend assets (HTML, CSS, JS, CodeMirror, xterm.js) embedded using `embed.FS`.
- **Database Layer**: Embedded SQLite with WAL mode (`PRAGMA journal_mode=WAL; PRAGMA foreign_keys=ON;`) for zero-ops, single-file persistence.
- **Agentless Inspection Pipeline**: Goroutine-based inspection worker running on configurable intervals (default: 5 minutes) executing standardized batch shell commands over an active SSH session to collect OS stats, docker state, backup files, and cron jobs.
- **Drift & Root Cause Engine**: Pure rule evaluation comparing the inspected Actual State against stored Desired State items. When an item status changes to degraded/failed, an automated diagnostic command (`docker inspect`, `docker logs --tail 50`, `journalctl -xeu <service> --no-pager -n 50`) is immediately executed and saved as a `Root Cause Excerpt` in the event history.
- **Lifecycle Scoring Algorithm**: 100-point base score penalized by:
  - OS EOL reached (-30) or approaching within 3 months (-15)
  - Sustained load average > core count over 7 days (-30)
  - Disk / I/O errors in `dmesg` (-40)
- **Web Terminal**: WebSocket endpoint bridging browser `xterm.js` traffic to an interactive SSH pseudo-terminal (`ssh.Session.RequestPty` and `ssh.Session.Shell`).
- **File Management & Editor**: SFTP client layer wrapping file operations (list, read, write, upload, stat, chmod) with a modal CodeMirror editor for text/PHP/config files.
- **Hybrid Alert Dispatcher**: An alert bus that updates in-memory/DB alert records for UI display and asynchronously dispatches HTTP POST requests to configured Telegram Bot API endpoints or generic webhook URLs.
- **Authentication**: Session cookie based authentication with password hashed via `bcrypt`. Database schema includes a `role` field defaulted to `admin` for future RBAC expansion.

## Testing Decisions

- **Test Seam**: The primary test seam is the **SSH Runner Interface** (`SSHClient` / `CommandExecutor`).
  - Production implementation connects over real SSH.
  - Test implementation provides scripted/mock responses for system commands (`cat /etc/os-release`, `docker ps`, `df -k`, `journalctl`, etc.).
- **What makes a good test**: Tests must verify externally observable behavior (e.g. given a simulated SSH response showing container crash, does the Inspection engine detect Drift, store the Root Cause Excerpt, calculate the correct Lifecycle Score, and emit a Telegram alert?). No tests mocking internal private functions.
- **HTTP / WebSocket Integration Tests**: Using standard Go `net/http/httptest` to test authentication, API endpoints, file operations, and terminal WebSocket connections end-to-end.

## Out of Scope

- Installing or managing background daemon agents on target hosts.
- Heavy metrics time-series aggregation (like Prometheus TSDB or Grafana chart engines).
- Multi-tenancy or complex multi-org permission matrices (single superadmin with role-ready schema for now).
- Full browser IDE features (Git tree, multi-file workspace search, language server protocols / LSP).

## Further Notes

- Target hosts require only standard POSIX tools (`sh`, `df`, `awk`, `uptime`, `date`) and standard SSH server (`sshd`). Docker management requires Docker CLI and socket on the target host.
