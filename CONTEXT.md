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

**Lifecycle Score**:
Nilai kelayakan sebuah Host (0-100) berdasarkan status dukungan OS (termasuk deteksi End-Of-Life untuk distribusi legacy/pra-systemd), beban sumber daya jangka panjang, dan indikasi kegagalan perangkat keras.
_Avoid_: Server grade, health rating

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
Kontainer modal terpusat berdimensi ringkas (lebar ~520px - 600px) untuk formulir input sekuensial, dialog pengaturan (Settings), atau konfirmasi aksi tunggal (Add Host, Direct Transfer, Restore Wizard).
_Avoid_: Small modal, mini popup, submodal

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
