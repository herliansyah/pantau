<p align="center">
  <a href="#readme">
    <img src=".github/assets/logo.svg" alt="Logo Pantau" width="100" />
  </a>
</p>

<h1 align="center">Pantau</h1>

<p align="center">
  Monitoring server Linux berbasis agentless SSH dan manajemen operasional ringan dalam satu binary tunggal.
</p>

<p align="center">
  <a href="README.md"><b>English</b></a> •
  <a href="README.id.md"><b>Bahasa Indonesia</b></a> •
  <a href="docs/user-guide.id.md"><b>Dokumentasi</b></a> •
  <a href="https://github.com/herliansyah/pantau/releases"><b>Rilis</b></a>
</p>

<p align="center">
  <a href="https://github.com/herliansyah/pantau/releases"><img src="https://img.shields.io/github/v/release/herliansyah/pantau?style=flat-square" alt="Rilis Terbaru" /></a>
  <img src="https://img.shields.io/badge/go-1.22+-00ADD8?style=flat-square&logo=go&logoColor=white" alt="Versi Go" />
  <img src="https://img.shields.io/badge/database-embedded%20sqlite-003B57?style=flat-square&logo=sqlite&logoColor=white" alt="SQLite" />
  <a href="LICENSE"><img src="https://img.shields.io/badge/license-MIT-green?style=flat-square" alt="Lisensi" /></a>
</p>

<p align="center">
  <img src=".github/assets/dashboard.png" alt="Dashboard Pantau" width="100%" />
</p>

---

## Mengapa Pantau?

Sebagian besar perangkat monitoring infrastruktur mengharuskan instalasi, konfigurasi, dan pemeliharaan agen background (seperti Prometheus Node Exporter, Datadog Agent, atau Zabbix Agent) di setiap server yang dikelola. Pada armada server skala kecil hingga menengah, intranet terisolasi (*airgapped*), atau lingkungan dengan sistem operasi lama, pola ini memicu beban pemeliharaan dan memperluas celah keamanan.

**Pantau mengambil pendekatan yang berbeda:**
- **Zero remote agents**: Beroperasi sepenuhnya melalui protokol standar OpenSSH (`port 22`) menggunakan perintah standar POSIX. Server target tidak membutuhkan instalasi daemon tambahan, tidak membuka port telemetri khusus, dan tanpa jejak penggunaan memori/CPU di latar belakang.
- **Single binary mandiri**: Ditulis dalam bahasa Go dengan basis data embedded SQLite dan antarmuka web tanpa npm. Berjalan di mana saja tanpa dependensi eksternal.
- **Operasional terpadu**: Menggabungkan pemantauan metrik, deteksi drift terhadap baseline kondisi yang diharapkan, terminal interaktif multi-tab, manajemen file aman, serta streaming file antar-server langsung dari satu dashboard web.

---

