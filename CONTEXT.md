# Pantau

Sistem pemantauan dan manajemen infrastruktur server berbasis agentless SSH dengan verifikasi kondisi seharusnya (desired state), diagnosis deviasi otomatis, dan manajemen file/terminal.

## Language

**Host**:
Mesin server fisik atau virtual (VPS) Linux yang dikelola dan dipantau melalui koneksi SSH.
_Avoid_: Node, instance, machine, windows target

**Desired State**:
Kumpulan aturan dan ekspektasi yang didefinisikan untuk sebuah Host (misal: service harus aktif, disk di bawah ambang batas, container tertentu harus running).
_Avoid_: Baseline, blueprint, config template

**Actual State**:
Kondisi riil sebuah Host yang didapatkan dari hasil pembacaan inspeksi berkala.
_Avoid_: Current status, telemetry snapshot

**Inspection**:
Proses pengambilan Actual State dari Host secara periodik melalui koneksi SSH tanpa menginstal agen di target.
_Avoid_: Polling, health check run, telemetry scrape

**Drift**:
Ketidaksesuaian atau deviasi antara Actual State dan Desired State pada Host.
_Avoid_: Mismatch, divergence, error state

**Root Cause Excerpt**:
Potongan log diagnostik terakhir (misal: exit code, potongan pesan journalctl atau file log sistem `/var/log/*`) yang diambil otomatis saat Drift terdeteksi.
_Avoid_: Crash dump, debug output

**Lifecycle Assessment**:
Proses evaluasi multi-faktor transparan terhadap Host untuk mengukur kelayakan siklus hidup sistem melalui analisis komparatif status OS EOL, saturasi memori/CPU/disk, integritas fisik I/O kernel, dan batas masa pakai produktif hardware.
_Avoid_: Server evaluation, health audit

**Lifecycle Score**:
Nilai kelayakan sebuah Host (0-100) hasil akhir dari Lifecycle Assessment yang merefleksikan kesiapan perangkat keras dan sistem operasi untuk terus beroperasi atau memerlukan penggantian/upgrade.
_Avoid_: Server grade, health rating

**Productive Lifespan**:
Rentang masa pakai produktif perangkat keras server (standar industri: 3–5 tahun) sebelum memasuki kurva penurunan MTBF (Mean Time Between Failures) dan risiko kegagalan fisik tinggi.
_Avoid_: Hardware durability, server lifetime

**Hardware Refresh**:
Rekomendasi penggantian atau peremajaan server yang telah melampaui masa pakai produktif atau terdepresiasi penuh untuk mencegah kegagalan perangkat keras tak terduga.
_Avoid_: Hardware upgrade, server replacement

**Backup Freshness**:
Status kevalidan backup berdasarkan keberadaan file di path tujuan, timestamp perubahan terbaru (recency), dan ukuran file yang wajar (> 0 byte).
_Avoid_: Backup validation, dump check

**Notification Channel**:
Saluran pengiriman peringatan saat terdeteksi Drift (seperti Telegram bot, webhook, atau in-app dashboard).
_Avoid_: Alert sink, message publisher

**Key Provisioning**:
Proses penyisipan otomatis SSH Public Key Pantau ke dalam `~/.ssh/authorized_keys` di Host target menggunakan autentikasi password sementara (one-time).
_Avoid_: Password sync, credential push

**Cross-Host Transfer**:
Proses pemindahan file atau folder langsung dari Host sumber ke Host tujuan melalui stream pipa relay Pantau tanpa penyimpanan file sementara pada disk master.
_Avoid_: File sync, remote copy, direct scp

**Transfer Job**:
Tugas pemindahan asinkron di latar belakang yang melacak status, akumulasi byte terkirim, kecepatan (MB/s), dan estimasi waktu tersisa (ETA).
_Avoid_: Copy task, sync process

**Folder Streaming Download**:
Proses pengaliran arsip direktori secara on-the-fly langsung dari Host ke response browser pengguna (menggunakan zip jika terpasang di remote atau tar.gz) tanpa menyimpan file arsip di disk master Pantau.
_Avoid_: Folder export, zip dump

**Network Egress**:
Status keterhubungan keluar Host ke internet publik dan pengukuran latensi roundtrip (ms) ke resolver global.
_Avoid_: Internet status, ping check

**Port Exposure**:
Klasifikasi tingkat keterbukaan listening port pada Host (apakah terikat pada interface publik `0.0.0.0`/`*` atau terisolasi lokal `127.0.0.1`) beserta penilaian risiko keamanan service sensitif.
_Avoid_: Open port list, firewall rule

