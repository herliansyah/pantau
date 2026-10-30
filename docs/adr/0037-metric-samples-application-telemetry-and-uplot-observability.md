# 0037. Observabilitas Metric Samples, Telemetri Aplikasi, dan Visualisasi uPlot

Menyediakan sistem perekaman dan visualisasi telemetri berbasis time-series (Metric Samples) di dalam basis data embedded SQLite dengan kebijakan masa simpan terkonfigurasi (Metric Retention) dan pembersihan otomatis (auto-prune berkala), probing metrik performa aplikasi ringan (Application Metric Probe) berbasis SSH terisolasi dengan preset terintegrasi, visualisasi dual-tier menggunakan pustaka ringan uPlot (Canvas 2D zero-dependency) untuk modal observabilitas detail serta mini-sparkline SVG murni pada kartu host, dan API query time-series berperforma tinggi dengan downsampling dinamis di level SQL.

## Konteks dan Masalah

Sebelum arsitektur ini diterapkan, Pantau hanya menyajikan metrik snapshot sesaat (*instantaneous snapshot*) dari Actual State host (seperti CPU load saat ini, persentase RAM, utilisasi partisi disk, dan throughput I/O jaringan/disk saat inspeksi berlangsung):
1. **Ketiadaan Visibilitas Tren Temporal**: Operator dan tim SRE tidak dapat melihat riwayat tren beban sumber daya hardware dalam 1 jam, 6 jam, 24 jam, atau 7 hari terakhir. Lonjakan CPU singkat yang terjadi di tengah malam atau pola kebocoran memori (memory leak) lambat yang terjadi berhari-hari tidak dapat terdeteksi tanpa pengamatan visual historis.
2. **Ketiadaan Telemetri Level Aplikasi**: Beban performa server Linux sangat ditentukan oleh layanan aplikasi utama (seperti Nginx, MySQL, PHP-FPM, Redis, dan PostgreSQL). Tanpa metrik aplikasi (misalnya: *active connections*, *QPS*, *slow queries*, *active workers*, *instantaneous ops/sec*), administrator harus masuk ke terminal dan menjalankan perintah diagnosis secara manual.
3. **Kendala Dependensi Eksternal & Filosofi Agentless Pantau**: Solusi monitoring tradisional (seperti Prometheus + Node Exporter + Grafana, Telegraf + InfluxDB, atau Datadog) memerlukan instalasi agen latar belakang berat di setiap target, port jaringan terbuka tambahan, dan sistem TSDB terdistribusi terpisah. Hal ini bertentangan dengan prinsip Pantau: *agentless*, single-binary, ramah lingkungan *airgapped*, dan bebas dependensi eksternal.
4. **Tantangan Efisiensi Penyimpanan & Performa Kueri**: Menyimpan sampel telemetri berkala (setiap 30-60 detik) untuk puluhan host pada database embedded SQLite dapat mengakibatkan pembengkakan ukuran disk (*disk bloat*) jika tidak dikontrol dengan retensi terstruktur, serta waktu render grafik yang lambat jika transfer data mentah ke antarmuka web berukuran puluhan megabyte.

## Keputusan Arsitektur

1. **Skema Basis Data Metric Samples & Auto-Prune Terjadwal**:
   - Dibuat tabel flat `metric_samples` (`id`, `host_id`, `metric`, `value`, `recorded_at`) dengan composite index `(host_id, metric, recorded_at)` untuk kueri cepat rentang waktu per-metrik, dan index `(recorded_at)` untuk akselerasi pembersihan data usang.
   - Mengimplementasikan worker latar belakang periodik (`startMetricsPruneWorker`) yang berjalan setiap 1 jam, mengeksekusi kueri `DELETE FROM metric_samples WHERE recorded_at < cutoff` berdasarkan konfigurasi retensi global (`metric_retention_days`, default: 7 hari).
   - Mekanisme transaksi batch (`BEGIN TRANSACTION ... COMMIT`) saat menyimpan metrik hasil inspeksi untuk meminimalkan I/O disk dan lock SQLite WAL.

2. **Kontrol Keaktifan Tri-State (Global & Per-Host Override)**:
   - Pengaturan global `metrics_enabled` (default: `true`) dan `metric_retention_days` (default: `7`) pada tabel `settings`.
   - Kolom `hosts.metrics_enabled` dengan semantik tri-state:
     - `-1`: Mewarisi konfigurasi global (*Inherit Global*).
     - `1`: Paksa aktif (*Force ON*).
     - `0`: Paksa nonaktif (*Force OFF*).
   - Memungkinkan administrator menghemat I/O pada host non-kritis atau mengaktifkan observabilitas mendalam hanya pada host produksi tertentu.

