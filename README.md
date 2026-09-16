<p align="center">
  <a href="#readme">
    <img src=".github/assets/logo.svg" alt="Pantau Logo" width="128" />
  </a>
</p>

<h1 align="center">Pantau</h1>

<p align="center">
  <b>Agentless Linux Server Monitoring, Desired State Drift Engine & Interactive Management</b><br>
  <i>Single binary. Zero remote daemons. Embedded SQLite. Native SSH.</i>
</p>

<p align="center">
  <a href="README.md"><b>English</b></a> •
  <a href="README.id.md"><b>Bahasa Indonesia</b></a> •
  <a href="docs/user-guide.md"><b>📖 User Guide</b></a>
</p>

<p align="center">
  <img src="https://img.shields.io/badge/Go-1.22+-00ADD8?style=for-the-badge&logo=go&logoColor=white" alt="Go Version" />
  <img src="https://img.shields.io/badge/Architecture-Agentless%20SSH-3b82f6?style=for-the-badge" alt="Architecture" />
  <img src="https://img.shields.io/badge/Database-Embedded%20SQLite%20(WAL)-003B57?style=for-the-badge&logo=sqlite&logoColor=white" alt="SQLite" />
  <img src="https://img.shields.io/badge/i18n-English%20%7C%20Indonesia-10b981?style=for-the-badge" alt="Dual Language" />
  <img src="https://img.shields.io/badge/License-MIT-green?style=for-the-badge" alt="License MIT" />
</p>

<p align="center">
  <img src=".github/assets/dashboard.png" alt="Pantau Dashboard" width="100%" />
</p>

> [!WARNING]
> **Production & Security Advisory**: Pantau is an open-source administrative tool designed for server management and monitoring. It executes remote SSH commands, manages files via SFTP, and provisions keys directly on target Linux hosts with elevated/root privileges. This software is provided **"AS IS"** without warranties of any kind. Users are solely responsible for verifying operations, safeguarding private keys, and maintaining independent offline backups. Never expose Pantau to the public internet without proper authentication, reverse proxy protection, and TLS termination.

---

## 📖 Overview

**Pantau** is a self-hosted, lightweight, single-binary infrastructure monitoring and server management system built in Go. Unlike Prometheus/Node-Exporter, Zabbix, or Datadog, Pantau operates **100% agentless** over standard SSH (`port 22`). Target servers require **no background agents, no daemon installation, and no persistent telemetry scrapers**.

Pantau inspects remote hosts via non-interactive SSH commands, continuously validates systems against **Desired State** rules, diagnoses deviations with automatic **Root Cause Excerpts**, streams cross-host files in memory, and provides an in-browser Web Terminal (xterm.js) and SFTP Code Editor.

> [!TIP]
> **Administrator Playbook**: Looking for step-by-step operational workflows, disaster recovery procedures, or security hardening guides? Read the [Pantau Administrator Guide](docs/user-guide.md) ([Bahasa Indonesia](docs/user-guide.id.md)).

---

## 🏛️ Architecture

```
                                  +--------------------------------------------------+
                                  |                 Web Browser Client               |
                                  |     (Single-Page App, Fixed Shell, xterm.js)     |
                                  +------------------------+-------------------------+
                                                           | HTTP / WebSocket
                                                           v
+--------------------------------------------------------------------------------------------------------------------+
|                                                PANTAU CONTROL PLANE (Go)                                           |
|                                                                                                                    |
|  +---------------------+   +---------------------+   +---------------------+   +--------------------------------+  |
|  |   Inspection Loop   |   |   Drift Engine      |   |  Hybrid Alerting    |   | Cross-Host Transfer Engine     |  |
|  | (SSH Collector: 5m)|   | (Desired vs Actual) |   | (Telegram/Webhooks) |   | (Piped FIFO Stream, Zero-Disk) |  |
|  +----------+----------+   +----------+----------+   +----------+----------+   +---------------+----------------+  |
|             |                         |                         |                              |                   |
|             +-------------------------+------------+------------+                              |                   |
|                                                    |                                           |                   |
|                                       +------------v-------------+                             |                   |
|                                       | Embedded SQLite (WAL)    |                             |                   |
|                                       | Encrypted Snapshot/Argon2|                             |                   |
|                                       +------------+-------------+                             |                   |
+----------------------------------------------------|-------------------------------------------|-------------------+
                                                     | Native SSH Protocol                       | Stream Pipe
                                                     v                                           v
                       +-------------------------------------------+   +-------------------------------------------+
                       |           Remote Linux Host A             |   |           Remote Linux Host B             |
                       |  (Ubuntu / Debian / RHEL / CentOS 6-9)    |   |  (Ubuntu / Debian / RHEL / CentOS 6-9)    |
                       |  - POSIX Standard CLI Tools (`ps`, `df`)  |   |  - POSIX Standard CLI Tools (`ps`, `df`)  |
                       |  - Docker Engine / Containers             |   |  - Docker Engine / Containers             |
                       |  - No Daemons / No Agents Installed       |   |  - No Daemons / No Agents Installed       |
                       +-------------------------------------------+   +-------------------------------------------+
```

