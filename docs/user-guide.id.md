# Panduan Administrator & Playbook Operasional Pantau

[**English**](user-guide.md) • [**Bahasa Indonesia**](user-guide.id.md)

Selamat datang di **Panduan Administrator Pantau**. Dokumen ini menyajikan panduan operasional (*operational playbook*) yang dirancang khusus bagi administrator sistem, insinyur DevOps, dan tim reliabilitas infrastruktur (SRE) yang mengelola kluster server Linux menggunakan Pantau.

Pantau beroperasi **100% agentless** melalui protokol standar SSH (`port 22`). Sistem target sama sekali **tidak memerlukan instalasi daemon atau agen telemetri latar belakang**. Seluruh evaluasi status, pengaliran berkas, dan penarikan diagnostik dieksekusi secara langsung melalui utilitas standar POSIX.

---

## 📑 Daftar Isi

1. [Arsitektur & Batas Kepercayaan (Trust Boundaries)](#1-arsitektur--batas-kepercayaan-trust-boundaries)
2. [Fase 1: Onboarding & Manajemen Inventaris Host](#2-fase-1-onboarding--manajemen-inventaris-host)
   - [Menambahkan Host & Key Provisioning](#menambahkan-host--key-provisioning)
   - [Pengelompokan Host Group & Manual Host Order](#pengelompokan-host-group--manual-host-order)
3. [Fase 2: Baseline Desired State & Proteksi Sistem](#3-fase-2-baseline-desired-state--proteksi-sistem)
   - [Membuat 1-Click Desired State Baseline](#membuat-1-click-desired-state-baseline)
   - [Konfigurasi Backup Freshness & Protected Path](#konfigurasi-backup-freshness--protected-path)
4. [Fase 3: Observabilitas, Analisis Drift & Respons Insiden](#4-fase-3-observabilitas-analisis-drift--respons-insiden)
   - [Siklus Inspeksi, Inspeksi Massal & Kesegaran Data](#siklus-inspeksi-inspeksi-massal-inspect-all--kesegaran-data)
   - [Mendeteksi Drift & Root Cause Excerpt](#mendeteksi-drift--root-cause-excerpt)
   - [Investigasi Interaktif via Terminal Dock & Terminal Preset](#investigasi-interaktif-via-terminal-dock--terminal-preset)
   - [Evaluasi Kelayakan Server dengan Lifecycle Assessment](#evaluasi-kelayakan-server-dengan-lifecycle-assessment)
5. [Fase 4: Operasi Berkas & Aliran Data Lintas-Host](#5-fase-4-operasi-berkas--aliran-data-lintas-host)
   - [Cross-Host Transfer (Pipa Memori Tanpa Disk Lokal)](#cross-host-transfer-pipa-memori-tanpa-disk-lokal)
   - [Folder Streaming Download Langsung ke Browser](#folder-streaming-download-langsung-ke-browser)
6. [Fase 5: Pengerasan Keamanan, Snapshot & Pemulihan Bencana](#6-fase-5-pengerasan-keamanan-snapshot--pemulihan-bencana)
   - [Two-Factor Authentication (2FA) & 2FA Bypass Flag](#two-factor-authentication-2fa--2fa-bypass-flag)
   - [System Snapshot Terenkripsi & Disaster Recovery GitHub](#system-snapshot-terenkripsi--disaster-recovery-github)

---

## 1. Arsitektur & Batas Kepercayaan (Trust Boundaries)

Pantau mengeksekusi perintah jarak jauh dan membaca telemetri server melalui koneksi SSH standar.

- **Control Plane**: Berupa binary tunggal Go yang berjalan di server manajemen atau mesin bastion administrator, didukung basis data tersemat SQLite (`pantau.db`) dengan mode WAL (Write-Ahead Logging).
- **Remote Host**: Setiap mesin fisik atau virtual (VPS) Linux (Ubuntu, Debian, RHEL, CentOS 6–9) yang dapat dijangkau melalui jaringan SSH.
- **Batas Kepercayaan**: Pantau membuat pasangan kunci privat/publik RSA 4096-bit saat pertama kali dijalankan. Host target memberikan izin akses dengan menempatkan kunci publik Pantau ke dalam berkas `~/.ssh/authorized_keys` milik pengguna manajemen (`root` atau pengguna khusus dengan privilese `sudo` tanpa kata sandi).

```
+-------------------+             SSH Port 22 (Agentless)             +-----------------------+
|  Pantau Control   | ==============================================> |   Remote Linux Host   |
|  Plane (Go + DB)  | < - - - - - - - - - - - - - - - - - - - - - - - |  (POSIX df, ps, logs) |
+-------------------+          Actual State & Telemetri               +-----------------------+
```

---

## 2. Fase 1: Onboarding & Manajemen Inventaris Host

### Menambahkan Host & Key Provisioning

Untuk menghubungkan **Host** baru ke dalam Pantau:

1. Klik tombol **Add Host** pada dashboard utama untuk memunculkan **Action Dialog**.
2. Masukkan parameter koneksi:
   - **Label**: Nama penanda yang unik (contoh: `prod-db-primary`).
   - **IP / Hostname**: Alamat IP publik/privat atau hostname yang dapat di-resolve.
   - **SSH Port**: Port standar `22` atau port kustom daemon SSH target.
   - **SSH User**: Akun `root` atau pengguna dengan hak akses sudo.
3. Pilih metode autentikasi:
   - **Existing SSH Key**: Gunakan opsi ini jika host sudah memiliki kunci publik Pantau di `~/.ssh/authorized_keys`.
   - **Key Provisioning (One-Time Password)**: Jika menghubungkan server yang benar-benar baru, masukkan kata sandi target sekali saja. Pantau akan membuat koneksi awal, menyisipkan kunci publik RSA 4096-bit secara idempoten ke `~/.ssh/authorized_keys`, lalu langsung menghapus kata sandi dari memori RAM.
4. Klik **Save & Test Connection**. Pantau akan melakukan uji koneksi (*handshake*) untuk memvalidasi akses.

> [!NOTE]
> Untuk server Linux lawas (*legacy*) seperti CentOS 6, Debian 7, atau OpenSSH 5.3+, Pantau secara otomatis mengaktifkan fallback cipher warisan (`aes128-cbc`, `3des-cbc`, `diffie-hellman-group1-sha1`, `ssh-dss`) saat negosiasi SSH.

### Pengelompokan Host Group & Manual Host Order

Ketika jumlah server bertambah banyak:
- **Host Group**: Kelompokkan host berdasarkan fungsi sistem atau lingkungan (contoh: `Database Cluster`, `Edge Proxies`, `Testing Environment`). Grup akan dipisahkan secara visual baik pada **View Mode** Grid maupun List.
- **Manual Host Order**: Atur urutan prioritas server utama agar selalu tampil di posisi teratas dashboard melalui pengurutan manual.
- **Host Note & Global Note**: Gunakan **Host Note** untuk mencatat catatan operasional spesifik per server (misal: `"Master PostgreSQL; node replikasi ada di db-02"`). Gunakan **Global Note** untuk pengumuman sistem bersama atau catatan serah-terima giliran tugas antar-administrator.

---

## 3. Fase 2: Baseline Desired State & Proteksi Sistem

Pantau tidak hanya menampilkan metrik pasif; sistem ini secara aktif memverifikasi keselarasan sistem terhadap aturan acuan yang telah Anda tentukan.

### Membuat 1-Click Desired State Baseline

1. Buka **Workspace Modal** host dengan mengklik kartu server pada dashboard.
2. Beralih ke tab **Desired State**.
3. Klik tombol **1-Click Baseline**:
   - Pantau mengeksekusi inspeksi kilat melalui SSH untuk mendeteksi container Docker yang sedang berjalan, partisi mount disk, entri cron job yang terpasang, dan port jaringan yang sedang listening.
   - Sumber daya yang terdeteksi secara otomatis disimpan sebagai acuan baku (**Desired State**) mesin tersebut.
4. Kustomisasi ambang batas aturan sesuai toleransi Anda:
   - **Disk Usage**: Atur batas persentase maksimal (misal: peringatan jika kapasitas root filesystem `> 85%`).
   - **Service & Container Check**: Tetapkan container Docker esensial (seperti `nginx`, `postgres`, `redis`) yang wajib berstatus `running`.
   - **Memory & Swap Saturation**: Pasang ambang batas peringatan saturasi RAM.

### Konfigurasi Backup Freshness & Protected Path

- **Backup Freshness**: Daftarkan lokasi file backup target (contoh: `/backup/postgres/daily.sql.gz`) beserta batas kedaluwarsa waktu (misal: harus diperbarui dalam 24 jam terakhir dan ukuran berkas `> 0 byte`). Pantau akan menandai **Drift** jika berkas backup hilang, kosong, atau usang.
- **Protected Path**: Daftarkan path direktori sistem inti (seperti `/etc/`, `/boot/`, `/var/lib/docker/`) ke dalam daftar terproteksi agar terhindar dari ketidaksengajaan operasi ubah, timpa, atau hapus berkas melalui modul SFTP file manager.

---

## 4. Fase 3: Observabilitas, Analisis Drift & Respons Insiden

### Siklus Inspeksi, Inspeksi Massal (Inspect All) & Kesegaran Data

Pantau mengumpulkan telemetri actual state dan memvalidasi desired state melalui siklus inspeksi terjadwal maupun on-demand:

- **Inspeksi Massal (Inspect All)**: Klik tombol `⚡ Inspect All` di bilah atas untuk memicu pembacaan kondisi seluruh armada server secara serentak (asinkron).
- **Inspeksi Mandiri Per-Host**: Klik tombol petir `⚡` pada masing-masing kartu server atau gunakan tombol *Run Immediate Inspection* di modal detail host.
- **Indikator Kesegaran Data (Inspection Recency)**: Setiap kartu dan baris tabel menyajikan waktu relatif inspeksi terakhir (contoh: `🕒 2m lalu`, `🕒 baru saja`) beserta tooltip jam presisi.
- **Pendeteksian Data Usang (Stale Inspection)**: Jika sebuah host tidak berhasil diinspeksi melebihi batas waktu toleransi (> 10 menit atau 2× interval normal), Pantau menampilkan penanda peringatan oranye `⚠️ Data Usang` untuk mencegah false-confidence pada data telemetri lama.
- **Auto-Refresh Dashboard**: Tampilan browser secara otomatis menyinkronkan status kartu host setiap 30 detik tanpa memerlukan reload halaman manual.
- **Konfigurasi Interval Inspeksi**: Administrator dapat mengubah frekuensi inspeksi background (default 300 detik / 5 menit, minimal 30 detik) melalui modal **Settings** > tab **Access**.

### Mendeteksi Drift & Root Cause Excerpt

Pada setiap siklus **Inspection** berkala (standar: setiap 5 menit) atau saat dipicu manual:

1. Pantau membandingkan kondisi riil (**Actual State**) target terhadap acuan baku (**Desired State**).
2. Jika ditemukan anomali atau deviasi (misal: container aplikasi mendadak berhenti atau partisi disk melampaui ambang batas), status host akan berubah menjadi **Drift**.
3. **Automated Root Cause Excerpt**: Pantau secara otonom menarik potongan bukti diagnostik penyebab gangguan:
   - Exit code container beserta indikator terminasi kehabisan memori (`OOMKilled`).
   - Potongan 50 baris log terakhir penyebab crash melalui `docker logs --tail 50 <container>` atau `journalctl -u <service> -n 50 --no-pager`.
4. **Root Cause Excerpt** langsung disajikan di lini masa insiden pada **Workspace Modal** dan dikirimkan ke **Notification Channel** (Telegram bot / Webhook).

### Investigasi Interaktif via Terminal Dock & Terminal Preset

Saat insiden memerlukan penanganan terminal langsung:

- **Terminal Dock**: Buka sesi shell interaktif multi-tab berbasis WebSocket PTY (`xterm.js`).
- **Split-Pane View (`Alt+\`)**: Belah area kerja terminal menjadi dua jendela berdampingan untuk mengamati log dari dua host berbeda secara sinkron.
- **Terminal Preset**: Jalankan perintah investigasi rutin yang telah disimpan (misal: `htop`, `journalctl -f`, `docker stats`) hanya dengan satu kali klik.
- **Session Pill**: Minimalkan dok terminal menjadi lencana mengambang di pojok kanan bawah. Perintah shell yang sedang berjalan (seperti proses kompilasi atau tailing log) akan terus aktif di latar belakang saat Anda memeriksa dashboard.

### Evaluasi Kelayakan Server dengan Lifecycle Assessment

Pantau menghitung nilai kelayakan sistem secara transparan melalui **Lifecycle Score** (0–100) berdasarkan 6 faktor evaluasi:

1. **OS End-of-Life (EOL)**: Evaluasi masa dukungan resmi rilis distribusi Linux dan versi kernel target.
2. **Productive Lifespan**: Mengukur usia operasional perangkat keras terhadap batas wajar industri (3–5 tahun) melalui tanggal rilis BIOS bare-metal atau waktu deploy OS pada VM cloud.
3. **Saturasi RAM**: Frekuensi kehabisan memori fisik dan aktivitas swap berlebih (*swap thrashing*).
4. **Saturasi Beban CPU**: Rata-rata beban sistem (load average) dinormalisasi terhadap jumlah inti prosesor.
5. **Kapasitas Partisi Disk**: Risiko kepenuhan penyimpanan lokal dan hambatan I/O disk.
6. **Integritas I/O Kernel**: Pemindaian log ring buffer kernel (`dmesg`) terhadap kerusakan sektor disk, remount read-only, atau hardware reset.

Ketika skor memasuki batas kritis, Pantau menghasilkan rekomendasi formal **Hardware Refresh** sebagai dasar pengajuan peremajaan infrastruktur.

---

## 5. Fase 4: Operasi Berkas & Aliran Data Lintas-Host

### Cross-Host Transfer (Pipa Memori Tanpa Disk Lokal)

Pemindahan basis data berukuran besar atau arsip direktori antar-dua server Linux umumnya membutuhkan penyimpanan perantara atau setup manual `scp`. Pantau mengalirkan data antar-host **tanpa menyimpan data perantara di disk lokal Pantau**.

```
+-------------------+        Piped Stream (Aliran RAM)              +-------------------+
|  Remote Host A    | ============================================> |  Remote Host B    |
|  (Sumber: tar)    |                 via Pantau                    |  (Tujuan: untar)  |
+-------------------+                                               +-------------------+
```

1. Buka file explorer pada **Workspace Modal** milik Host sumber.
2. Pilih file atau folder yang hendak dipindahkan, lalu klik **Cross-Host Transfer**.
3. Tentukan Host tujuan beserta path direktori target.
4. Pilih mode transfer:
   - **Fast Stream Mode**: Aliran data berkecepatan tinggi memanfaatkan pipa *tar stream*.
   - **Verified Mode**: Menghitung dan mencocokkan checksum SHA256 secara end-to-end di kedua host sebelum transfer dinyatakan selesai.
5. Pantau progres pemindahan pada antrean **Transfer Job** latar belakang (menampilkan kecepatan transfer MB/s, akumulasi byte, dan estimasi waktu tersisa).

### Folder Streaming Download Langsung ke Browser

Untuk mengunduh satu struktur folder dari server target ke komputer kerja Anda:
- Pilih folder pada manajer berkas SFTP, lalu klik **Download as Archive**.
- Pantau akan memampatkan folder secara instan (*on-the-fly* menggunakan `zip` jika terpasang di target, atau `tar.gz`) dan mengalirkannya langsung ke antarmuka unduhan browser Anda tanpa menyisakan sampah arsip di server Pantau.

---

## 6. Fase 5: Pengerasan Keamanan, Snapshot & Pemulihan Bencana

### Two-Factor Authentication (2FA) & 2FA Bypass Flag

Untuk melindungi akses administratif dari kebocoran kata sandi:

1. Masuk ke **Settings** → **Two-Factor Authentication (2FA)**.
2. Pantau akan menampilkan **TOTP Secret** (RFC 6238) beserta kode QR yang di-generate **100% offline** langsung di browser Anda (tanpa dependensi API pihak ketiga).
3. Pindai kode QR menggunakan aplikasi authenticator standar (Google Authenticator, Aegis, 1Password, Bitwarden).
4. Simpan **8 Recovery Code** yang diberikan ke tempat penyimpanan dokumen rahasia offline yang aman.
5. Masukkan 6-digit token untuk mengaktifkan proteksi 2FA.

#### Mitigasi & Akses Darurat:
- **2FA Lockout**: Jika terjadi 3 kali kegagalan verifikasi berturut-turut, sistem otomatis mengunci proses login selama 30 detik untuk menangkis serangan tebak otomatis (*brute-force*).
- **2FA Bypass Flag**: Jika ponsel authenticator dan seluruh Recovery Code hilang, administrator yang memiliki akses shell ke mesin host Pantau dapat me-restart biner dengan flag darurat:
  ```bash
  ./pantau -port 8080 -db /data/pantau.db -disable-2fa
  ```
  Perintah ini akan langsung menonaktifkan konfigurasi TOTP dari SQLite dan memulihkan akses login administrator.

### System Snapshot Terenkripsi & Disaster Recovery GitHub

**System Snapshot** adalah berkas arsip mandiri terenkripsi yang memuat seluruh basis data Pantau (inventaris host, kredensial SSH terenkripsi, aturan Desired State, dan preferensi aplikasi).

#### Membuat & Mengenkripsi Snapshot:
1. Masuk ke **Settings** → **Backup & Disaster Recovery**.
2. Masukkan **Snapshot Passphrase** yang kuat. Pantau akan melakukan derivasi kunci menggunakan algoritma **Argon2id** dan mengenkripsi seluruh database menggunakan **AES-256-GCM**.
3. Unduh berkas `.enc` yang dihasilkan untuk cadangan offline.

#### Sinkronisasi Otomatis ke Remote Storage Provider (GitHub):
- Daftarkan GitHub Personal Access Token (PAT dengan izin `repo`) dan target repositori privat (`username/pantau-backups`).
- Tentukan jadwal sinkronisasi berkala (misal: harian). Pantau akan mengunggah berkas `.enc` terenkripsi secara otomatis ke repositori GitHub privat tersebut.
- **Tips Multi-Server**: Jika Anda menjalankan Pantau di beberapa server (misal: di kantor dan di home lab) dengan repositori GitHub yang sama, ubah kolom **File Path in Repo** di masing-masing server (misal: `snapshots/kantor.enc` dan `snapshots/homelab.enc`) agar backup tidak saling menimpa.


#### Rekonstruksi Kluster dari Nol (Disaster Recovery Wizard):
Jika server master Pantau mengalami kerusakan total:
1. Siapkan binary Pantau baru di mesin baru.
2. Jalankan aplikasi; pada layar inisialisasi awal, pilih opsi **Restore from System Snapshot**.
3. Unggah file `.enc` cadangan Anda atau masukkan kredensial repositori privat GitHub Anda.
4. Masukkan **Snapshot Passphrase**. Pantau akan memverifikasi integritas data, mendekripsi basis data SQLite WAL, dan langsung melanjutkan pemantauan seluruh Host secara otomatis.
