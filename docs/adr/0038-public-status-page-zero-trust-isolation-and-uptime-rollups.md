# 0038. Public Status Page, Isolasi Keamanan Zero-Trust, dan Agregasi Daily Uptime Rollup

Menyediakan fitur Public Status Page mandiri tanpa autentikasi pada path `/status` untuk menyajikan status operasional sistem secara real-time, visual bar riwayat ketersediaan (uptime) 90 hari, dan pengumuman insiden resmi (*Status Incident*) dengan batas isolasi keamanan zero-trust, opt-in target eksplisit (*data masking* total), arsitektur cache lock-free in-memory (`atomic.Pointer`) pra-kompresi Gzip dengan proteksi token bucket rate limiter 100% Go stdlib, serta agregasi ketersediaan harian berbasis SQLite UPSERT atomik.

## Konteks dan Masalah

Sebelum arsitektur ini diterapkan, Pantau merupakan sistem monitoring infrastruktur internal yang seluruh antarmuka dan API-nya terlindungi di balik autentikasi sesi admin (cookie `pantau_session` dan 2FA TOTP):
1. **Ketiadaan Visibilitas Status untuk Pengguna Eksternal**: Pengguna akhir, tim pengembang aplikasi, atau klien bisnis tidak dapat memverifikasi status operasional layanan tanpa menanyakan langsung kepada tim SRE/administrator atau memerlukan akun login administratif ke Pantau.
2. **Risiko Kebocoran Data Sensitif Infrastruktur (Data Leakage)**: Menyajikan halaman publik langsung dari dashboard internal membawa risiko eksfiltrasi informasi sensitif tingkat tinggi (seperti alamat IP privat/publik, port SSH internal, username remote, kredensial, struktur rule drift, dan potongan log kegagalan sistem).
3. **Ancaman Lonjakan Trafik Saat Insiden (*The Slashdot / DDoS Effect*)**: Saat terjadi insiden pemadaman atau degradasi performa pada sistem utama, ratusan hingga ribuan pengguna akan secara serentak mengakses halaman status dan melakukan *refresh* berulang kali. Jika endpoint publik mengeksekusi kueri agregasi SQLite 90 hari secara langsung pada setiap request HTTP, basis data embedded Pantau akan mengalami *lock contention* dan saturasi I/O disk, yang berpotensi melumpuhkan proses inspeksi SSH di saat paling kritis.
4. **Ketergantungan CDN Eksternal pada Solusi Pihak Ketiga**: Solusi status page mandiri yang ada (seperti Atlassian Statuspage, Status.io, atau Better Uptime) memerlukan langganan cloud berbayar atau hosting eksternal terpisah. Solusi self-hosted lain kerap memuat font Google, pustaka Bootstrap/Tailwind CDN, atau websocket berat yang melanggar prinsip Pantau: *agentless*, single-binary, dan ramah lingkungan terisolasi (*airgapped*).

## Keputusan Arsitektur

1. **Skema Basis Data Terisolasi & Jaminan Data Masking Total**:
   - Dibuat 4 tabel mandiri yang tidak menduplikasi field privat:
     - `status_page_targets`: Pemetaan opt-in eksplisit (`target_type`, `host_id`, `desired_rule_id`, `public_name`, `group_name`, `sort_order`, `enabled`).
     - `status_page_daily_rollups`: Agregasi ketersediaan harian per target (`target_id`, `date`, `total_checks`, `successful_checks`, `degraded_checks`, `outage_checks`, `uptime_percentage`) dengan constraint `UNIQUE(target_id, date)` dan indeks pencarian cepat `idx_status_rollups_lookup (target_id, date DESC)`.
     - `status_page_incidents`: Pengumuman resmi insiden atau pemeliharaan terkurasi (`title`, `status`, `impact`, `message`, `affected_targets`, `resolved_at`).
     - `status_page_incident_updates`: Catatan kronologis pembaruan penanganan insiden.
   - Entitas internal hanya terekspos melalui `public_name` samaran. Alamat IP, port SSH, username, path file sistem, dan log kesalahan teknis dieliminasi mutlak dari DTO publik.

