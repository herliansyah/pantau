# 🛡️ Pantau

<p align="center">
  <img src="https://raw.githubusercontent.com/herliansyah/pantau/main/.github/assets/logo.png" alt="Logo Pantau" width="120" onerror="this.style.display='none'" />
</p>

<p align="center">
  <b>Sistem Pemantauan Server Linux Berbasis Agentless SSH, Desired State Drift Engine & Manajemen Interaktif</b><br>
  <i>Single binary. Tanpa instalasi agen remote. Embedded SQLite. Native SSH.</i>
</p>

<p align="center">
  <a href="README.md"><b>English</b></a> •
  <a href="README.id.md"><b>Bahasa Indonesia</b></a>
</p>

<p align="center">
  <img src="https://img.shields.io/badge/Go-1.22+-00ADD8?style=for-the-badge&logo=go&logoColor=white" alt="Go Version" />
  <img src="https://img.shields.io/badge/Architecture-Agentless%20SSH-3b82f6?style=for-the-badge" alt="Architecture" />
  <img src="https://img.shields.io/badge/Database-Embedded%20SQLite%20(WAL)-003B57?style=for-the-badge&logo=sqlite&logoColor=white" alt="SQLite" />
  <img src="https://img.shields.io/badge/i18n-English%20%7C%20Indonesia-10b981?style=for-the-badge" alt="Dual Language" />
  <img src="https://img.shields.io/badge/License-MIT-green?style=for-the-badge" alt="License MIT" />
</p>

---

## 📖 Ringkasan

**Pantau** adalah sistem pemantauan dan manajemen infrastruktur server Linux mandiri (*self-hosted*), ringan, dan berformat *single binary* yang dibangun dengan Go. Berbeda dari sistem tradisional seperti Prometheus/Node-Exporter, Zabbix, atau Datadog, Pantau beroperasi **100% agentless** melalui protokol SSH standar (`port 22`). Server target **tidak memerlukan daemon tambahan, tidak ada instalasi agen, dan tidak ada scraper telemetri yang membebani sistem**.

Pantau menginspeksi Host remote melalui perintah SSH non-interaktif, memvalidasi kondisi sistem secara berkala terhadap aturan **Desired State**, mendiagnosis deviasi secara otomatis melalui **Root Cause Excerpt**, mentransfer file antar-host langsung lewat memori, serta menyediakan Web Terminal interaktif (xterm.js) dan SFTP Code Editor langsung dari browser.

---

## 🏛️ Arsitektur Sistem

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
|  | (15s SSH Collector)|   | (Desired vs Actual) |   | (Telegram/Webhooks) |   | (Piped FIFO Stream, Zero-Disk) |  |
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
                       |  - Tanpa Daemon / Tanpa Instalasi Agen    |   |  - Tanpa Daemon / Tanpa Instalasi Agen    |
                       +-------------------------------------------+   +-------------------------------------------+
