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
   - [Inspection Lifecycle, Mass Inspection (Inspect All) & Recency](#inspection-lifecycle-mass-inspection-inspect-all--recency)
   - [Detecting Drift & Root Cause Excerpts](#detecting-drift--root-cause-excerpts)
   - [Interactive Diagnostics via Terminal Dock & Terminal Presets](#interactive-diagnostics-via-terminal-dock--terminal-presets)
   - [Evaluating Server Longevity with Lifecycle Assessment](#evaluating-server-longevity-with-lifecycle-assessment)
5. [Phase 4: File Operations & Cross-Host Data Transfer](#5-phase-4-file-operations--cross-host-data-transfer)
   - [Cross-Host Transfer (Zero-Disk Memory Pipes)](#cross-host-transfer-zero-disk-memory-pipes)
   - [On-The-Fly Folder Streaming Download](#on-the-fly-folder-streaming-download)
6. [Phase 5: Hardening, Snapshots & Disaster Recovery](#6-phase-5-hardening-snapshots--disaster-recovery)
   - [Two-Factor Authentication (2FA) & Emergency Bypass](#two-factor-authentication-2fa--emergency-bypass)
   - [Encrypted System Snapshots & GitHub Disaster Recovery](#encrypted-system-snapshots--github-disaster-recovery)
7. [Phase 6: Maintenance & Updates (Update Checker & Self-Update)](#7-phase-6-maintenance--updates-update-checker--self-update)
   - [Update Checker & Release Discovery](#update-checker--release-discovery)
   - [One-Click Self-Update & SHA-256 Verification](#one-click-self-update--sha-256-verification)
   - [Docker Container vs Standalone Environments](#docker-container-vs-standalone-environments)
   - [Airgapped Deployments](#airgapped-deployments)

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

### Inspection Lifecycle, Mass Inspection (Inspect All) & Recency

Pantau collects actual state telemetry and validates desired state rules through periodic background cycles or on-demand triggers:

- **Mass Inspection (Inspect All)**: Click the `⚡ Inspect All` button in the header toolbar to trigger asynchronous, concurrent inspections across all configured hosts.
- **Per-Host Immediate Inspection**: Click the quick `⚡` inspect button on any host card or row, or click *Run Immediate Inspection* inside the host detail modal.
- **Inspection Recency Indicators**: Host cards and list rows show dynamic relative timestamps (e.g. `🕒 2m ago`, `🕒 just now`) with precise hover tooltips.
- **Stale Inspection Detection**: When a host fails inspection or telemetry remains unrefreshed beyond tolerance (> 10 minutes or 2× the normal interval), an amber `⚠️ Stale Data` warning alerts operators to potential SSH disconnection or unresponsive nodes.
- **Dashboard Auto-Refresh**: The browser UI automatically synchronizes host metrics and status every 30 seconds without requiring manual page reload.
- **Configurable Inspection Interval**: Administrators can adjust background SSH inspection frequency (default: 300 seconds / 5 minutes, minimum 30 seconds) via **Settings** > **Access** tab.

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

---

## 7. Phase 6: Maintenance & Updates (Update Checker & Self-Update)

Pantau provides a robust, zero-data-loss update lifecycle ensuring administrators can update the control plane safely without disrupting ongoing monitoring jobs.

### Update Checker & Release Discovery
- **Background Release Detection**: Scans upstream GitHub Releases every 12 hours (with a 6-hour local cache) to avoid hitting GitHub API IP rate limits.
- **Visual Status Badges**: If a newer release is published, update badges and release notes appear non-obtrusively in the dashboard footer and Settings modal.
- **On-Demand Checking**: Administrators can trigger immediate checks via the **Check for Updates** button in Settings.

### One-Click Self-Update & SHA-256 Verification
For standalone binary installations on Linux and Windows:
1. Navigate to **Settings** → **System Updates** and select **Update Now**.
2. **SHA-256 Verification**: Pantau downloads the official `checksums.txt` manifest from upstream and verifies archive integrity before extraction.
3. **Pre-flight Smoke Testing**: Extracts candidate binary to a temporary path and runs `pantau.tmp -v`. If execution fails (e.g., incompatible architecture or corrupt download), the update is aborted with zero changes made (*automatic rollback*).
4. **Cross-Platform Atomic Swap**:
   - On Linux: Atomic swap via `os.Rename`.
   - On Windows: Renames running locked binary (`pantau.exe` -> `pantau.exe.old`) and installs the new binary. The `.old` artifact is purged on subsequent launches.
5. **Graceful Restart**: Clicking **Restart Pantau Now** triggers an orderly shutdown (finishing pending requests and closing the SQLite WAL safely), spawns the new binary with identical CLI parameters, and the web interface automatically re-establishes connection.

### Docker Container vs Standalone Environments
- When executing inside Docker (`/.dockerenv`), self-replacement of the binary file is automatically disabled to preserve container immutability.
- The UI instead presents recommended update commands:
  ```bash
  docker compose pull && docker compose up -d
  ```

### Airgapped Deployments
- In isolated enterprise intranets without public internet egress, the Update Checker operates *fail-silent* with a 3-second timeout, ensuring no UI errors or boot latency occur.
- Automated update checks can be explicitly disabled using the `-disable-update-check` CLI flag or `PANTAU_DISABLE_UPDATE_CHECK=true` environment variable.

