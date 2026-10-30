# 05 — Desain Tab Observabilitas Metrik & Interaktivitas UI

Type: prototype
Status: resolved
Blocked by: 03, 04

## Question

Bagaimana tata letak dan interaksi tab observabilitas baru di Host Detail Modal (pemilihan rentang waktu 1h/6h/24h/7d, toggle visibilitas series HW vs Aplikasi, tooltip hover live data point, dan integrasi mini-sparkline di Host Card)?

## Answer

Disepakati desain antarmuka dan pengalaman pengguna (UI/UX) sebagai berikut:

1. **Tab `Metrics & Telemetry` di Host Detail Modal**:
   - Ditambahkan tab baru `[📊 Metrics & Telemetry]` di `modal-tabs-bar` berdampingan dengan Overview, Network, Desired State, dan File Manager.
   - **Toolbar Atas**:
     - Tombol pill rentang waktu: `1h`, `6h`, `24h` (default active), `7d`.
     - Filter kategori metrik: `[All]`, `[Hardware]`, `[Applications]`.
     - Tombol `⚙️ Manage Probes` untuk membuka modal konfigurasi preset/custom probe host.
   - **Multi-Chart Stack Panel (uPlot)**:
     - Panel 1: CPU & Memory Utilization (%) (Dual-series, 0-100% scale).
     - Panel 2: Disk I/O & Network Throughput (MB/s & KB/s).
     - Panel 3: Application Metrics (Nginx Active Conn, MySQL QPS/Threads, PHP-FPM Workers, dsb.).
     - **Crosshair Synchronization**: Menggunakan `uPlot.sync("pantau-host-metrics")` sehingga hover di panel CPU otomatis memindahkan garis penunjuk waktu dan tooltip di panel RAM, Disk, Net, dan Aplikasi secara bersamaan.

2. **Mini Sparkline di Host Card (Dashboard Utama)**:
   - Disematkan fungsi utilitas lightweight pure SVG polyline (~15 baris vanilla JS, zero dependency) untuk menggambar sparkline tren CPU 1 jam terakhir langsung di kartu host.

3. **Status Nonaktif (Disabled State)**:
   - Jika `metrics_enabled = 0` (atau dinonaktifkan secara global), tab menampilkan banner informatif ramah: *"Observabilitas metrik dinonaktifkan untuk host ini. [Aktifkan di Pengaturan Host]"* dengan tombol shortcut ke pengaturan.