## Arsitektur Sistem

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
| - Standar perintah POSIX        |       | - Standar perintah POSIX        |
| - Docker Engine / systemd       |       | - Docker Engine / systemd       |
| - Tanpa instalasi agen / daemon |       | - Tanpa instalasi agen / daemon |
+---------------------------------+       +---------------------------------+
```

---

## Kapabilitas Utama

### 1. Pemantauan Fleet & Metrik Real-time
Pantau menginspeksi host remote melalui koneksi SSH standar menggunakan perintah non-interaktif POSIX (`/proc`, `ps`, `df`, `ss`).
- **Saturasi sumber daya**: Memantau CPU load, penggunaan RAM, alokasi swap, partisi disk, dan throughput I/O via `/proc/diskstats` tanpa dependensi `iostat`.
- **Telemetri proses dan jaringan**: Mengidentifikasi proses dengan konsumsi CPU/RAM tertinggi, port mendengarkan (*listening ports*), koneksi soket aktif, dan deteksi percobaan login SSH yang gagal (*failed logins*).
- **Inventaris paket dan siklus hidup**: Memindai paket terinstal di berbagai distro Linux (`dpkg`, `rpm`, `apk`, `pacman`), mendeteksi paket/layanan tidak aktif (*dormant*), dan menghitung estimasi masa pakai serta tanggal End-of-Life (EOL) sistem operasi.
- Baca selengkapnya pada [Panduan Inspeksi & Observabilitas](docs/user-guide.id.md#4-fase-3-observabilitas-analisis-drift--respons-insiden).

### 2. Desired State Baseline & Diagnostik Insiden
Definisikan baseline kondisi operasional yang diharapkan untuk kontainer, layanan sistem, batas toleransi disk, dan jadwal backup.
- **Deteksi drift otomatis**: Memberikan peringatan ketika kontainer berhenti mendadak, layanan systemd gagal berjalan, cron job hilang, atau penggunaan partisi disk melampaui ambang batas aman.
- **Tangkapan akar penyebab (Root Cause Excerpt)**: Saat layanan atau kontainer mengalami kegagalan (*crash*), Pantau otomatis mencatat *exit code*, status bendera `OOMKilled`, dan potongan log terakhir dari `journalctl` atau `docker logs`.
- Baca selengkapnya pada [Panduan Desired State & Proteksi](docs/user-guide.id.md#3-fase-2-baseline-desired-state--proteksi-sistem).

### 3. Web Terminal Terpadu & Operasional Remote
Kelola server target langsung dari browser tanpa perlu berganti-ganti aplikasi terminal eksternal.
- **Web terminal multi-tab**: Sesi interaktif PTY berbasis `xterm.js` dan WebSocket, mendukung tampilan terpisah (*split-pane* `Alt+\`), penyesuaian ukuran otomatis, dan *preset* perintah berulang.
- **SFTP file editor & bookmark**: Navigasi struktur direktori remote, sematkan (*pin*) file konfigurasi atau log penting, dan sunting skrip secara langsung dengan *syntax highlighting*.
- **Tunneling port web remote**: Reverse proxy HTTP/WebSocket tanpa agen di atas kanal SSH `direct-tcpip`, memungkinkan akses aman ke aplikasi web lokal di server target (`localhost:port`) tanpa membuka port firewall publik.
- **Bridge terminal desktop lokal**: Buka aplikasi terminal desktop lokal Anda (Windows Terminal, GNOME Terminal, iTerm2, Alacritty) langsung ke server remote dengan 1-klik menggunakan kunci SSH efemeral sementara di RAM.
- Baca selengkapnya pada [Panduan Terminal & Operasional Remote](docs/user-guide.id.md#investigasi-interaktif-via-terminal-dock--terminal-preset).

### 4. Zero-Disk Streaming & Backup Terenkripsi
Transfer data antar-server secara aman dan lindungi seluruh konfigurasi kluster Anda.
- **Transfer streaming lintas host**: Salin file dan folder antar dua server remote melalui *memory pipe* (`tar` streaming), tanpa pernah menyimpan (*spooling*) data ke disk lokal Pantau, dilengkapi verifikasi integritas opsional SHA-256.
- **Snapshot sistem terenkripsi**: Ekspor konfigurasi host, baseline, dan kredensial SSH ke dalam arsip snapshot portabel yang diamankan dengan algoritma Argon2id dan AES-256-GCM.
- **Sinkronisasi pemulihan bencana**: Jadwalkan sinkronisasi otomatis snapshot terenkripsi ke repositori privat GitHub untuk pemulihan cepat saat terjadi bencana (*disaster recovery*).
- Baca selengkapnya pada [Panduan Transfer Streaming Lintas-Host](docs/user-guide.id.md#cross-host-transfer-pipa-memori-tanpa-disk-lokal).

### 5. Arsitektur Ramah Lingkungan Airgapped
Dirancang dari awal untuk kebutuhan kepatuhan keamanan tinggi, VPC tertutup, dan intranet tanpa akses internet publik.
- **Bebas dependensi runtime**: Binary Go mandiri dengan basis data embedded SQLite beroperasi dalam mode Write-Ahead Logging (WAL).
- **Aset web mandiri (Zero-CDN)**: Seluruh pustaka antarmuka, font, ikon, dan skrip klien (`xterm.js`, `CodeMirror`) disematkan langsung melalui `go:embed` tanpa permintaan jaringan ke CDN eksternal.
- **Keamanan lokal teruji**: Autentikasi dua faktor berbasis RFC 6238 TOTP dengan pembuatan QR code murni di sisi browser dan kode pemulihan darurat offline.
- **Pembaruan semi-otomatis**: Pemeriksaan rilis langsung dari antarmuka dengan verifikasi hash SHA-256, uji kelayakan eksekusi (*pre-flight check*), dan penggantian binary secara atomik.

---

## Memulai Cepat

### Opsi 1: Binary Mandiri (Direkomendasikan)

Unduh binary siap pakai untuk arsitektur server Anda dari halaman [GitHub Releases](https://github.com/herliansyah/pantau/releases):

```bash
# Berikan izin eksekusi pada binary
chmod +x pantau