**Socket Profile**:
Ringkasan koneksi TCP/UDP aktif pada Host yang memetakan alamat IP klien eksternal teratas, port tujuan, dan proses yang melayaninya.
_Avoid_: Netstat dump, raw connections

**System Snapshot**:
Arsip mandiri terenkripsi yang memuat seluruh basis data Pantau (konfigurasi Host, kredensial SSH, aturan Desired State, dan pengaturan aplikasi).
_Avoid_: Database dump, backup archive, config export

**Snapshot Passphrase**:
Kunci rahasia berbasis frasa sandi dari pengguna yang digunakan untuk derivasi kunci enkripsi (AES-256-GCM) sebelum System Snapshot disimpan atau dipulihkan.
_Avoid_: Master password, backup pin, encryption key

**Remote Storage Provider**:
Konektor penyimpanan eksternal (GitHub Private Repository) untuk sinkronisasi System Snapshot secara berkala melalui protokol HTTPS API.
_Avoid_: Cloud storage, backup destination, git sync


**Workspace Modal**:
Kontainer modal layar lebar berdimensi tetap (75vw × 80vh di desktop) untuk tugas interaktif observabilitas dan data (Host Detail, Editor, Transfer Queue) tanpa perubahan ukuran layout saat navigasi antar-tab.
_Avoid_: Popup window, dynamic modal, fluid dialog

**Action Dialog**:
Kontainer modal terpusat berdimensi ringkas (lebar ~520px - 740px) untuk formulir input sekuensial, dialog pengaturan (Settings), atau konfirmasi aksi tunggal (Add Host, Direct Transfer, Restore Wizard).
_Avoid_: Small modal, mini popup, submodal

**Documentation Modal**:
Kontainer Workspace Modal interaktif berdimensi tetap (75vw × 80vh) untuk membaca panduan operasional Pantau (README, User Guide, dan Changelog) secara mandiri (airgapped) tanpa ketergantungan jaringan eksternal.
_Avoid_: Help popup, docs reader, guide dialog, manual tab, external wiki

**Changelog**:
Catatan historis kronologis terstruktur (berbasis standar Keep a Changelog) yang mendokumentasikan penambahan fitur, perubahan perilaku, dan perbaikan bug di setiap rilis Pantau, disematkan langsung ke dalam biner aplikasi untuk akses in-app pada lingkungan terisolasi (airgapped).
_Avoid_: Release notes, commit log, version summary, update history

**Host Note**:
Catatan bebas berbasis teks yang disematkan pada Host oleh administrator untuk mencatat konteks operasional atau panduan pemeliharaan spesifik mesin tersebut.
_Avoid_: Host description, server memo, tag

**Global Note**:
Catatan operasional bersama tingkat sistem pada dashboard Pantau untuk pengumuman tim, prosedur operasional darurat, atau catatan serah-terima tugas antar-administrator.
_Avoid_: System message, announcement banner, global memo

**Manual Host Order**:
Urutan urut prioritas tampilan kartu Host pada dashboard Pantau yang diatur secara manual oleh administrator untuk mengelompokkan server utama pada posisi teratas.
_Avoid_: Custom sort, priority list, pinned hosts

**View Mode**:
Format tata letak penyajian daftar Host pada dashboard Pantau, dalam bentuk kartu visual (Grid View) atau baris tabel kompak (List View).
_Avoid_: Layout style, display type, presentation format

**Protected Path**:
Lokasi direktori sistem operasi atau file inti pada Host Linux yang dibatasi secara permanen dari operasi manipulasi (tulis, ubah, unggah, dan hapus) melalui antarmuka manajemen file.
_Avoid_: Blocked folder, blacklisted path, system lock

**Terminal Preset**:
Konfigurasi pintasan perintah terminal interaktif (nama label, perintah shell, target Host opsional, dan preferensi persistensi sesi) yang tersimpan di basis data Pantau untuk eksekusi instan melalui antarmuka web.
_Avoid_: Terminal shortcut, terminal macro, saved command, quick command

**Toast Notification**:
Pemberitahuan mengambang non-blocking di pojok antarmuka Pantau dengan ikon status (sukses, peringatan, error) dan penghilangan otomatis (*auto-dismiss*) untuk mengeliminasi pemblokiran UI thread dari modal dialog bawaan peramban.
_Avoid_: Alert popup, banner alert, notification snackbar

**Confirmation Dialog**:
Dialog aksi ringkas berbasis Promise yang menyajikan konfirmasi eksplisit sebelum eksekusi aksi destruktif atau peringatan batas sistem, menggantikan dialog konfirmasi bawaan peramban.
_Avoid_: Confirm popup, prompt dialog, native confirmation