---

## ✨ Key Features

### 1. 🔍 Agentless SSH Inspection & 1-Click Key Provisioning
- Collects real-time metrics (CPU Load, RAM, Disk partitions, Network Egress, Sockets, Uptime, Kernel) purely via standard POSIX SSH.
- **One-Time Key Provisioning**: Provide target root/sudo password once in RAM. Pantau idempotently injects its universal RSA 4096-bit public key into `~/.ssh/authorized_keys` (universally supported across legacy OpenSSH 5.3+ through modern OpenSSH) and discards the password immediately from memory.
- **Legacy Server Compatibility**: Native cipher fallbacks (`aes128-cbc`, `3des-cbc`, `diffie-hellman-group1-sha1`, `ssh-dss`) allow monitoring legacy Linux servers (CentOS 6, Debian 7, OpenSSH 5.3+).

### 2. 📋 Desired State Baseline & Automated Drift Engine
- **1-Click Baseline**: Auto-detects running Docker containers, disks, cron jobs, and database services (`mysqld`, `postgres`, `redis`, `nginx`).
- **Real-Time Drift Detection**: Alerts when a container crashes, disk exceeds threshold (e.g. `> 85%`), cron job disappears, or backup becomes stale.

### 3. 🩺 Root Cause Excerpt Diagnostic Capture
- When a service or container crashes, Pantau captures the exact fault context:
  - Docker container exit code & `OOMKilled` memory termination flag.
  - Tail 50 lines of crash output from `journalctl -u <service>` or `docker logs <container>`.
- Displays incident diagnostic timelines directly inside the host workspace modal.

### 4. 🚀 Piped Cross-Host Streaming Transfer
- Stream files and folders between two remote hosts directly through memory pipes without spooling to Pantau's local disk.
- **Fast Stream Mode**: Maximizes throughput via `tar` streaming pipes.
- **Verified Mode**: Computes and verifies SHA256 checksums end-to-end on both source and destination hosts before reporting completion.

### 5. 🛡️ Network & Security Observability
- **Internet Egress & Latency**: Tests outbound connectivity and ping latency to global DNS resolvers (`1.1.1.1`).
- **Live Active Sockets**: Aggregates top connected remote IP addresses and established connections.
- **Brute-Force & Failed Login Counter**: Detects SSH brute-force attacks via `/var/log/auth.log` or `journalctl _SYSTEMD_UNIT=ssh.service`.
- **Attack Surface Classification**: Highlights open listening ports, binding addresses (`0.0.0.0` vs `127.0.0.1`), and tags risk levels (Public Internet vs Localhost).

### 6. 🔐 Encrypted System Snapshot & GitHub Disaster Recovery
- Encrypts all host configurations, SSH credentials, and desired state baselines into portable `.enc` snapshots using **Argon2id** key derivation and **AES-256-GCM** authenticated encryption.
- **Automated GitHub Sync**: Push encrypted snapshots to a private GitHub repository on a scheduled interval.
- **Disaster Recovery Wizard**: Rebuild an entire monitoring cluster from scratch using a GitHub token + repository path or a raw `.enc` file.