# Jalankan Pantau pada port 8080
./pantau -port 8080 -db data/pantau.db
```

Buka `http://localhost:8080` di browser Anda.  
Password bawaan: `admin` *(Anda akan diminta mengubahnya saat pertama kali masuk)*.

### Opsi 2: Docker Compose

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

### Opsi 3: Layanan Systemd (Server Produksi)

Buat file `/etc/systemd/system/pantau.service`:

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

## Referensi Konfigurasi

Pantau mendukung konfigurasi melalui flag CLI atau variabel lingkungan (*environment variables*):

| Flag | Variabel Lingkungan | Default | Deskripsi |
| :--- | :--- | :--- | :--- |
| `-port` | `PANTAU_PORT` (atau `PORT`) | `8080` | Port listening HTTP (otomatis memindai `8080`–`8099` jika port default terpakai) |
| `-db` | `PANTAU_DB` (atau `DB_PATH`) | `pantau.db` | Jalur file basis data SQLite |
| `-open` | - | `true` (Win) / `false` | Membuka browser default secara otomatis saat dijalankan |
| `-disable-2fa` | - | `false` | Flag darurat untuk menonaktifkan 2FA TOTP langsung dari terminal host |
| `-check-update` | - | `false` | Memeriksa ketersediaan versi rilis terbaru lalu keluar |
| `-update` | - | `false` | Memperbarui executable Pantau ke versi rilis terbaru lalu keluar |
| `-disable-update-check` | `PANTAU_DISABLE_UPDATE_CHECK` | `false` | Menonaktifkan pengecekan rilis otomatis (disarankan untuk lingkungan airgapped) |
| `-log-level` | `PANTAU_LOG_LEVEL` | `error` | Tingkat log konsol (`debug`, `info`, `warn`, `error`, `off`) |
| `-q`, `-quiet` | - | `false` | Menekan log konsol non-error (sama dengan `-log-level=error`) |
| `-v`, `-version` | - | - | Menampilkan informasi versi Pantau lalu keluar |

---

## Catatan Keamanan & Produksi

- **Eksposur jaringan**: Pantau adalah panel kontrol administratif yang memiliki wewenang mengeksekusi perintah shell dan memanipulasi file pada server target. **Jangan pernah membuka port Pantau langsung ke internet publik** tanpa memasangnya di balik reverse proxy yang aman (misalnya Caddy, Nginx, atau Cloudflare Access) dengan enkripsi TLS dan pembatasan IP.
- **Manajemen kunci SSH**: Server target dapat dihubungkan menggunakan injeksi kata sandi satu kali atau penempatan kunci publik manual ke `~/.ssh/authorized_keys`. Pantau menggunakan pasangan kunci SSH khusus dan segera menghapus kata sandi dari memori setelah injeksi berhasil.
- **Pemulihan bencana**: Selalu ekspor dan cadangkan snapshot konfigurasi Anda secara berkala. Snapshot dilindungi menggunakan enkripsi terautentikasi Argon2id dan AES-256-GCM.

Untuk panduan pengerasan keamanan dan praktik terbaik operasional, pelajari [Panduan Keamanan Administrator](docs/user-guide.id.md#6-fase-5-pengerasan-keamanan-snapshot--pemulihan-bencana).

---

## Lisensi

Didistribusikan di bawah **Lisensi MIT**. Lihat [LICENSE](LICENSE) untuk rincian selengkapnya.

Dikembangkan oleh [Herliansyah](https://github.com/herliansyah).
