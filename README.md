# 🛡️ Pantau

**Agentless Linux Server Monitoring & Interactive Management System**

Pantau is a self-hosted, lightweight, single-binary Go application providing agentless server management and monitoring over standard SSH.

---

## ✨ Features

- **Agentless SSH Inspection**: Inspect remote Linux servers without installing daemons or agents on target hosts.
- **One-Time Key Provisioning**: Input the target server's SSH password once in RAM; Pantau idempotently injects its ED25519 public key to `~/.ssh/authorized_keys` and discards the password immediately.
- **Desired State Engine**:
  - **1-Click Baseline**: Auto-detect running Docker containers, disks, cron jobs, and DB services.
  - **Automated Drift Detection**: Compares Actual State against Desired State rules.
- **Root Cause Excerpt Capture**: Automatically extracts container exit codes, OOMKilled flags, and the last 50 lines of error logs (`journalctl` / `docker logs`) when a crash or drift occurs.
- **Interactive Web Terminal**: Direct in-browser shell access using `xterm.js` via WebSocket SSH PTY.
- **SFTP File Manager & Code Editor**: In-browser file explorer (upload, download, chmod, delete) and lightweight CodeMirror editor for PHP, `.env`, config, and bash scripts.
- **Docker Control**: Start, stop, restart containers and stream live container logs in the web interface.
- **Lifecycle Score (0-100)**: Evaluates hardware & OS retirement suitability based on OS EOL status, sustained resource pressure, and kernel disk I/O errors.
- **Backup Freshness**: Verifies file existence, size (> 0 bytes), and modification age (< 24h).
- **Hybrid Alerting**: Real-time web alert badges + instant Telegram Bot and Webhook notifications.
- **Zero-Ops**: Embedded SQLite (WAL mode) and embedded web assets. Single binary footprint (~17MB, <30MB RAM).

---

## 🚀 Quick Start

### Option 1: Standalone Binary

```bash
# Run binary directly
./pantau -port 8080 -db data/pantau.db
```

Open your browser at `http://localhost:8080` (Default password: `admin`).

### Option 2: Docker Compose

```bash
docker compose up -d
```

---

## 🛠️ Tech Stack

- **Backend**: Golang (`net/http`, `golang.org/x/crypto/ssh`, `pkg/sftp`, `gorilla/websocket`)
- **Database**: Embedded SQLite (Pure-Go `modernc.org/sqlite` with WAL mode)
- **Frontend**: Single-Page Web UI embedded via `go:embed` (Vanilla JS, xterm.js, CodeMirror)

---

## 📄 License

MIT License.