### 7. 💻 Interactive Web Terminal & SFTP File Manager
- **Web Terminal**: In-browser interactive shell powered by `xterm.js` over WebSocket SSH PTY sessions with full ANSI color and terminal resize support.
- **SFTP Explorer & Code Editor**: Navigate remote directories, upload/download files, edit scripts and `.env` files with embedded CodeMirror (Nord syntax highlighting).

### 8. 🌐 Dual Language Interface (English & Bahasa Indonesia)
- Instant client-side localization switcher (`🌐 EN` / `🌐 ID`) in the header.
- Zero-dependency local translation dictionary stored in `localStorage` (default: English).
- Canonical domain terminology preserved in Indonesian version for clear operational communication.

### 9. 🩺 Transparent 6-Factor Lifecycle Assessment & Hardware Refresh
- Evaluates server operational health across 6 transparent factors: Linux OS End-of-Life (EOL) status, hardware age & MTBF degradation (bare-metal BIOS date vs cloud VM OS deployment age), RAM pressure, CPU core-to-load saturation, disk capacity, and kernel I/O errors.
- **Productive Lifespan Guidance**: Automatically flags servers exceeding standard 3–5 year hardware amortization or 8-year critical lifespans, providing auditable checklist breakdowns and formal hardware refresh justifications.

### 10. 🛡️ Airgapped & Self-Contained Web Assets (Zero-CDN)
- 100% of frontend vendor libraries (`xterm.js`, `xterm-addon-fit`, and `CodeMirror` with 8 syntax modes: XML, JS, CSS, HTML, C-like, PHP, Shell, YAML) are bundled directly inside the single binary via `go:embed`.
- Enforces a strict Content Security Policy (CSP) with zero external CDN network requests, guaranteeing full offline operations in isolated intranets, airgapped VPCs, or disconnected environments.

### 11. 📂 Host Grouping, Terminal Maximize & Port Auto-Scan
- **Host Grouping**: Categorize hosts by environment or role (e.g. *Primary Cluster*, *Testing*, *Database Servers*) with unified grid and list view separation.
- **Terminal Maximize**: Expand the web terminal to a full-viewport canvas while preserving interactive terminal shortcuts (such as `Esc` in `vim`, `nano`, or `htop`).
- **Port Auto-Scan Fallback**: Automatically scans sequential ports (`8080` to `8099`) when initialized on the default port, eliminating startup crashes caused by busy ports.