**Host Group**:
Pengelompokan logis untuk satu atau lebih Host pada dashboard Pantau berdasarkan fungsi server, lingkungan (environment), atau lokasi (misal: "Server Utama", "Testing", "Database Cluster").
_Avoid_: Server cluster, host tag, machine folder

**Terminal Maximize**:
Mode pembesaran antarmuka terminal interaktif hingga memenuhi seluruh area pandang peramban (full-viewport) untuk ruang operasional shell yang lebih luas tanpa memutus sesi SSH atau merusak layout modal.
_Avoid_: Native fullscreen, modal expand, popout window

**Port Auto-Scan**:
Mekanisme deteksi ketersediaan port jaringan lokal secara berurutan saat server web Pantau diinisialisasi dengan port default, untuk menghindari kegagalan proses akibat port yang telah terpakai.
_Avoid_: Port hopping, dynamic port binding

**Self-Contained Web Assets**:
Koleksi seluruh pustaka antarmuka web Pantau (stylesheet CSS, modul terminal xterm, dan editor teks CodeMirror) yang disematkan langsung ke dalam binary aplikasi melalui sistem file tersemat (embedded filesystem) dan disajikan secara lokal, menghilangkan seluruh ketergantungan jaringan eksternal ke CDN publik untuk menjamin operasional penuh pada lingkungan terisolasi (airgapped).
_Avoid_: CDN dependencies, online scripts, external assets

**Terminal Dock**:
Kontainer antarmuka terminal global multi-tab di level aplikasi Pantau yang dapat diminimalkan menjadi bilah dok mengambang (dock bar) atau dimaksimalkan ke seluruh viewport tanpa memutus sesi koneksi shell aktif.
_Avoid_: Terminal popup, floating window, modal shell

**Terminal Tab**:
Entitas sesi terminal independen di dalam Terminal Dock yang merepresentasikan satu koneksi PTY remote ke Host tertentu dengan dukungan penamaan judul dinamis (inline rename).
_Avoid_: Terminal window, shell pane, subterminal

**Two-Factor Authentication (2FA)**:
Mekanisme pengamanan lapis kedua berbasis TOTP (Time-based One-Time Password) opsional untuk autentikasi sesi administrator Pantau.
_Avoid_: Multi-factor authentication, MFA, second password

**TOTP Secret**:
Kunci rahasia bersama (shared secret Base32) yang disimpan di basis data Pantau dan dipasangkan ke aplikasi authenticator pengguna untuk menghasilkan token 6-digit periodik (RFC 6238).
_Avoid_: 2FA key, auth seed, pairing token

**Recovery Code**:
Kumpulan kode acak satu-kali-pakai yang di-generate saat aktivasi 2FA untuk memulihkan akses login ketika administrator tidak dapat mengakses perangkat authenticator.
_Avoid_: Backup pin, emergency password, rescue code

**2FA Bypass Flag**:
Opsi argumen baris perintah (`-disable-2fa`) pada binary Pantau untuk menonaktifkan 2FA secara darurat melalui akses terminal host master.
_Avoid_: Emergency unlock, force reset flag

**Terminal Header Launcher**:
Tombol aksi terminal global terpadu pada header utama Pantau yang memicu tampilan Terminal Dock, dilengkapi badge indikator jumlah sesi PTY aktif secara real-time.
_Avoid_: Floating pill, session pill, dock launcher, terminal shortcut

**Terminal Session Badge**:
Indikator numerik reaktif pada tombol terminal kartu Host dan Terminal Header Launcher yang merefleksikan jumlah sesi shell aktif untuk masing-masing Host atau seluruh sistem.
_Avoid_: Tab counter, active count pill, connection tag

**2FA Lockout**:
Penangguhan sementara proses verifikasi login (jeda pendinginan 30 detik setelah 3 kali kegagalan berturut-turut memasukkan token TOTP atau Recovery Code) untuk memitigasi upaya serangan tebak token otomatis (brute-force).
_Avoid_: Account ban, login block, brute-force penalty

**Stale Inspection**:
Kondisi di mana Actual State sebuah Host belum berhasil diperbarui melampaui ambang batas toleransi (2× interval inspeksi normal), menandakan risiko koneksi SSH terputus, jaringan bermasalah, atau mesin target tidak responsif. Penanda peringatan ini otomatis disupresi ketika Background Inspection dinonaktifkan secara sengaja oleh administrator.
_Avoid_: Outdated metrics, expired telemetry, laggy check

