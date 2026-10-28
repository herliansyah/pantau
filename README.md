<p align="center">
  <a href="#readme">
    <img src=".github/assets/logo.svg" alt="Pantau Logo" width="100" />
  </a>
</p>

<h1 align="center">Pantau</h1>

<p align="center">
  Agentless Linux server monitoring and lightweight remote operations in a single binary.
</p>

<p align="center">
  <a href="README.md"><b>English</b></a> •
  <a href="README.id.md"><b>Bahasa Indonesia</b></a> •
  <a href="docs/user-guide.md"><b>Documentation</b></a> •
  <a href="https://github.com/herliansyah/pantau/releases"><b>Releases</b></a>
</p>

<p align="center">
  <a href="https://github.com/herliansyah/pantau/releases"><img src="https://img.shields.io/github/v/release/herliansyah/pantau?style=flat-square" alt="Latest Release" /></a>
  <img src="https://img.shields.io/badge/go-1.22+-00ADD8?style=flat-square&logo=go&logoColor=white" alt="Go Version" />
  <img src="https://img.shields.io/badge/database-embedded%20sqlite-003B57?style=flat-square&logo=sqlite&logoColor=white" alt="SQLite" />
  <a href="LICENSE"><img src="https://img.shields.io/badge/license-MIT-green?style=flat-square" alt="License" /></a>
</p>

<p align="center">
  <img src=".github/assets/dashboard.png" alt="Pantau Dashboard" width="100%" />
</p>

---

## Why Pantau?

Most infrastructure monitoring tools require installing, configuring, and maintaining background agents (like Prometheus Node Exporter, Datadog Agent, or Zabbix Agent) across every managed machine. For small to mid-sized fleets, airgapped intranets, or environments with legacy operating systems, this introduces significant maintenance overhead and security surface.

**Pantau takes a different path:**
- **Zero remote agents**: Operates exclusively over standard OpenSSH (`port 22`) using POSIX standard commands. Target servers require zero daemon installations, zero open telemetry ports, and zero background resource footprint.
- **Single self-contained binary**: Written in Go with an embedded SQLite database and embedded zero-npm web interface. Runs anywhere with zero external dependencies.
- **Operations in one place**: Combines health metrics, drift detection against desired state baselines, interactive multi-tab terminals, secure file management, and zero-disk cross-host file streaming inside a single web dashboard.

---

## Architecture

```
                    +------------------------------------+
                    |         Web Browser Client         |
                    |   (SPA, xterm.js, CodeMirror, SSE) |
                    +-----------------+------------------+
                                      | HTTP / WebSocket
                                      v
+---------------------------------------------------------------------------------+
|                            PANTAU CONTROL PLANE (Go)                            |
|                                                                                 |
|  +--------------------+  +---------------------+  +--------------------------+  |
|  |  Inspection Loop   |  | Drift Engine        |  | Cross-Host Stream        |  |
|  |  (SSH POSIX probe) |  | (Baseline vs State) |  | (Memory pipe, zero disk) |  |
|  +---------+----------+  +----------+----------+  +------------+-------------+  |
|            |                        |                          |                |
|            +------------------------+--------------------------+                |
|                                     |                                           |
|                       +-------------v-------------+                             |
|                       |   Embedded SQLite (WAL)   |                             |
|                       | Encrypted Snapshots (AES) |                             |
|                       +-------------+-------------+                             |
+-------------------------------------|-------------------------------------------+
                                      | Native SSH (Port 22)
                 +--------------------+--------------------+
                 v                                         v
+---------------------------------+       +---------------------------------+
|       Remote Linux Host A       |       |       Remote Linux Host B       |
| (Debian, Ubuntu, RHEL, Alpine)  |       | (Debian, Ubuntu, RHEL, Alpine)  |
| - Standard POSIX tools (ps, df) |       | - Standard POSIX tools (ps, df) |
| - Docker Engine / systemd       |       | - Docker Engine / systemd       |
| - No agents / daemons installed |       | - No agents / daemons installed |
+---------------------------------+       +---------------------------------+
```

---

## Core Capabilities

