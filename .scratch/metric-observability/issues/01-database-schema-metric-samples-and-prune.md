# 01 — Desain Skema Database Metric Samples & Konfigurasi Auto-Prune

Type: grilling
Status: resolved
Blocked by:

## Question

Bagaimana spesifikasi skema tabel SQLite `metric_samples` (pilihan kolom, tipe data, composite index untuk filtering per host dan rentang waktu) dan mekanisme background worker auto-prune berbasis konfigurasi `metric_retention_days` (default 7 hari) agar ukuran database terkendali dan performa write/read tetap optimal?

## Answer

Disepakati desain skema dan mekanisme retensi sebagai berikut:

1. **Skema Tabel `metric_samples` (Flat Key-Value)**:
   ```sql
   CREATE TABLE IF NOT EXISTS metric_samples (
       id INTEGER PRIMARY KEY AUTOINCREMENT,
       host_id INTEGER NOT NULL REFERENCES hosts(id) ON DELETE CASCADE,
       metric TEXT NOT NULL,
       value REAL NOT NULL,
       recorded_at INTEGER NOT NULL
   );
   CREATE INDEX IF NOT EXISTS idx_metric_samples_lookup ON metric_samples(host_id, metric, recorded_at);
   CREATE INDEX IF NOT EXISTS idx_metric_samples_prune ON metric_samples(recorded_at);
   ```
   - **Rasional**: Struktur flat mendukung metrik HW (CPU, RAM, Disk, Net) dan metrik aplikasi (Nginx, MySQL, PHP-FPM, custom) tanpa modifikasi skema DDL berulang. Timestamp epoch integer (`recorded_at`) memaksimalkan kecepatan query range dan pruning.

2. **Skema Toggle On/Off Global & Per-Host**:
   - **Global**: Disimpan di tabel `settings`:
     - `metrics_enabled`: `"true"` (default: aktif).
     - `metric_retention_days`: `"7"` (default: 7 hari).
   - **Per-Host**: Kolom `metrics_enabled` di tabel `hosts` dengan format tri-state:
     - `-1`: *Inherit Global* (mengikuti setting global, default).
     - `1`: *Force ON* (selalu aktif untuk host ini).
     - `0`: *Force OFF* (selalu nonaktif untuk host ini).

3. **Mekanisme Auto-Prune**:
   - Background worker berjalan periodik setiap 1 jam (`time.NewTicker(1 * time.Hour)`).
   - Menghitung batas retensi: `cutoff = unix_now - (retention_days * 86400)`.
   - Menjalankan `DELETE FROM metric_samples WHERE recorded_at < ?` memanfaatkan index `idx_metric_samples_prune`.
