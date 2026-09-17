# Pantau Administrator Guide & Operational Playbook

[**English**](user-guide.md) • [**Bahasa Indonesia**](user-guide.id.md)

Welcome to the **Pantau Administrator Guide**. This document provides an operational playbook designed for system administrators, DevOps engineers, and site reliability teams managing remote Linux infrastructure with Pantau.

Pantau operates **100% agentless** over standard SSH (`port 22`). It requires zero daemons or telemetry agents on target machines. All state evaluation, file streaming, and diagnostic extraction occur directly through standard POSIX utilities.

---

## 📑 Table of Contents

1. [Architecture & Trust Boundaries](#1-architecture--trust-boundaries)
2. [Phase 1: Onboarding & Inventory Management](#2-phase-1-onboarding--inventory-management)
   - [Adding a Host & Key Provisioning](#adding-a-host--key-provisioning)
   - [Organizing with Host Groups & Manual Host Order](#organizing-with-host-groups--manual-host-order)
3. [Phase 2: Desired State Baselines & System Guardrails](#3-phase-2-desired-state-baselines--system-guardrails)
   - [Capturing a 1-Click Desired State Baseline](#capturing-a-1-click-desired-state-baseline)
   - [Configuring Backup Freshness & Protected Paths](#configuring-backup-freshness--protected-paths)
4. [Phase 3: Observability, Drift Triage & Incident Response](#4-phase-3-observability-drift-triage--incident-response)
   - [Detecting Drift & Root Cause Excerpts](#detecting-drift--root-cause-excerpts)
   - [Interactive Diagnostics via Terminal Dock & Terminal Presets](#interactive-diagnostics-via-terminal-dock--terminal-presets)
   - [Evaluating Server Longevity with Lifecycle Assessment](#evaluating-server-longevity-with-lifecycle-assessment)
5. [Phase 4: File Operations & Cross-Host Data Transfer](#5-phase-4-file-operations--cross-host-data-transfer)
   - [Cross-Host Transfer (Zero-Disk Memory Pipes)](#cross-host-transfer-zero-disk-memory-pipes)
   - [On-The-Fly Folder Streaming Download](#on-the-fly-folder-streaming-download)
6. [Phase 5: Hardening, Snapshots & Disaster Recovery](#6-phase-5-hardening-snapshots--disaster-recovery)
   - [Two-Factor Authentication (2FA) & Emergency Bypass](#two-factor-authentication-2fa--emergency-bypass)
   - [Encrypted System Snapshots & GitHub Disaster Recovery](#encrypted-system-snapshots--github-disaster-recovery)

---

## 1. Architecture & Trust Boundaries

Pantau executes remote commands and inspects telemetry via standard SSH connections.

- **Control Plane**: A single Go binary running on your management machine or bastion host, backed by an embedded SQLite database (`pantau.db`) in WAL mode.
- **Remote Host**: Any physical or virtual Linux machine (Ubuntu, Debian, RHEL, CentOS 6–9) accessible via SSH.
- **Trust Boundary**: Pantau uses an RSA 4096-bit keypair generated on first startup. Target hosts must grant access to this public key inside `~/.ssh/authorized_keys` for the designated management user (`root` or a dedicated user with passwordless `sudo`).

```
+-------------------+             SSH Port 22 (Agentless)             +-----------------------+
|  Pantau Control   | ==============================================> |   Remote Linux Host   |
|  Plane (Go + DB)  | < - - - - - - - - - - - - - - - - - - - - - - - |  (POSIX df, ps, logs) |
+-------------------+          Actual State & Telemetry               +-----------------------+
```

---

## 2. Phase 1: Onboarding & Inventory Management

### Adding a Host & Key Provisioning

When connecting a new **Host** to Pantau:

1. Click **Add Host** on the main dashboard to open the **Action Dialog**.
2. Enter the connection parameters:
   - **Label**: A distinct identifier (e.g., `prod-db-primary`).
   - **IP / Hostname**: Resolvable hostname or IPv4/IPv6 address.
   - **SSH Port**: Standard port `22` or custom SSH daemon port.
   - **SSH User**: `root` or a sudo-privileged user.
3. Choose the authentication method:
   - **Existing SSH Key**: Use if the host already accepts Pantau's universal public key.
   - **Key Provisioning (One-Time Password)**: If configuring a brand-new host, provide the target server password once. Pantau will connect via SSH, append its RSA 4096-bit public key to `~/.ssh/authorized_keys` idempotently, and immediately discard the password from memory.
4. Click **Save & Test Connection**. Pantau performs an initial handshake to verify connectivity.

> [!NOTE]
> For legacy Linux systems (CentOS 6, Debian 7, or systems running OpenSSH 5.3+), Pantau automatically enables legacy cipher fallbacks (`aes128-cbc`, `3des-cbc`, `diffie-hellman-group1-sha1`, `ssh-dss`) during the SSH handshake.

### Organizing with Host Groups & Manual Host Order

As your fleet grows:
- **Host Group**: Categorize hosts by functional tiers or environments (e.g., `Database Cluster`, `Edge Proxies`, `Staging`). Groups render as distinct visual partitions in both Grid and List **View Mode**.
- **Manual Host Order**: Click and drag or prioritize mission-critical hosts to the top of the dashboard.
- **Host Note & Global Note**: Use **Host Note** for server-specific maintenance checklists (e.g., `"Primary PostgreSQL; failover standby is db-02"`). Use **Global Note** for cluster-wide operational announcements and shift handovers.

---

## 3. Phase 2: Desired State Baselines & System Guardrails

Pantau does not merely display metrics; it enforces continuous adherence to your declared configuration baseline.

### Capturing a 1-Click Desired State Baseline

1. Open the host's **Workspace Modal** by clicking on its card.
2. Navigate to the **Desired State** tab.
3. Click **1-Click Baseline**:
   - Pantau triggers an immediate SSH command sequence to inspect running Docker containers, mounted disk partitions, configured cron jobs, and listening network ports.
   - These discovered resources are saved as the host's authoritative **Desired State**.
4. Customize threshold rules:
   - **Disk Usage**: Set maximum percentage limits (e.g., root filesystem alert at `> 85%`).
   - **Service & Container Check**: Specify critical Docker containers (e.g., `nginx`, `redis`) or systemd services that must remain in `running` state.
   - **Memory & Swap Saturation**: Specify RAM warning levels.

### Configuring Backup Freshness & Protected Paths

- **Backup Freshness**: Define expected backup target paths (e.g., `/backup/postgres/daily.sql.gz`) and max age thresholds (e.g., must be updated within 24 hours and non-zero in byte size). Pantau marks a **Drift** if the backup file is missing, empty, or outdated.
- **Protected Path**: Protect critical operational directories (e.g., `/etc/`, `/boot/`, `/var/lib/docker/`) from inadvertent modification, deletion, or unauthorized file uploads via the integrated SFTP file manager.

---

## 4. Phase 3: Observability, Drift Triage & Incident Response

### Detecting Drift & Root Cause Excerpts

During every scheduled **Inspection** cycle (default: every 5 minutes) or upon manual trigger:

1. Pantau compares the remote **Actual State** against the defined **Desired State**.
2. If any discrepancy exists (e.g., a critical container exited, or disk capacity exceeded its threshold), the host status changes to **Drift**.
3. **Automated Root Cause Excerpt**: Pantau immediately collects diagnostic evidence without administrator intervention:
   - Container exit code and `OOMKilled` status flag.
   - Last 50 lines of crash output from `docker logs --tail 50 <container>` or `journalctl -u <service> -n 50 --no-pager`.
4. The **Root Cause Excerpt** is rendered directly in the **Workspace Modal** incident timeline and dispatched to configured **Notification Channels** (Telegram bot / Webhooks).

### Interactive Diagnostics via Terminal Dock & Terminal Presets

When a drift or alert requires hands-on investigation:

- **Terminal Dock**: Open a multi-tab interactive shell powered by `xterm.js` over WebSocket SSH PTY sessions.
- **Split-Pane View (`Alt+\`)**: Split the terminal view to compare logs or configurations across two hosts side by side in real time.
- **Terminal Preset**: Execute pre-configured one-click diagnostic commands (e.g., `htop`, `journalctl -f`, `docker stats`) without typing redundant commands.
- **Session Pill**: Minimize the dock into a floating badge in the bottom-right corner. Background shell sessions, long-running tail jobs, or scripts continue running uninterrupted while you browse the dashboard.

### Evaluating Server Longevity with Lifecycle Assessment

Pantau continuously calculates an auditable **Lifecycle Score** (0–100) based on six weighted factors:

1. **OS End-of-Life (EOL)**: Checks OS distribution release and kernel version against official vendor support dates.
2. **Productive Lifespan**: Assesses hardware age against standard industry amortization (3–5 years) using bare-metal BIOS release dates or cloud VM deployment timestamps.
3. **RAM Pressure**: Sustained memory exhaustion and swap thrashing.
4. **CPU Core-to-Load Saturation**: Load averages normalized against physical/virtual core counts.
5. **Disk Partition Capacity**: I/O saturation and storage exhaustion risk.
6. **Kernel I/O & Hardware Errors**: Scans kernel ring buffers (`dmesg`) for disk sector faults, filesystem remounts (read-only), or hardware resets.

When a host drops into degraded lifecycle thresholds, Pantau generates concrete **Hardware Refresh** justifications for capacity planning and hardware replacement.

---

## 5. Phase 4: File Operations & Cross-Host Data Transfer

### Cross-Host Transfer (Zero-Disk Memory Pipes)

Transferring large databases, directory trees, or archive bundles between two remote hosts typically requires intermediate storage or manual `scp` setup. Pantau streams data across hosts **without writing any intermediate bytes to Pantau's local disk**.

```
+-------------------+        Piped Stream (In-Memory Relay)         +-------------------+
|  Remote Host A    | ============================================> |  Remote Host B    |
|  (Source: tar)    |                 via Pantau                    |  (Dest: untar)    |
+-------------------+                                               +-------------------+
```

1. Open the file explorer in the **Workspace Modal** of the source Host.
2. Select the file or folder and click **Cross-Host Transfer**.
3. Select the target destination Host and destination path.
4. Choose the transfer mode:
   - **Fast Stream Mode**: High-throughput in-memory piping using standard `tar` streams.
   - **Verified Mode**: Calculates and validates SHA256 hashes end-to-end on both source and destination hosts before concluding the transfer.
5. Track progress in the background **Transfer Job** queue (real-time MB/s throughput, transferred bytes, and ETA).

### On-The-Fly Folder Streaming Download

To download an entire remote folder structure to your local workstation:
- Select the folder in the SFTP manager and click **Download as Archive**.
- Pantau streams the compressed archive on-the-fly (`zip` if installed on remote host, or `tar.gz`) directly to your browser's HTTP download stream without creating temporary files on the Pantau server disk.

---

## 6. Phase 5: Hardening, Snapshots & Disaster Recovery

### Two-Factor Authentication (2FA) & Emergency Bypass

To protect management actions against credential theft:

1. Open **Settings** → **Two-Factor Authentication (2FA)**.
2. Pantau displays an RFC 6238 compliant **TOTP Secret** and QR code generated **100% offline** in your browser (zero external API calls).
3. Scan the QR code using your authenticator app (Google Authenticator, Aegis, 1Password, Bitwarden).
4. Save the **8 Emergency Recovery Codes** in a secure offline vault.
5. Enter the 6-digit code to activate 2FA.

#### Security Policies & Emergency Bypass:
- **2FA Lockout**: 3 consecutive failed verification attempts automatically trigger a 30-second cooldown lock to prevent automated brute-force attacks.
- **2FA Bypass Flag**: If mobile devices and recovery codes are completely lost, an administrator with shell access to the Pantau server can restart the binary with the emergency flag:
  ```bash
  ./pantau -port 8080 -db /data/pantau.db -disable-2fa
  ```
  This immediately clears active TOTP configurations from SQLite and allows administrative login.

### Encrypted System Snapshots & GitHub Disaster Recovery

A **System Snapshot** is a self-contained, encrypted archive containing the complete Pantau database (host inventories, encrypted SSH keys, Desired State rules, and application settings).

#### Creating & Encrypting Snapshots:
1. Open **Settings** → **Backup & Disaster Recovery**.
2. Enter a strong **Snapshot Passphrase**. Pantau derives an encryption key using **Argon2id** and seals the snapshot with **AES-256-GCM**.
3. Download the resulting `.enc` file for offline storage.

#### Automated Remote Storage Sync:
- Configure a GitHub Personal Access Token (PAT with `repo` scope) and a private GitHub repository (`username/pantau-backups`).
- Set an automated sync interval (e.g., daily). Pantau pushes the encrypted `.enc` file to the remote repository automatically.
- **Multi-Server Tip**: If running multiple Pantau instances (e.g., office server and home lab) against the same GitHub repository, customize the **File Path in Repo** on each instance (e.g., `snapshots/office.enc` and `snapshots/homelab.enc`) so backups do not overwrite each other.


#### Bare-Metal Recovery (Disaster Recovery Wizard):
If the Pantau server is completely lost or destroyed:
1. Deploy a clean Pantau binary on a new host.
2. Launch the application; on first boot, click **Restore from System Snapshot**.
3. Select your local `.enc` file or enter your GitHub repository credentials + token.
4. Input your **Snapshot Passphrase**. Pantau decrypts the database, verifies integrity, reinstates the WAL SQLite database, and resumes background host inspection immediately.
