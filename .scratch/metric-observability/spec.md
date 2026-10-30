# Spesifikasi: Metric Samples & Application Telemetry Observability

Status: resolved

## Deskripsi
Fitur pengamatan time-series telemetri berbasis SQLite internal Pantau dengan retensi terkonfigurasi (default 7 hari + auto-prune), visualisasi grafik tren temporal interaktif di dashboard menggunakan uPlot (HW resource dan metrik aplikasi via preset Nginx, MySQL, PHP-FPM maupun kustom probe), yang diinspeksi secara efisien melalui batch SSH agentless dengan kontrol keaktifan global dan per-host.

## Keputusan Arsitektur
Sesuai hasil wayfinding di [`.scratch/metric-observability/map.md`](map.md):
1. **Skema Database & Pruning**:
   - Tabel flat `metric_samples` (`id`, `host_id`, `metric`, `value`, `recorded_at`) dengan composite index `(host_id, metric, recorded_at)` dan index `recorded_at`.
   - Kontrol toggle tri-state di `hosts.metrics_enabled` (`-1` = Inherit, `1` = Force ON, `0` = Force OFF) dan global setting `metrics_enabled` & `metric_retention_days`.
   - Worker auto-prune periodik 1 jam menghapus data `recorded_at < cutoff`.
2. **Arsitektur Application Metric Probe**:
   - Tabel `host_metric_probes` (`id`, `host_id`, `name`, `command`, `enabled`) untuk konfigurasi probe kustom dan preset (Nginx, MySQL, PHP-FPM, Redis, PostgreSQL).
   - Ekstraksi via output plain text key-value (`key: value`).
   - Eksekusi disatukan ke dalam satu sesi SSH inspection dengan pembatas timeout isolasi per-probe dan graceful silent fallback saat target service down.
3. **Visualisasi Dual-Tier Frontend**:
   - **uPlot** (`~48 KB` minified, canvas 2D, zero dependencies) di-vendor lokal di `internal/web/static/vendor/` untuk tab observabilitas detail dengan crosshair sync `uPlot.sync()`.
   - **Pure SVG Helper** (~15 baris vanilla JS, 0 KB) untuk mini-sparkline 1 jam di kartu host.
4. **API Query Time-Series**:
   - `GET /api/hosts/:id/metrics?range=24h` mengembalikan format JSON columnar array langsung kompatibel dengan uPlot.
   - Dynamic SQL bucketing (`step=0` untuk 1h/6h, `300s` untuk 24h, `1800s` untuk 7d) memastikan respon < 25 ms dan ukuran payload transfer < 15 KB.
5. **UI Tab Observabilitas**:
   - Tab baru `[📊 Metrics & Telemetry]` di Host Detail Modal dengan toolbar rentang waktu, stack chart uPlot ter-sync, filter kategori metrik, tombol manage probes, serta friendly disabled state.