**Background Inspection**:
Mekanisme eksekusi Inspeksi berkala di latar belakang untuk seluruh Host secara otomatis sesuai interval waktu yang dikonfigurasi, yang dapat dinonaktifkan (diatur ke 0 detik) untuk beralih sepenuhnya ke mode inspeksi manual (*On-Demand Inspection*).
_Avoid_: Background polling, auto scrape worker, periodic cron

**Inspect All**:
Mekanisme eksekusi Inspeksi serentak secara asinkron ke seluruh Host yang terdaftar dalam satu tindakan operasional terpadu.
_Avoid_: Bulk ping, refresh all, mass scan

**Update Checker**:
Mekanisme deteksi periodik asinkron terhadap ketersediaan rilis versi resmi terbaru Pantau di repositori upstream tanpa memblokir operasional utama sistem.
_Avoid_: Version polling, release scraper, update sniffer

**Self-Update**:
Proses pengunduhan paket rilis, validasi integritas checksum, dan penggantian mandiri (in-place binary replacement) file executable Pantau yang dipicu atas konfirmasi eksplisit administrator.
_Avoid_: Auto patch, silent updater, live patch, background updater

**Resource Metrics**:
Rangkuman metrik kapasitas komputasi riil sebuah Host yang mencakup kuantitas core CPU, beban antrian proses (*load average*), kapasitas memori fisik (RAM), memori virtual (Swap), dan utilisasi penyimpanan root (Disk).
_Avoid_: Machine specs, server performance counters, hardware telemetry

**Inspection Concurrency Guard**:
Mekanisme penguncian in-flight pada level Host untuk mencegah penumpukan inspeksi paralel (*inspection stampede*) saat inspeksi latar belakang dan inspeksi manual dipicu bersamaan atau ketika server lambat merespons.
_Avoid_: Inspection lock, polling mutex, stampede blocker

**Inspection Run**:
Catatan riwayat satu kali eksekusi Inspeksi pada Host, mendokumentasikan timestamp, durasi eksekusi (ms), status hasil (sukses/gagal/drift), dan rincian diagnostik saat terjadi deviasi atau kegagalan.
_Avoid_: Task log, execution audit, polling history, health log

**Inspection Cooldown**:
Jeda waktu minimum (15 detik) yang diwajibkan antar eksekusi Inspeksi manual pada Host yang sama guna mencegah pembanjiran koneksi SSH (*connection flooding*) dan lonjakan beban CPU target.
_Avoid_: Rate limit window, inspect debounce, retry wait

**Instance Guard**:
Mekanisme proteksi proses tunggal berbasis file lock kernel OS yang terikat pada file database untuk mendeteksi instance Pantau yang sedang berjalan, mencegah eksekusi ganda pada database yang sama, dan memberikan notifikasi lokasi port serta PID aktif kepada pengguna.
_Avoid_: App lock, multi-instance blocker, process mutex

**Directory Filter**:
Penyaringan reaktif instan pada entri berkas dan folder di direktori aktif SFTP File Manager berdasarkan kata kunci nama berkas atau filter tipe entri (semua, folder, berkas) tanpa pemuatan ulang jaringan.
_Avoid_: File search, search bar, deep finder

**File Sorting**:
Pengorganisasian urutan entri berkas dan folder pada antarmuka SFTP File Manager berdasarkan atribut kolom (nama, ukuran, tanggal modifikasi) dengan prioritas entri folder di atas berkas (*folders first*).
_Avoid_: File order, column ordering

**Alert Banner**:
Bilah ringkasan peringatan deviasi sistem pada dashboard Pantau yang menyajikan kuantitas alert aktif dan host terdampak secara non-blocking dengan panel rincian mengambang (*floating overlay*) dan pembersihan massal (*dismiss all*).
_Avoid_: Warning list, alert strip, popup banner

**Alert Auto-Resolution**:
Mekanisme penutupan otomatis terhadap status peringatan aktif pada Host ketika hasil pembacaan inspeksi berikutnya memverifikasi bahwa kondisi sistem telah pulih sepenuhnya ke Desired State tanpa deviasi (*healthy*).
_Avoid_: Auto clear, alert wipe, drift expiry

**Dual-Bucket Pruning**:
Kebijakan penyimpanan riwayat inspeksi pada basis data internal yang mengalokasikan kuota retensi bergulir terpisah antara eksekusi normal dan eksekusi bermasalah (*drift/error/down*) guna menjamin ketersediaan jejak audit deviasi historis tanpa memicu pembengkakan ukuran file.
_Avoid_: Multi-retention, split purge, error archiving