2. **Arsitektur Agregasi Dual-Trigger (Real-Time Incremental + Hourly Prune)**:
   - **Real-Time UPSERT**: Di akhir siklus inspeksi host (`ins.InspectHost`), target publik yang terhubung langsung di-UPSERT ke `status_page_daily_rollups` untuk tanggal hari ini (`YYYY-MM-DD`). Ini menjamin bar hari ini selalu bergerak real-time tanpa menunggu tengah malam.
   - **Mitigasi Anti-Flapping**: Target hanya diklasifikasikan sebagai `outage` jika mengalami minimal 2 kali kegagalan berturut-turut (*2 consecutive failures*). Kegagalan pertama diklasifikasikan sementara sebagai `degraded` ("Verifying Connectivity") guna menangkal false alarm akibat packet loss sesaat.
   - **Akumulasi Uptime Non-Drift**: Menghindari kesalahan matematis `AVG(daily_uptime)`, persentase 90 hari dihitung dari penjumlahan akumulasi seluruh pemeriksaan (`(SUM(successful) + 0.5 * SUM(degraded)) / SUM(total) * 100`).
   - **Hourly 90d Auto-Prune**: Worker latar belakang membersihkan rekaman yang lebih tua dari 90 hari, menjaga ukuran tabel stabil di bawah 50 KB.

3. **Batas Keamanan Zero-Trust & Lock-Free In-Memory Caching (`atomic.Pointer`)**:
   - Handler `/status` dan `/api/status-page/summary` didaftarkan langsung pada `http.ServeMux` tanpa pembungkus `authMiddleware` dan sepenuhnya mengabaikan cookie `pantau_session`. Tidak ada rute debug atau bypass otentikasi.
   - **Zero DB Hits pada HTTP Read Path**: Snapshot status publik disimpan di memori RAM via `atomic.Pointer[statusPageSnapshot]` lengkap dengan payload JSON mentah, pre-compressed Gzip (`compress/gzip.BestCompression`), dan ETag SHA-256. Mampu melayani > 120.000 req/detik pada latensi sub-milidetik dengan dukungan HTTP 304 *Not Modified*.
   - Rebuild snapshot dijalankan secara asinkron di latar belakang menggunakan saluran debounced (300 ms) saat terjadi event inspeksi baru atau perubahan insiden.

4. **In-Memory Sharded Token Bucket Rate Limiter (Zero-Dependency)**:
   - Menggunakan array 16 shard mutex standar Go stdlib (`shardedRateLimiter`) untuk mencegah lock contention antar-goroutine di bawah beban tinggi.
   - Dikonfigurasi dengan batas 60 req/menit dan burst 30 req per IP, lengkap dengan background reaper setiap 5 menit untuk mencegah kebocoran memori.

5. **Antarmuka Publik Mandiri Airgapped (< 25 KB)**:
   - Template `/status` disimpan di `internal/web/static/status.html`, di-embed via `embed.FS`, dan disajikan dengan header Content Security Policy (CSP) ketat tanpa WebSocket.
   - Visual bar 90 hari dibangun menggunakan 90 elemen mikro-interaktif berbasis CSS flexbox murni dengan tooltip mengambang presisi tinggi, tema ganda otomatis (Dark & Light mode via CSS variables), dan timeline pengumuman insiden aktif.

6. **Antarmuka Manajemen Admin Terpadu**:
   - Tab baru `🌐 Status Page` disematkan pada Action Dialog pengaturan (`settingsModal`) untuk toggle keaktifan global, kustomisasi branding (nama, logo, deskripsi, tautan bantuan), formulir opt-in Status Target, dan modal publikasi/pembaruan Status Incident.

## Konsekuensi

- **Transparansi Layanan Tanpa Biaya Tambahan**: Pengguna eksternal dan tim internal mendapatkan visibilitas ketersediaan layanan 90 hari dan pengumuman insiden secara real-time tanpa perlu berlangganan platform status page pihak ketiga.
- **Ketahanan Maksimal Terhadap Lonjakan Trafik**: Pemanfaatan in-memory caching `atomic.Pointer` dan sharded rate limiter memastikan lonjakan ribuan request pengunjung saat downtime tidak membebani database SQLite atau mengganggu engine pemantauan Pantau.
- **Keamanan Terjamin (Zero-Leakage)**: Model opt-in eksplisit dan isolasi middleware menjamin tidak ada alamat IP, kredensial, atau detail kesalahan teknis internal yang terekspos ke publik.
- **Operasional Airgapped Penuh**: Ketiadaan dependensi CDN atau skrip daring menjamin status page tetap berfungsi mulus pada jaringan terisolasi maupun internet publik.