### 1. Fleet Health & Real-time Metrics
Pantau inspects remote hosts via standard SSH connections using non-interactive POSIX commands (`/proc`, `ps`, `df`, `ss`).
- **Resource saturation**: Tracks CPU load, memory utilization, swap usage, disk partitions, and `/proc/diskstats` I/O rates without requiring `iostat`.
- **Process and network telemetry**: Identifies top CPU and memory-consuming processes, open listening ports, inbound active sockets, and failed SSH authentication attempts.
- **Package and lifecycle audit**: Catalogs installed software across Debian (`dpkg`), RHEL (`rpm`), Alpine (`apk`), and Arch (`pacman`), highlighting dormant services and calculating OS End-of-Life (EOL) timelines.
- Learn more in the [Inspection and Metrics Guide](docs/user-guide.md#4-phase-3-observability-drift-triage--incident-response).

### 2. Desired State Baselines & Incident Diagnostics
Establish expected operational baselines for containers, core services, disk thresholds, and backup timestamps.
- **Automated drift detection**: Detects when containers stop, systemd services fail, cron jobs vanish, or disk usage exceeds configured warning levels.
- **Root cause crash capture**: When a tracked service or container fails, Pantau automatically extracts the exit code, OOM-killed status flags, and the trailing lines from `journalctl` or `docker logs`.
- Learn more in the [Desired State & Drift Guide](docs/user-guide.md#3-phase-2-desired-state-baselines--system-guardrails).

### 3. Integrated Web Terminal & Remote Operations
Manage target servers directly from the browser without context-switching to external terminal applications.
- **Multi-tab PTY terminal**: Browser-based interactive shell powered by `xterm.js` and WebSockets, supporting split-pane view (`Alt+\`), dynamic resizing, and custom presets.
- **SFTP file editor & bookmarks**: Browse remote file trees, pin critical log or configuration paths, and edit scripts with syntax highlighting.
- **Remote web port tunneling**: Agentless HTTP/WebSocket reverse proxy over SSH `direct-tcpip` channels, allowing you to access private services (`localhost:port`) on remote servers without opening firewall ports.
- **Native OS terminal bridge**: Launch your local desktop terminal (Windows Terminal, GNOME Terminal, iTerm2, Alacritty) into any remote host with one click via ephemeral in-memory SSH keys.
- Learn more in the [Terminal and Remote Operations Guide](docs/user-guide.md#interactive-diagnostics-via-terminal-dock--terminal-presets).

### 4. Zero-Disk Streaming & Encrypted Backup
Safely transfer data between servers and safeguard your configuration cluster.
- **Piped cross-host transfer**: Stream files and directories directly between two remote Linux servers through memory pipes (`tar` stream), bypassing Pantau's local storage entirely, with optional end-to-end SHA-256 verification.
- **Encrypted cluster snapshots**: Export host configurations, baselines, and SSH keys into portable encrypted snapshot archives secured with Argon2id and AES-256-GCM.
- **Automated disaster recovery**: Schedule periodic encrypted snapshot backups to private GitHub repositories for automated offsite recovery.
- Learn more in the [Cross-Host Transfer Guide](docs/user-guide.md#cross-host-transfer-zero-disk-memory-pipes).

### 5. Airgapped-First Architecture
Engineered from the ground up for strict security postures, private VPCs, and disconnected intranets.
- **Zero runtime dependencies**: Pure Go binary with embedded SQLite running in Write-Ahead Logging (WAL) mode.
- **Self-contained frontend**: All web assets, fonts, icons, and client scripts (`xterm.js`, `CodeMirror`) are embedded directly via `go:embed` with zero CDN calls.
- **Local security**: Multi-factor authentication via standard RFC 6238 TOTP with entirely client-side QR generation and emergency offline recovery codes.
- **Semi-automatic updates**: In-app release checking with SHA-256 integrity checks, pre-flight binary verification, and atomic in-place binary swapping.

---

## Quick Start

### Option 1: Standalone Binary (Recommended)

Download the pre-compiled binary for your architecture from [GitHub Releases](https://github.com/herliansyah/pantau/releases):

```bash
# Make binary executable
chmod +x pantau

# Start Pantau on port 8080
./pantau -port 8080 -db data/pantau.db
```

Open `http://localhost:8080` in your browser.  
Default password: `admin` *(prompted to update immediately upon first login)*.

### Option 2: Docker Compose

```yaml
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

```bash
docker compose up -d
```

### Option 3: Systemd Service (Linux Server)

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

## Configuration Reference

Pantau accepts configuration via CLI flags or equivalent environment variables:

| Flag | Environment Variable | Default | Description |
| :--- | :--- | :--- | :--- |
| `-port` | `PANTAU_PORT` (or `PORT`) | `8080` | HTTP listening port (scans `8080`–`8099` if default is occupied) |
| `-db` | `PANTAU_DB` (or `DB_PATH`) | `pantau.db` | Path to SQLite database file |
| `-open` | - | `true` (Win) / `false` | Automatically launch default browser on startup |
| `-disable-2fa` | - | `false` | Emergency bypass flag to disable TOTP 2FA directly via terminal |
| `-check-update` | - | `false` | Check upstream release version and exit |
| `-update` | - | `false` | Self-update binary to latest release and exit |
| `-disable-update-check` | `PANTAU_DISABLE_UPDATE_CHECK` | `false` | Disable background update checks (recommended for airgapped setups) |
| `-log-level` | `PANTAU_LOG_LEVEL` | `error` | Console log verbosity (`debug`, `info`, `warn`, `error`, `off`) |
| `-q`, `-quiet` | - | `false` | Shorthand to suppress non-error logs (`-log-level=error`) |
| `-v`, `-version` | - | - | Print version and runtime details, then exit |

---

## Security & Production Notes

- **Network exposure**: Pantau is an administrative control plane capable of executing remote commands and modifying files. **Never expose Pantau directly to the public internet** without placing it behind a hardened reverse proxy (e.g., Caddy, Nginx, or Cloudflare Access) with TLS termination and restricted IP allowlists.
- **SSH key management**: Target hosts can be provisioned using one-time password injection or manual public key placement (`~/.ssh/authorized_keys`). Pantau uses dedicated key pairs and discards passwords from memory immediately after provisioning.
- **Disaster recovery**: Always export and backup your encrypted configuration snapshot regularly. Snapshots are encrypted with Argon2id and AES-256-GCM.

For detailed security guidelines and hardening recommendations, consult the [Administrator Security Guide](docs/user-guide.md#6-phase-5-hardening-snapshots--disaster-recovery).

---

## License

Distributed under the **MIT License**. See [LICENSE](LICENSE) for details.

Maintained by [Herliansyah](https://github.com/herliansyah).