```

---

## ✨ Fitur Utama

### 1. 🔍 Inspeksi Agentless SSH & 1-Click Key Provisioning
- Mengambil metrik sistem secara aktual (CPU Load, RAM, partisi Disk, Network Egress, Sockets, Uptime, Kernel) murni melalui koneksi SSH standar.
- **One-Time Key Provisioning**: Masukkan password server target sekali di RAM. Pantau secara idempoten menyalin public key ED25519 ke `~/.ssh/authorized_keys` dan langsung menghapus password dari memori.
- **Kompatibilitas Server Lawas**: Mendukung cipher warisan (`aes128-cbc`, `3des-cbc`, `diffie-hellman-group1-sha1`, `ssh-dss`) untuk memantau server Linux legasi (CentOS 6, Debian 7, OpenSSH 5.3+).

### 2. 📋 Baseline Desired State & Otomatisasi Drift Engine
- **1-Click Baseline**: Mendeteksi otomatis container Docker yang aktif, partisi disk, cron job, dan service database (`mysqld`, `postgres`, `redis`, `nginx`).
- **Deteksi Drift Real-Time**: Memberikan peringatan otomatis saat container berhenti mendadak, disk melebihi batas ambang (misal: `> 85%`), cron job terhapus, atau file backup basi.

### 3. 🩺 Penangkapan Diagnostik Root Cause Excerpt
- Saat terjadi kegagalan service atau crash container, Pantau langsung mengekstrak konteks insiden secara instan:
  - Kode keluar (*exit code*) container Docker dan penanda memori habis `OOMKilled`.
  - 50 baris log error terakhir dari `journalctl -u <service>` atau `docker logs <container>`.
- Kronologi diagnostik insiden langsung tersaji pada tab *Timeline* pada modal Host.

### 4. 🚀 Streaming Transfer Antar-Host (Piped Cross-Host Transfer)
- Mengalirkan file dan folder antar dua server remote secara langsung melalui *memory pipe* tanpa perlu disimpan ke disk lokal Pantau terlebih dahulu.
- **Fast Stream Mode**: Memaksimalkan kecepatan transfer via pipa kompresi `tar`.
- **Verified Mode**: Menghitung dan mencocokkan checksum SHA256 ujung-ke-ujung (*end-to-end*) pada server sumber dan target sebelum menyatakan transfer selesai.

### 5. 🛡️ Observabilitas Jaringan & Keamanan
- **Internet Egress & Latensi**: Menguji konektivitas keluar dan latensi ping ke DNS global (`1.1.1.1`).
- **Live Active Sockets**: Merekap daftar IP publik eksternal yang sedang terhubung dan total koneksi aktif.
- **Brute-Force & Failed Login Tracking**: Memantau percobaan login gagal dan indikasi serangan brute-force via `/var/log/auth.log` atau `journalctl _SYSTEMD_UNIT=ssh.service`.
- **Klasifikasi Bidang Serang (Attack Surface)**: Mengidentifikasi port listen yang terbuka, jenis binding IP (`0.0.0.0` vs `127.0.0.1`), serta mengklasifikasikan tingkat risikonya (*Public Internet* vs *Localhost Only*).

### 6. 🔐 System Snapshot Terenkripsi & Sinkronisasi GitHub
- Mengenkripsi seluruh konfigurasi Host, kredensial SSH, dan aturan desired state ke dalam file snapshot `.enc` menggunakan derivasi kunci **Argon2id** dan enkripsi terotentikasi **AES-256-GCM**.
- **Sinkronisasi GitHub Otomatis**: Mendorong (*push*) file snapshot terenkripsi ke repositori GitHub privat secara terjadwal.
- **Disaster Recovery Wizard**: Pulihkan seluruh inventaris monitoring dari nol hanya menggunakan token GitHub + path repositori atau file `.enc` cadangan.

### 7. 💻 Interactive Web Terminal & SFTP File Manager
- **Web Terminal**: Akses shell interaktif di browser berbasis `xterm.js` melalui koneksi WebSocket SSH PTY dengan dukungan warna ANSI dan penyesuaian ukuran terminal.
- **SFTP Explorer & Code Editor**: Eksplorasi direktori remote, unggah/unduh file, ubah izin (*chmod*), serta sunting script dan file konfigurasi langsung menggunakan editor CodeMirror (tema Nord).

### 8. 🌐 Antarmuka Dwibahasa (English & Bahasa Indonesia)
- Tombol pemilih bahasa instan (`🌐 EN` / `🌐 ID`) langsung di bilah navigasi atas (header).
- Kamus translasi lokal zero-dependency tersimpan di `localStorage` (default: Bahasa Inggris).
- Istilah teknis domain kanonikal (*Host*, *Desired State*, *Drift*, *Root Cause Excerpt*, *Snapshot*) tetap dipertahankan sesuai konvensi industri.

---

## 🚀 Panduan Memulai Cepat (Quick Start)

### Opsi 1: Binary Mandiri (Paling Praktis)

Unduh binary terbaru untuk arsitektur server Anda dari [GitHub Releases](https://github.com/herliansyah/pantau/releases):

```bash
# Berikan izin eksekusi pada binary
chmod +x pantau

# Jalankan Pantau dengan port dan lokasi database kustom
./pantau -port 8080 -db data/pantau.db
```

Buka `http://localhost:8080` di browser Anda.  
**Password Default**: `admin` *(segera ganti melalui menu Settings)*.

---

### Opsi 2: Docker Compose

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
      - PORT=8080
      - DB_PATH=/data/pantau.db

volumes:
  pantau-data:
```

```bash
docker compose up -d
```

---

### Opsi 3: Systemd Service (Linux Produksi)

Buat file unit `/etc/systemd/system/pantau.service`:

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

## ⚙️ Referensi Konfigurasi

| Argumen (Flag) | Environment Variable | Default | Keterangan |
| :--- | :--- | :--- | :--- |
| `-port` | `PORT` | `8080` | Port listening HTTP |
| `-db` | `DB_PATH` | `pantau.db` | Lokasi file database SQLite |

---

## 🛠️ Tech Stack

- **Backend Inti**: Golang (`net/http`, `golang.org/x/crypto/ssh`, `pkg/sftp`, `gorilla/websocket`)
- **Database**: Pure-Go SQLite (`modernc.org/sqlite`) dengan mode Write-Ahead Logging (WAL)
- **Frontend**: Single-Page Web App tersemat via `go:embed` (Zero-npm, Vanilla JS, xterm.js, CodeMirror)
- **Keamanan**: Enkripsi snapshot Argon2id + AES-256-GCM, autentikasi admin ter-hash bcrypt

---

## 📄 Lisensi

Didistribusikan di bawah lisensi **MIT License**. Lihat file `LICENSE` untuk rincian selengkapnya.

Dikembangkan dengan ❤️ oleh [Herliansyah](https://github.com/herliansyah).