### 12. ⚡ Terminal Presets & Split-Pane Observability
- **Terminal Presets**: Save and execute recurring diagnostic commands (`htop`, `docker stats`, `journalctl -f`) with pre-flight availability checks.
- **Split-Pane Web Terminal**: Launch dual side-by-side terminal sessions (`Alt+\`) to monitor and compare multiple remote hosts simultaneously in real time.

### 13. 🔐 Airgapped TOTP Two-Factor Authentication (2FA) & Emergency Recovery
- **Standard RFC 6238 TOTP**: Hardens administrator login using time-based one-time passwords compatible with any standard authenticator app (Google Authenticator, Aegis, 1Password, Bitwarden).
- **100% Offline Enrollment**: Base32 secrets and QR codes are generated purely client-side with zero external API calls or telemetry leaks.
- **Emergency Recovery Codes**: Generates 8 single-use recovery codes with instant copy and text file download for disaster recovery.
- **Brute-Force Rate Limiting**: Enforces an automatic 30-second cooling lockout after 3 consecutive failed verification attempts.
- **Host Bypass Flag**: Emergency `-disable-2fa` startup flag disables 2FA directly via the host master console if all credentials are lost.

### 14. 🖥️ Global Multi-Tab Terminal Dock & Persistent Session Pill
- **Multi-Tab Terminal Dock**: Persistent application-wide dock hosting multiple concurrent SSH PTY terminal sessions across different remote servers.
- **Dynamic Inline Renaming**: Rename terminal tabs on the fly to track multi-server maintenance workflows.
- **Persistent Floating Session Pill**: Minimize the dock into an unobtrusive bottom-right badge showing active shell count; explore dashboards and metrics while background processes run uninterrupted.
- **Seamless Maximize & Split Views**: One-click maximize to full viewport without disconnecting SSH sessions or breaking active terminal utilities (`htop`, `tmux`, `nano`).

---

## 🚀 Quick Start

### Option 1: Standalone Binary (Fastest)

Download the latest binary for your architecture from [GitHub Releases](https://github.com/herliansyah/pantau/releases):

```bash
# Make binary executable
chmod +x pantau

# Run Pantau with custom port and data directory
./pantau -port 8080 -db data/pantau.db
```

Open `http://localhost:8080` in your web browser.  
**Default Password**: `admin` *(change immediately in Settings)*.

---

### Option 2: Docker Compose

```yaml
version: "3.8"

services:
  pantau:
    image: ghcr.io/herliansyah/pantau:latest
    container_name: pantau
    restart: unless-stopped
    ports:
      - "8080:8080"
    volumes:
      - pantau-data:/data
    environment:
      - PANTAU_PORT=8080
      - PANTAU_DB=/data/pantau.db

volumes:
  pantau-data:
```

*(Note: Standard `PORT` and `DB_PATH` environment variables are also supported as automatic fallbacks).*

```bash
docker compose up -d
```

---

### Option 3: Systemd Service (Linux Production)

Create `/etc/systemd/system/pantau.service`:

```ini
[Unit]
Description=Pantau Server Monitoring & Management
After=network.target

[Service]
Type=simple
User=root
WorkingDirectory=/opt/pantau
ExecStart=/opt/pantau/pantau -port 8080 -db /opt/pantau/data/pantau.db
Restart=always
RestartSec=5
LimitNOFILE=65535

[Install]
WantedBy=multi-user.target
```

```bash
systemctl daemon-reload
systemctl enable --now pantau
```

---

## ⚙️ Configuration Reference

| Flag | Env Variable | Default | Description |
| :--- | :--- | :--- | :--- |
| `-port` | `PANTAU_PORT` (or `PORT`) | `8080` | HTTP listening port (auto-scans `8080`–`8099` on default) |
| `-db` | `PANTAU_DB` (or `DB_PATH`) | `pantau.db` | Path to SQLite database file |
| `-open` | - | `true` (Windows) / `false` | Automatically open default web browser on launch |
| `-disable-2fa` | - | `false` | Emergency bypass flag to disable TOTP 2FA directly from host terminal |
| `-v`, `-version` | - | - | Print Pantau version and exit |

---

## 🛠️ Tech Stack

- **Core Backend**: Golang (`net/http`, `golang.org/x/crypto/ssh`, `pkg/sftp`, `gorilla/websocket`)
- **Database**: Pure-Go SQLite (`modernc.org/sqlite`) with Write-Ahead Logging (WAL mode)
- **Frontend**: Single-Page Web App embedded via `go:embed` (Zero-npm, Vanilla JS, xterm.js, CodeMirror)
- **Security**: Argon2id + AES-256-GCM snapshot encryption, bcrypt admin credentials

---

## ⚠️ Disclaimer & Limitation of Liability

1. **"AS IS" Warranty Disclaimer**: Pantau is open-source software provided under the MIT License on an "AS IS" and "AS AVAILABLE" basis, without warranties of any kind, whether express, implied, statutory, or otherwise, including but not limited to warranties of merchantability, fitness for a particular purpose, non-infringement, or system integration.
2. **Assumption of Risk & Limitation of Liability**: The author, maintainers, and contributors accept **no liability or responsibility** for any damages, operational failures, data loss, server downtime, security breaches, unauthorized access, kernel panics, configuration corruption, or financial losses arising directly or indirectly from the installation, execution, or misuse of this software.
3. **User Responsibility**: You as the system operator retain full and exclusive responsibility for all actions performed through Pantau, including but not limited to remote shell commands, file modifications or deletions via SFTP, cross-host file streaming, cron modifications, container state transitions, and SSH key provisioning.

---

## 📄 License

Distributed under the **MIT License**. See [LICENSE](LICENSE) for the full license text and terms.

Developed with ❤️ by [Herliansyah](https://github.com/herliansyah).