3. **Arsitektur Application Metric Probe & Kontrak Key-Value**:
   - Dibuat tabel `host_metric_probes` (`id`, `host_id`, `name`, `command`, `enabled`, `created_at`) untuk menyimpan perintah probe khusus per-host.
   - Seluruh probe aktif digabungkan dan dieksekusi secara efisien dalam satu sesi SSH inspeksi berkala (`sshrunner.Runner`), sehingga tidak menimbulkan beban koneksi jabat tangan SSH baru.
   - Perlindungan eksekusi terisolasi per-probe menggunakan pembungkus subshell dengan batasan waktu ketat (`timeout -k 1s 3s bash -c ... 2>/dev/null || true`), mencegah kegagalan satu probe atau hung network socket memblokir siklus inspeksi host.
   - Kontrak ekstraksi data berbasis baris teks standar (*plain-text key-value* `key: value` atau `key=value`). Nilai numerik pertama diekstrak secara otomatis, sementara baris komentar (`#`) atau output kosong diabaikan dengan aman.
   - Menyediakan endpoint preset terintegrasi (`GET /api/metric-probes/presets`) untuk aplikasi populer:
     - **Nginx Stub Status**: Ekstraksi multi-path otomatis (`stub_status`, `nginx_status`, port 80/8080) untuk metrik `nginx_active`, `nginx_accepts`, `nginx_handled`, `nginx_requests`, `nginx_reading`, `nginx_writing`, dan `nginx_waiting`.
     - **MySQL Status**: Ekstraksi via `mysqladmin status` untuk metrik `mysql_uptime`, `mysql_threads_connected`, `mysql_questions`, `mysql_slow_queries`, `mysql_opens`, dan `mysql_qps`.
     - **PHP-FPM Status**: Ekstraksi otomatis via HTTP status JSON atau fallback native `ps` untuk `active_processes`, `idle_processes`, dan `total_processes`.
     - **Redis Info**: Ekstraksi via `redis-cli info` dengan auto-discovery password dari `/etc/redis/redis.conf` dan unix socket untuk `redis_connected_clients`, `redis_used_memory_bytes`, `redis_ops_per_sec`, `redis_rejected_connections`, `redis_expired_keys`, dan `redis_uptime`.
     - **PostgreSQL Stats**: Ekstraksi via query `pg_stat_activity` dengan dukungan autentikasi peer `sudo -u postgres` untuk `pg_active_connections`, `pg_idle_connections`, dan `pg_total_connections`.

4. **API Kueri Time-Series Columnar dengan Dynamic SQL Downsampling**:
   - Endpoint `GET /api/hosts/{id}/metrics?range=24h&category=all|hardware|applications`.
   - Mengembalikan data dalam format columnar array (`timestamps: [t1, t2, ...]`, `series: {metric1: [v1, v2, ...], ...}`) yang dapat langsung dikonsumsi oleh pustaka grafik tanpa transformasi matriks berat di sisi browser.
   - Downsampling dinamis dieksekusi di level SQL engine menggunakan ekspresi matematis pembagian bucket waktu:
     - Rentang `1h` dan `6h`: Resolusi penuh (`step=0`, data mentah tanpa agregasi).
     - Rentang `24h`: Dikelompokkan ke dalam bucket 5 menit (`step=300s`, rata-rata `AVG(value)`).
     - Rentang `7d`: Dikelompokkan ke dalam bucket 30 menit (`step=1800s`, rata-rata `AVG(value)`).
   - Strategi ini memangkas transfer payload jaringan di bawah 15 KB dan menjaga latensi kueri database tetap di bawah 25 ms bahkan setelah terisi ribuan data sampel.

5. **Visualisasi Antarmuka Dual-Tier (uPlot & Pure SVG Helper)**:
   - **Tingkat Modal Detail (Host Detail Modal `[📊 Metrics & Telemetry]`)**:
     - Menggunakan **uPlot** (`~48 KB` minified, zero dependencies, Canvas 2D) yang di-vendor secara lokal di `internal/web/static/vendor/` (`uPlot.iife.min.js` dan `uPlot.min.css`) demi kepatuhan total lingkungan *airgapped* (Zero-CDN).
     - Sinkronisasi kursor crosshair (`uPlot.sync()`) di semua grafik (CPU, RAM, Disk I/O, Network I/O, Internet Latency, dan Metrik Aplikasi) sehingga inspeksi korelasi temporal antar sumber daya dapat dilakukan secara presisi.
     - Kontrol rentang waktu interaktif (tombol pill `1h`, `6h`, `24h`, `7d`), tombol filter kategori (`Semua`, `Hardware`, `Aplikasi`), tombol auto-refresh, dan pintasan kelola probe.
   - **Tingkat Kartu Dashboard (Host Card Mini-Sparklines)**:
     - Menggunakan pembantu SVG murni (~15 baris vanilla JS tanpa pustaka eksternal) untuk merender mini-sparkline tren 1 jam CPU dan RAM langsung di kartu host pada tampilan Grid dan List.
     - Memberikan indikasi visual cepat terhadap fluktuasi beban tanpa mengorbankan performa render DOM utama.

6. **Pengelolaan Probe & Status UI Terpadu**:
   - Modal manajemen probe (`Metric Probes`) dengan antarmuka penambahan, penyuntingan, pengujian (*test probe*), dan penghapusan probe kustom.
   - Modal pengaturan global dan form edit host yang menyediakan kontrol toggle keaktifan metrik dan konfigurasi hari retensi secara transparan.

## Konsekuensi

- **Observabilitas Lengkap Tanpa Beban Tambahan**: Administrator mendapatkan analisis tren historis dan performa aplikasi tanpa perlu menginstal atau memelihara TSDB eksternal dan agen terpisah.
- **Konsumsi Sumber Daya Minimal**: Penggabungan eksekusi probe ke sesi SSH yang sudah ada menjaga overhead CPU remote mendekati nol.
- **Ukuran Basis Data Terkendali**: Pembersihan otomatis berkala (auto-prune) memastikan ukuran berkas SQLite tetap stabil dan tidak mengalami pertumbuhan tak terbatas.
- **Kepatuhan Airgapped & Kecepatan Maksimal**: Bundling lokal uPlot dan helper SVG murni menjamin dashboard tetap bekerja mulus 100% offline dengan waktu muat instan.
