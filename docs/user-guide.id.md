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
   - [Resource Metrics, Process Telemetry & Observabilitas Perangkat Keras](#resource-metrics-process-telemetry--observabilitas-perangkat-keras)
   - [Inventarisasi Paket & Audit Higienitas Software](#inventarisasi-paket--audit-higienitas-software)
   - [Mendeteksi Drift & Root Cause Excerpt](#mendeteksi-drift--root-cause-excerpt)
   - [Investigasi Interaktif via Terminal Dock & Terminal Preset](#investigasi-interaktif-via-terminal-dock--terminal-preset)
   - [In-App Activity Console & Live Telemetry Stream](#in-app-activity-console--live-telemetry-stream)
   - [Diagnostik Jaringan Agentless & Analisis Hop](#diagnostik-jaringan-agentless--analisis-hop)
   - [Evaluasi Kelayakan Server dengan Lifecycle Assessment](#evaluasi-kelayakan-server-dengan-lifecycle-assessment)
   - [Evaluasi Komparatif Multi-Host & Matriks Penilaian](#evaluasi-komparatif-multi-host--matriks-penilaian)
5. [Fase 4: Operasi Berkas & Aliran Data Lintas-Host](#5-fase-4-operasi-berkas--aliran-data-lintas-host)
   - [Cross-Host Transfer (Pipa Memori Tanpa Disk Lokal)](#cross-host-transfer-pipa-memori-tanpa-disk-lokal)
   - [Folder Streaming Download Langsung ke Browser](#folder-streaming-download-langsung-ke-browser)
   - [Pin Berkas dengan Catatan Operasional & Global Bookmarks](#pin-berkas-dengan-catatan-operasional--global-bookmarks)
6. [Fase 5: Pengerasan Keamanan, Snapshot & Pemulihan Bencana](#6-fase-5-pengerasan-keamanan-snapshot--pemulihan-bencana)
   - [Two-Factor Authentication (2FA) & 2FA Bypass Flag](#two-factor-authentication-2fa--2fa-bypass-flag)
   - [System Snapshot Terenkripsi & Disaster Recovery GitHub](#system-snapshot-terenkripsi--disaster-recovery-github)
7. [Fase 6: Pemeliharaan Sistem & Pembaruan (Update Checker & Self-Update)](#7-fase-6-pemeliharaan-sistem--pembaruan-update-checker--self-update)
   - [Update Checker & Siklus Deteksi Rilis](#update-checker--siklus-deteksi-rilis)
   - [Proses One-Click Self-Update & Verifikasi Integritas SHA-256](#proses-one-click-self-update--verifikasi-integritas-sha-256)
   - [Diferensiasi Lingkungan Docker vs Standalone Binary](#diferensiasi-lingkungan-docker-vs-standalone-binary)
   - [Kesiapan Lingkungan Airgapped](#kesiapan-lingkungan-airgapped)

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
> Untuk server Linux lawas (*legacy*) seperti CentOS 5/6, Debian 7, atau OpenSSH 4.x/5.x, Pantau secara otomatis mengaktifkan fallback cipher dan algoritma host key warisan (`aes128-cbc`, `3des-cbc`, `diffie-hellman-group1-sha1`, `ssh-dss`, `ssh-rsa`) saat negosiasi SSH. Pantau menerapkan verifikasi host key TOFU (Trust-On-First-Use) yang otomatis mencatat fingerprint host pada koneksi awal sekaligus melindungi sesi berikutnya dari manipulasi Machine-in-the-Middle (MITM).

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
   - Pantau mengeksekusi inspeksi kilat melalui SSH untuk mendeteksi container Docker yang sedang berjalan, partisi disk root, service database/web aktif, dan entri cron job yang terpasang.
   - Sumber daya yang terdeteksi secara otomatis disimpan sebagai acuan baku (**Desired State**) mesin tersebut.
4. Kustomisasi atau tambah aturan manual sesuai kebutuhan operasional.

### Tabel Referensi Aturan Desired State

Pantau mendukung 7 jenis aturan Desired State yang dievaluasi secara otomatis pada setiap siklus inspeksi:

| Jenis (`Kind`) | Target Identifier | Kondisi Diharapkan (`Expected`) | Contoh Target & Expected | Kriteria Drift & Root Cause Excerpt |
| :--- | :--- | :--- | :--- | :--- |
| **`container`** | Nama kontainer Docker | `running` | `nginx` &rarr; `running` | Kontainer `exited`, `dead`, crash. Menyajikan exit code, OOMKilled, dan 50 baris log kontainer. |
| **`service`** | Nama daemon Linux | `active` | `mariadb` &rarr; `active` | Service `inactive` atau `failed`. Menyajikan 50 baris log dari `journalctl` atau syslog. |
| **`port`** | Port TCP atau `ip:port` | `listening` (atau `closed`) | `80` &rarr; `listening` | Port tidak terbuka / proses mati. Memeriksa `ss`/`netstat`. |
| **`disk`** | Mount point partisi | `<N%` (ambang batas) | `/` &rarr; `<85%` | Utilisasi partisi >= ambang batas atau storage IO timeout 5s (hung NFS). |
| **`cron`** | Kata kunci / perintah cron | `configured` | `backup.sh` &rarr; `configured` | Baris perintah tidak ditemukan dalam `crontab -l` user SSH. |
| **`backup`** | Path absolut berkas backup | `fresh_<N>h` (default: 24h) | `/backup/db.sql.gz` &rarr; `fresh_24h` | Berkas tidak ditemukan, berukuran 0 byte, atau usia berkas melampaui N jam. |
| **`process`** | Pola atau nama proses | `running` (atau `stopped`) | `celery` &rarr; `running` | Proses tidak aktif pada sistem (memeriksa `pgrep`/`ps`). |

### Konfigurasi Backup Freshness & Protected Path

- **Backup Freshness**: Daftarkan lokasi file backup target (contoh: `/backup/postgres/daily.sql.gz`) beserta batas kedaluwarsa waktu (misal: harus diperbarui dalam 24 jam terakhir dan ukuran berkas `> 0 byte`). Pantau akan menandai **Drift** jika berkas backup hilang, kosong, atau usang.
- **Protected Path**: Daftarkan path direktori sistem inti (seperti `/etc/`, `/boot/`, `/var/lib/docker/`) ke dalam daftar terproteksi agar terhindar dari ketidaksengajaan operasi ubah, timpa, atau hapus berkas melalui modul SFTP file manager.

---

## 4. Fase 3: Observabilitas, Analisis Drift & Respons Insiden

### Siklus Inspeksi, Inspeksi Massal (Inspect All) & Kesegaran Data

Pantau mengumpulkan telemetri actual state dan memvalidasi desired state melalui siklus inspeksi terjadwal maupun on-demand:

- **Inspeksi Massal (Inspect All)**: Klik tombol `⚡ Inspect All` di bilah atas untuk memicu pembacaan kondisi seluruh armada server secara serentak (asinkron). Eksekusi massal diproses melalui antrean worker pool terbatas (maksimal 5 host bersamaan) dengan proteksi anti-flood cooldown guna mencegah saturasi bandwidth dan lonjakan beban CPU.
- **Inspeksi Mandiri Per-Host**: Klik tombol petir `⚡` pada masing-masing kartu server atau baris tabel, atau gunakan ikon refresh kompak pada header modal detail host (di samping badge status).
- **Anti-Flood Inspection Cooldown**: Guna mencegah pembanjiran koneksi (*connection flooding*), inspeksi manual menerapkan jeda waktu minimum 15 detik per host. Pemicuan inspeksi dalam rentang jeda ini mengembalikan `HTTP 429 Too Many Requests` disertai notifikasi toast peringatan sisa detik tunggu.
- **In-Flight Concurrency Guard**: Mekanisme penguncian aktif pada level host mencegah inspeksi ganda (*in-flight conflict*). Meminta inspeksi ketika proses sebelumnya masih berjalan akan menghasilkan `HTTP 409 Conflict` dan menampilkan animasi spinner pada tombol tindakan hingga proses tuntas.
- **Structured Inspection Runs & Riwayat Audit**: Setiap siklus inspeksi secara otomatis merekam data riwayat audit terstruktur di basis data SQLite internal `inspection_runs` (waktu mulai, durasi roundtrip dalam milidetik, badge status, ringkasan manusiawi, dan rincian diagnostik root cause saat anomali). Operator dapat memantau riwayat ini pada tab **Riwayat Inspeksi** di Workspace Modal Host Detail.
- **Pembersihan Otomatis (Inline Rolling Prune)**: Fitur auto-pruning sebaris tanpa konfigurasi secara otomatis mempertahankan kuota 100 riwayat terbaru per host di SQLite, menjaga ukuran database tetap efisien dan stabil tanpa membebani scheduler latar belakang.
- **Proteksi Hung NFS & Stale Storage Mounts**: Evaluasi partisi disk secara ketat mengisolasi filesystem lokal (`df -lPk /`) untuk melompati remote storage, serta membungkus pengecekan disk dengan batas waktu 5 detik. Mount network storage yang macet (NFS, CIFS) ditandai sebagai indikasi *drift* diagnostik yang jelas tanpa membuat proses inspeksi membeku.
- **Hard Timeout Budget Total 45 Detik**: Setiap siklus inspeksi dibatasi batas waktu absolut maksimum 45 detik. Sesi SSH yang gantung akan diputus paksa di sisi Pantau dan dicatat berstatus *down* untuk menjaga ketersediaan antrean worker.
- **Indikator Kesegaran & Durasi Eksekusi**: Setiap kartu dan baris tabel menyajikan waktu relatif inspeksi terakhir (contoh: `🕒 2m lalu`, `🕒 baru saja`) berdampingan dengan badge durasi roundtrip riil (contoh: `⚡ 320ms`).
- **Pendeteksian Data Usang (Stale Inspection)**: Jika sebuah host tidak berhasil diinspeksi melebihi batas waktu toleransi (> 10 menit atau 2× interval normal), Pantau menampilkan penanda peringatan oranye `⚠️ Data Usang` untuk mencegah false-confidence pada data telemetri lama. Ketika Background Inspection sengaja dinonaktifkan (diisi `0`), peringatan data usang otomatis disupresi untuk menghindari false alarm.
- **Auto-Refresh Dashboard**: Tampilan browser secara otomatis menyinkronkan status kartu host setiap 30 detik tanpa memerlukan reload halaman manual.
- **Konfigurasi Interval Inspeksi & Saklar Nonaktif**: Administrator dapat mengubah frekuensi inspeksi background (default 300 detik / 5 menit, minimal 30 detik, atau setel ke `0` untuk menonaktifkan inspeksi latar belakang agar murni berjalan manual/on-demand) melalui modal **Settings** > tab **General**. Saat dinonaktifkan, badge indikator (`⏸️ Background Inspection Paused`) muncul pada toolbar dashboard dan footer engine.

### Resource Metrics, Process Telemetry & Observabilitas Perangkat Keras

Pengambilan telemetri mengekstrak metrik kapasitas komputasi riil secara berkelanjutan:
- **Laju Disk I/O Real-Time (Speedometer)**: Mengevaluasi kecepatan baca dan tulis disk secara dinamis (B/s) dan akumulasi volume byte dari `/proc/diskstats`. Menghadirkan pill status beban throughput kualitatif (Optimal `< 10 MB/s`, Aktif `10–80 MB/s`, Beban I/O Tinggi `≥ 80 MB/s`) pada Kartu Host dan Modal Detail Host.
- **Kapasitas Absolut Inline**: Bar progress RAM dan Disk menyajikan angka riil `terpakai / total` (misal `4.2 GB / 15.8 GB`) langsung secara inline tanpa penalti layout vertikal.
- **Telemetri Swap**: Baris Swap permanen pada setiap kartu host menyajikan pemakaian memori virtual aktif, atau menampilkan penanda redup `Disabled` jika swap belum dikonfigurasi di server.
- **Beban CPU Ternormalisasi**: Beban antrian sistem (*load average*) dievaluasi terhadap jumlah core CPU fisik dan logis (`nproc`), menampilkan aksen warna peringatan ketika antrian melampaui kapasitas core.
- **Kartu Sys Info**: Modal Detail Host menyajikan tipe model hardware, lapisan virtualisasi, kuantitas core CPU, dan topologi partisi penyimpanan.
- **Top Resource Consumers (Process Telemetry)**: Di setiap siklus inspeksi rutin, Pantau mengekstrak 5 proses teratas pemakan sumber daya dalam tiga dimensi:
  - **Top CPU**: Melacak PID, user, `%cpu`, dan lama eksekusi CPU via `ps -eo pid,user,%cpu,cputime,comm --sort=-%cpu`.
  - **Top RAM (Physical RSS)**: Melacak PID, user, `%mem`, jejak memori RSS, dan nama proses via `ps -eo pid,user,%mem,rss,comm --sort=-rss`.
  - **Top Swap (Virtual Memory)**: Diekstrak secara kondisional hanya saat `SwapUsedBytes > 0` dengan mem-parsing baris `VmSwap:` di `/proc/[0-9]*/status`, mendeteksi pemborosan swap tanpa overhead daemon jarak jauh.
  - **Pintasan Terminal Satu-Klik**: Kartu Top Resource Consumers di tab Overview menyediakan tombol **Open Terminal** untuk langsung membuka sesi shell interaktif ke host terkait guna investigasi cepat.

### Inventarisasi Paket & Audit Higienitas Software

Pantau menyediakan visibilitas inventaris paket perangkat lunak secara agentless lintas berbagai distribusi Linux:
- **Dukungan Polyglot Multi-Distro**: Deteksi bawaan untuk Debian/Ubuntu (`dpkg-query`), RHEL/CentOS/AlmaLinux/Rocky (`rpm`), Alpine (`apk`), dan Arch Linux (`pacman`).
- **Kategorisasi Higienitas Perangkat Lunak**:
  - **Manual Install (`⭐ Manual Install`)**: Paket yang dipasang secara eksplisit oleh administrator melalui manajer paket (misal `apt-mark showmanual`, `dnf history`, `/etc/apk/world`, `pacman -Qqe`), membedakan utilitas operasional dari paket bawaan distro.
  - **Base System**: Paket bawaan instalasi awal sistem operasi (distro bootstrap) dan pustaka dependensi inti.
  - **Dormant Packages (`💤 Dormant Packages`)**: Unit aplikasi/paket manual install yang unit layanan `systemd`-nya berada dalam status inactive, disabled, atau failed selama periode observasi, mempermudah identifikasi potensi celah keamanan atau daemon usang.
- **Tab Packages di Workspace Modal**:
  - Menampilkan nama paket, versi, arsitektur, repositori sumber, dan tanggal instalasi.
  - Fitur pencarian instan via bilah filter real-time.
  - Filter kategori cepat: **Manual Install**, **Dormant Packages**, dan **All Packages**.
  - **Rescan Packages**: Tombol pemindaian ulang on-demand dengan dialog konfirmasi, dilindungi timer cooldown anti-flood 30 detik (`HTTP 429 Too Many Requests`) dan kunci konkurensi latar belakang (`HTTP 409 Conflict`).
  - **Prinsip Non-Destruktif**: Pantau tidak menyediakan tombol uninstall destruktif di web UI untuk mencegah risiko rusaknya dependensi berantai (*cascading broken dependencies*). Administrator diarahkan memanfaatkan Terminal Dock bawaan atau merumuskan aturan Desired State.

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
- **Terminal Header Launcher**: Akses dan kontrol dok terminal dari header navigasi utama. Lencana sesi aktif menampilkan akumulasi sesi PTY di seluruh sistem. Saat dok kosong, mengklik launcher akan langsung membuka dropdown pemilih host untuk memulai sesi baru.
- **Reactive Host Session Badges**: Kartu host pada Grid View dan List View menyematkan badge angka reaktif ketika terdapat sesi shell yang aktif untuk server tersebut.
- **Split-Pane View (`Alt+\`)**: Belah area kerja terminal menjadi dua jendela berdampingan untuk mengamati log dari dua host berbeda secara sinkron.
- **Terminal Preset & Pencarian Live**: Jalankan perintah investigasi rutin yang telah disimpan (misal: `htop`, `journalctl -f`, `docker stats`) baik berlingkup global maupun terikat secara dinamis pada host aktif di tab terminal. Dilengkapi kolom pencarian instan pada dropdown yang menyaring berdasarkan judul preset, perintah, atau tag sesi tmux (`t:tag`).
- **Akses Cepat Pinned Paths Terminal (`📌`)**: Akses daftar berkas dan direktori tersemat untuk host aktif langsung dari bilah alat terminal. Dilengkapi pencarian real-time lintas path dan catatan operasional. Mengklik direktori akan mengeksekusi `cd '<path>'`; mengklik berkas akan menyisipkan path bertanda kutip ke sesi terminal. Lepas sematan secara instan lewat tombol `✕`.
- **Pindah Langsung ke Pengelola Berkas (`📁 Files`)**: Lompat mulus dari tab terminal aktif ke tab Pengelola Berkas host yang langsung membuka direktori kerja terminal saat itu sekaligus meminimalkan dok terminal.
- **Kontrol Zoom Font**: Sesuaikan ukuran teks terminal langsung dari toolbar (`+`, `-`, dan `0` untuk reset).
- **Deteksi Link Terminal Interaktif (`Ctrl+Click` / `Cmd+Click`)**: Link URL yang tercetak di output terminal otomatis terdeteksi. Menekan kombinasi `Ctrl+Click` (Linux/Windows) atau `Cmd+Click` (macOS) langsung membuka situs web publik di tab peramban baru tanpa mengganggu seleksi teks log dan operasi copy-paste terminal.
- **Remote Web Port Tunneling (`localhost:port`)**: Ketika perintah terminal menghasilkan URL loopback remote (misal `http://localhost:3000` atau `127.0.0.1:8080`), mengklik link tersebut memicu dialog konfirmasi modal (*Remote Web Service*) dengan tombol 1-klik untuk membuka layanan via reverse proxy SSH `direct-tcpip` Pantau (`/api/tunnel/:hostId/:port/`) di tab browser baru.
- **Menu Toolbar Dropdown Ports (`🌐 Ports`)**: Akses aplikasi web privat yang berjalan di server remote dengan membuka dropdown `🌐 Ports` di toolbar terminal. Masukkan nomor port (1-65535) dan klik **Buka** untuk meluncurkan sesi tunnel. Port yang baru saja dibuka otomatis tersimpan di memori browser lokal (*localStorage*).
- **Zero-Configuration Remote Browser Bridge**: Perintah CLI di server remote (`gh auth login`, `xdg-open`, Python `webbrowser`, AWS SSO, `npm login`) secara otomatis membuka tab peramban desktop asli di komputer klien via protokol escape terminal (OSC 1337 / OSC 777), mengeliminasi jebakan browser teks headless (`w3m`/`lynx`).
- **Sinkronisasi Direktori Kerja**: Memantau direktori kerja aktif secara otomatis melalui pembaruan judul jendela terminal dan sekuens escape ANSI OSC 7.
- **Modal Stacking & Tombol Escape**: Dialog aksi turunan (form preset, aturan desired state, catatan) menumpuk di atas modal induk secara dinamis dengan layer z-index bertingkat. Penekanan tombol `Escape` menutup dialog paling atas secara berurutan tanpa merusak sesi terminal di bawahnya.

### In-App Activity Console & Live Telemetry Stream

Untuk memberikan visibilitas operasional yang transparan, Pantau dilengkapi dengan konsol diagnostik real-time yang menampilkan aktivitas yang sedang dijalankan daemon dan subsistem setiap saat:

- **Subsistem yang Dimonitor**: Menyiarkan telemetri terstruktur dari 7 modul inti:
  - `inspect`: Siklus inspeksi host, latensi roundtrip, status sehat, host down, dan ringkasan deviasi drift.
  - `ssh`: Upaya injeksi kunci SSH (*key provisioning*), inisialisasi runner, dan kegagalan koneksi.
  - `transfer`: Unggah berkas SFTP dan progres pemindahan berkas lintas-host di latar belakang.
  - `terminal`: Pembukaan, perubahan ukuran, dan penutupan sesi shell PTY interaktif.
  - `snapshot`: Pencadangan basis data terenkripsi otomatis dan sinkronisasi ke repositori GitHub.
  - `system`: Inisialisasi daemon, pemindaian port otomatis, instance file lock, dan graceful shutdown.
  - `auth`: Upaya login administrator, validasi kredensial, dan verifikasi token 2FA.
- **Membuka/Menutup Activity Console**: Klik tombol `⚡ Activity` di bilah footer, atau tekan pintasan keyboard global **`Ctrl + \``** (backtick) dari mana saja di dashboard.
- **Indikator Status & Badge Unread**: Tombol footer menampilkan dot hijau yang berdenyut (*pulsing*) saat terhubung ke live stream. Jika laci tertutup saat terjadi peringatan atau error, badge merah unread counter (`99+`) akan menandai masalah baru hingga laci dibuka.
- **Smart Auto-Scroll & Freeze Feed**: Log baru mengalir secara dinamis ke baris terbawah tabel. Menggulir ke atas akan otomatis menangguhkan auto-scroll agar Anda dapat memeriksa baris historis tanpa terganggu lonjakan tampilan (*Smart Auto-Scroll*). Anda juga dapat menekan tombol **Pause / Resume** untuk membekukan stream secara eksplisit.
- **Filter Severity Level & Pencarian Instan**: Saring baris yang tampil berdasarkan tingkat keparahan (`ALL`, `ERROR`, `WARN`, `INFO`), atau gunakan kotak pencarian untuk menyaring teks pesan, nama host, dan modul secara instan.
- **Detail Metadata JSON Interaktif**: Mengklik baris mana pun pada tabel akan membuka kartu detail metadata atribut terstruktur (`duration_ms`, `port`, `drift_rules`, rincian error) dalam format JSON yang rapi.
- **Ekspor Riwayat Log**: Klik **Export** untuk langsung mengunduh rekaman peristiwa yang ada di buffer ke format berkas teks mentah `.log` atau format `.json` untuk kebutuhan audit, debugging, atau pelaporan insiden.
- **Keamanan & Redaction Filter**: Data kredensial sensitif—termasuk password SSH, private key, token autentikasi, cookie sesi, dan rahasia 2FA—secara deterministik disensor menjadi `[REDACTED]` sebelum dikirimkan ke stream browser.
- **Throttling Performa Ringan**: Saat Activity Console tertutup, operasi perenderan DOM diabaikan 100% untuk menjaga penggunaan CPU dan memori browser tetap minimal.

### Diagnostik Jaringan Agentless & Analisis Hop

Saat menangani kendala konektivitas antar-server, pemblokiran firewall, atau anomali rute data center:

- **Mode Diagnostik**: Eksekusi pengujian non-destruktif langsung dari host jarak jauh:
  - **Ping**: Menguji keterjangkauan host dan roundtrip time via `timeout 14 ping -c 4 -W 2 <target>`.
  - **Traceroute**: Memeriksa hop rute jaringan menggunakan fallback cerdas tanpa dependensi (`tracepath` atau `traceroute`) tanpa butuh hak akses root/raw-socket.
  - **Port Probe (TCP)**: Menguji konektivitas port TCP via socket bash native `/dev/tcp/<target>/<port>`, tanpa butuh `nc`, `telnet`, atau `nmap`.
  - **DNS Resolve**: Memverifikasi jalur resolusi DNS dan latensi nama domain via `getent hosts` dan `nslookup`.
- **Analisis Hop Cerdas**: Memetakan hop rute traceroute secara otomatis ke dalam kartu topologi terstruktur:
  - **Klasifikasi Zona**: Mengidentifikasi apakah tiap hop merupakan `Lokal/VPC Privat` (RFC 1918 / CGNAT), `Transit ISP`, atau `Destinasi`.
  - **Deteksi Titik Terhenti (*Blackhole Drop*)**: Menunjukkan persis hop jaringan tempat paket terhenti jika target tidak tercapai.
  - **Peringatan Lonjakan Latensi**: Menandai lonjakan latensi signifikan (>60ms) antar dua hop berurutan.
- **Dua Titik Akses UI**:
  - Buka melalui tab **Network Diagnostics** di Workspace Modal (dengan host sumber terkunci).
  - Buka secara global via tombol **`⚡ NetDiag`** di header navbar atas (dengan pemilih host sumber aktif).
- **Proteksi Konkurensi & Keamanan**: Menggunakan kunci singleflight in-memory per host untuk mencegah kehabisan sesi SSH. Host yang sedang offline secara otomatis dinonaktifkan dari daftar target antar-host.

### Evaluasi Kelayakan Server dengan Lifecycle Assessment

Pantau menghitung nilai kelayakan sistem secara transparan melalui **Lifecycle Score** (0–100) berdasarkan 6 faktor evaluasi:

1. **OS End-of-Life (EOL)**: Evaluasi masa dukungan resmi rilis distribusi Linux dan versi kernel target.
2. **Productive Lifespan**: Mengukur usia operasional perangkat keras terhadap batas wajar industri (3–5 tahun) melalui tanggal rilis BIOS bare-metal atau waktu deploy OS pada VM cloud.
3. **Saturasi RAM**: Frekuensi kehabisan memori fisik dan aktivitas swap berlebih (*swap thrashing*).
4. **Saturasi Beban CPU**: Rata-rata beban sistem (load average) dinormalisasi terhadap jumlah inti prosesor.
5. **Kapasitas Partisi Disk**: Risiko kepenuhan penyimpanan lokal dan hambatan I/O disk.
6. **Integritas I/O Kernel**: Pemindaian log ring buffer kernel (`dmesg`) terhadap kerusakan sektor disk, remount read-only, atau hardware reset.

Ketika skor memasuki batas kritis, Pantau menghasilkan rekomendasi formal **Hardware Refresh** sebagai dasar pengajuan peremajaan infrastruktur.

### Evaluasi Komparatif Multi-Host & Matriks Penilaian

Saat mengevaluasi disparitas performa infrastruktur, memilih server tujuan migrasi beban kerja, atau mengaudit konsistensi klaster:

- **Matriks Komparasi Interaktif**: Buka melalui tombol **`⚖️ Bandingkan`** di header global atau melalui tab Host Workspace Modal. Pilih 2 hingga 4 host secara bersamaan untuk meninjau telemetri perbandingan berkepadatan tinggi dalam tampilan modal luas (75vw × 80vh).
- **Penilaian 4 Dimensi Terstandarisasi**:
  - **Kapasitas & Beban Komputasi**: Core fisik/virtual, clock speed, arsitektur, dan saturasi beban sistem ternormalisasi per core.
  - **Penyimpanan & I/O Disk**: Utilisasi partisi, integritas berkas sistem, dan kecepatan throughput I/O aktif.
  - **Keandalan Jaringan**: Jitter latensi, reliabilitas transmisi paket, dan status keterjangkauan.
  - **Higienitas Sistem & Siklus Hidup**: Status deviasi aturan konfigurasi, akumulasi paket dorman, masa aktif sistem (*uptime*), dan skor siklus hidup (*lifecycle score*).
- **Filter Derau Deadband 5%**: Selisih performa tipis dalam rentang toleransi ±5% diperlakukan setara/imbang (*tie*) untuk meredam fluktuasi metrik dan mencegah pemeringkatan semu. Host yang stale atau offline dikenakan penalti skor proporsional.
- **Sintesis Keputusan Eksekutif**: Merangkum kesimpulan operasional secara otomatis dalam bahasa manusia, menyoroti juara tiap kategori (*category champions*), rekomendasi penempatan beban kerja, dan peringatan risiko server tersaturasi.
- **Ekspor Markdown Sekali Klik**: Salin papan skor matriks perbandingan dan ringkasan eksekutif ke clipboard dalam format Markdown rapi (tetap berfungsi pada koneksi LAN HTTP polos via mekanisme fallback browser).

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

### Duplikasi Berkas & Folder Aman (Disk Space Safety Check)

Pantau menyediakan fitur penggandaan (*cloning*) lokal untuk berkas atau folder langsung di server remote tanpa proses unduh dan unggah ulang:

1. Klik tombol aksi **📋 Duplikat** pada baris file atau folder yang ingin digandakan.
2. Modal akan menampilkan rekomendasi nama ber-timestamp otomatis:
   - Untuk berkas berektensi: `<nama_file>_<YYYYMMDDHHmmss>.<ekstensi>` (misal: `nginx_20261002043000.conf`), menjaga asosiasi tipe berkas dan *syntax highlighting* tetap aktif.
   - Untuk direktori atau berkas tanpa ekstensi: `<nama_folder>_<YYYYMMDDHHmmss>`.
3. **Disk Space Safety Check**: Pantau secara otomatis memeriksa ketersediaan ruang penyimpanan riil pada partisi target via `df -PB1`. Jika sisa ruang disk kurang dari ukuran item ditambah batas aman (*safety buffer*) 100 MB, proses duplikasi dicegah dengan pesan peringatan merah untuk melindungi server dari risiko kepenuhan disk (*disk exhaustion*).
4. **Pencegahan Tabrakan Nama (Collision Prevention)**: Jika berkas atau folder dengan nama target sudah ada, sistem menolak eksekusi secara ketat untuk mencegah penimpaan data secara tidak sengaja.
5. **Eksekusi Native Remote**: Duplikasi dijalankan langsung di host remote via `cp -a`, menjaga hak izin (*permissions*), kepemilikan, dan timestamp asli. Item berukuran $\ge$ 500 MB menampilkan lencana peringatan bahwa proses mungkin membutuhkan waktu beberapa detik.

### Navigasi Breadcrumb Interaktif & Estetika Antarmuka

SFTP File Manager dilengkapi navigasi dan ergonomi visual yang telah disempurnakan:
- **Interactive Breadcrumb Bar**: Klik segmen folder mana saja pada bilah jalur direktori (contoh: `📁 /` &rarr; `var` &rarr; `www` &rarr; `html`) untuk melompat langsung ke direktori induk tanpa perlu menekan tombol navigasi atas berulang kali.
- **Ikon Berkas Kontekstual**: Baris berkas menampilkan ikon spesifik berdasarkan ekstensi (🐚 script shell, ⚙️ konfigurasi/json/yaml, 📝 dokumen log/teks, 🗜️ arsip, 🖼️ media gambar, 💻 kode pemrograman).
- **Sticky Table Header**: Header kolom dan tombol sorting tetap terpancang di posisi atas (*sticky*) saat menggulir daftar direktori yang panjang.

### Editor Modal Maximize & Tema VS Code Dark+

Saat menyunting skrip, konfigurasi, atau berkas `.env` pada editor CodeMirror terintegrasi:
- **Workspace Modal Maximize**: Klik tombol maximize `⛶` atau klik dua kali (*double-click*) pada header modal untuk memperbesar area editor hingga memenuhi seluruh area layar peramban (100vw &times; 100vh) untuk kenyamanan penelaahan kode yang luas.
- **Palet Tema VS Code Dark+**: Dilengkapi tema gelap modern tersemat (latar belakang `#1e1e1e`, pewarnaan sintaks kontras tinggi untuk *keywords*, *functions*, *variables*, dan *strings*) dengan tipografi monospace modern (`JetBrains Mono`, `Fira Code`, `Consolas`).
- **Deteksi Sintaks Dinamis**: Menyesuaikan mode *syntax highlighting* secara otomatis untuk berkas PHP, JavaScript, Shell, YAML, CSS, HTML, XML, dan C-like berdasarkan ekstensi berkas.

### Pin Berkas dengan Catatan Operasional & Global Bookmarks

Untuk mempermudah akses ke berkas konfigurasi penting, rahasia aplikasi, dan folder log layanan yang aktif ditinjau:

- **Penyematan Berkas & Direktori**: Klik ikon pin (`📌`) pada baris mana pun di File Manager SFTP. Tambahkan catatan operasional opsional (contoh: `Konfigurasi utama nginx produksi` atau `Direktori arsip WAL Postgres`). Baris yang tersemat akan ditandai dengan aksen border dan lencana catatan visual.
- **Bilah Quick Access**: Path berkas/folder yang disematkan untuk host aktif akan muncul sebagai tombol akses cepat (*chips*) pada bilah Quick Access tepat di atas tabel berkas. Mengklik chip akan langsung membuka folder atau memilih berkas terkait.
- **Modal Global Bookmarks**: Akses seluruh berkas tersemat dari semua host secara terpusat dengan mengklik **`🔖 Bookmarks`** pada header navigasi global:
  - **Pencarian Real-Time**: Saring bookmark lintas host, path, dan catatan secara instan.
  - **Editor Terintegrasi**: Buka berkas teks atau konfigurasi tersemat langsung di editor CodeMirror.
  - **Loncat ke Terminal Dock**: Buka tab terminal baru yang secara otomatis telah berada di direktori induk berkas tersemat (`openTerminalForHost` dengan path tujuan).
  - **Sunting Catatan & Lepas Pin**: Perbarui catatan operasional atau lepas sematan berkas langsung dari jendela modal.

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

---

## 7. Fase 6: Pemeliharaan Sistem & Pembaruan (Update Checker & Self-Update)

Pantau menyediakan siklus pemeliharaan semi-otomatis yang aman untuk menjaga ketersediaan fitur terbaru dan perbaikan keamanan tanpa merusak stabilitas pemantauan.

### Update Checker & Siklus Deteksi Rilis
- **Pengecekan Periodik Latar Belakang**: Secara otomatis memeriksa rilis terbaru di upstream GitHub Releases setiap 12 jam sekali, dengan hasil disimpan dalam cache selama 6 jam untuk mencegah pembatasan kuota rate limit GitHub API.
- **Indikator Status UI**: Jika versi baru terdeteksi, badge status dan catatan rilis (*release notes*) akan ditampilkan secara elegan pada footer dashboard dan modal Settings.
- **Pengecekan Manual**: Administrator dapat memicu pengecekan seketika dengan menekan tombol **Periksa Pembaruan** di menu Pengaturan.
- **Pengecekan via CLI**: Menjalankan `pantau -check-update` di terminal akan meminta metadata rilis upstream, menampilkan nomor versi terbaru, URL rilis, serta mencetak catatan rilis (*release notes*) terformat langsung ke output terminal.

### Proses One-Click Self-Update & Verifikasi Integritas SHA-256
Untuk instalasi binary mandiri (Linux dan Windows):
1. Buka menu **Settings** → **System Updates** dan klik **Perbarui Sekarang**.
2. **Validasi Hash SHA-256**: Pantau mengunduh manifest resmi `checksums.txt` dari rilis upstream dan memverifikasi integritas arsip biner sebelum ekstraksi.
3. **Pre-flight Smoke Test**: Sebelum biner lama diganti, Pantau mengekstrak biner baru ke berkas sementara dan mengeksekusi tes smoke (`pantau.tmp -v`). Jika terjadi kegagalan (misal: binary korup atau ketidaksesuaian arsitektur), operasi dibatalkan dan sistem dikembalikan ke kondisi semula (*automatic rollback*).
4. **Penggantian Biner Aman (Atomic Swap)**:
   - Pada Linux: file aktif ditimpa secara atomik menggunakan `os.Rename`.
   - Pada Windows: file aktif yang sedang terkunci di-rename menjadi `pantau.exe.old`, lalu biner baru dipindahkan ke `pantau.exe`. File `.old` akan dibersihkan pada startup berikutnya.
5. **Graceful Restart & Lock Handover**: Klik tombol **Restart Pantau Sekarang**. Server akan menyelesaikan proses yang sedang berjalan, melepaskan instance guard, dan menutup koneksi database SQLite secara aman (*graceful shutdown*). Biner baru di-spawn dengan argumen konfigurasi yang identik dan memanfaatkan `instance.AcquireWithTimeout` (jeda toleransi hingga 3 detik) untuk menunggu pelepasan file lock oleh proses lama secara mulus, lalu antarmuka web akan otomatis memuat ulang saat server aktif kembali.

### Diferensiasi Lingkungan Docker vs Standalone Binary
- Image Docker resmi di GitHub Container Registry (`ghcr.io/herliansyah/pantau:latest`) mendukung multi-arsitektur secara native untuk platform `linux/amd64` dan `linux/arm64`.
- Jika Pantau dijalankan di dalam container Docker, tombol penggantian biner otomatis dinonaktifkan demi menjaga integritas pola kontainerisasi.
- Antarmuka web akan menampilkan panduan penarikan image resmi:
  ```bash
  docker compose pull && docker compose up -d
  ```

### Kesiapan Lingkungan Airgapped
- Pada instalasi server tertutup tanpa akses internet publik (intranet/airgapped), Update Checker beroperasi secara *fail-silent* dengan batas waktu request pendek (3 detik) sehingga tidak memblokir server maupun memunculkan alarm palsu.
- Administrator juga dapat menonaktifkan fitur pengecekan pembaruan sepenuhnya dengan menyertakan flag startup `-disable-update-check` atau variabel lingkungan `PANTAU_DISABLE_UPDATE_CHECK=true`.

---

## 8. Dokumentasi In-App & Viewer Changelog

Pantau dilengkapi dokumentasi operasional mandiri yang disematkan langsung ke dalam biner berekstensi tunggal via `go:embed`:

- **Workspace Modal Ramah Airgapped**: Akses panduan operasional lengkap secara offline tanpa koneksi internet publik melalui **Documentation Modal** (`75vw × 80vh`).
- **Navigasi Multi-Tab Dokumen**: Membaca ringkasan proyek (**README**), buku panduan teknis (**Panduan Pengguna**), serta catatan rilis historis (**Changelog**) langsung dari dalam antarmuka web.
- **Dukungan Dwibahasa & Pengalih Bahasa**: Beralih antara Bahasa Indonesia dan Bahasa Inggris secara instan menggunakan tombol bahasa `🌐 ID` / `🌐 EN` di dalam modal.
- **Daftar Isi Dinamis (Dynamic Table of Contents)**: Menghasilkan navigasi heading otomatis untuk *smooth-scrolling* langsung ke bab bahasan atau nomor versi rilis yang dituju.
- **Titik Akses Universal**: Modal dokumentasi dapat dibuka sebelum login (via tautan bantuan di layar Initial Setup dan Disaster Recovery) maupun setelah login (melalui tombol `(?)` di header utama, klik badge nomor versi di footer dashboard, atau tautan Changelog di modal Settings).

