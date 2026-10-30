# 02 — Arsitektur Application Metric Probe & Kontrak Ekstraksi Preset/Kustom

Type: grilling
Status: resolved
Blocked by:

## Question

Bagaimana arsitektur eksekusi Application Metric Probe melalui batch SSH Pantau untuk mendukung preset built-in (Nginx status, MySQL mysqladmin ping/status, PHP-FPM pm.status) serta probe kustom pengguna (perintah shell bebas), termasuk format parsing output key-value numerik dan penanganan graceful jika service target mati?

## Answer

Disepakati arsitektur dan kontrak Application Metric Probe sebagai berikut:

1. **Format Output Ekstraksi (Key-Value Plain Text)**:
   - Script remote menghasilkan output satu baris per metrik dalam format `key: value` atau `key value` (misal: `nginx_active: 14`, `mysql_threads_connected: 5`, `phpfpm_active_processes: 3`).
   - Parser Go Pantau membaca baris demi baris, mem-parse angka integer/float, dan mengabaikan baris non-angka atau komentar tanpa error.

2. **Penyimpanan Definisi Probe (Tabel `host_metric_probes`)**:
   ```sql
   CREATE TABLE IF NOT EXISTS host_metric_probes (
       id INTEGER PRIMARY KEY AUTOINCREMENT,
       host_id INTEGER NOT NULL REFERENCES hosts(id) ON DELETE CASCADE,
       name TEXT NOT NULL,
       command TEXT NOT NULL,
       enabled INTEGER DEFAULT 1,
       created_at DATETIME DEFAULT CURRENT_TIMESTAMP
   );
   CREATE INDEX IF NOT EXISTS idx_host_metric_probes_host ON host_metric_probes(host_id, enabled);
   ```
   - UI menyediakan modal/dropdown *"Add from Preset"* (Nginx Stub Status, MySQL Admin Status, PHP-FPM Status, Redis Info, PostgreSQL Stats) yang mengisi template perintah shell yang dapat diedit langsung oleh pengguna.

3. **Mekanisme Eksekusi SSH (Single Batch Script Concatenation)**:
   - Seluruh probe aktif untuk host terkait digabungkan ke dalam satu sesi SSH bersama dengan delimitasi:
     `echo "---PROBE:<name>---"; (timeout -k 1s 3s bash -c '<command>' 2>/dev/null || true)`
   - Menghindari pembukaan multi-koneksi SSH (zero extra connection overhead) dan latensi minimal.

4. **Penanganan Graceful Degradation**:
   - Jika service remote mati atau perintah menghasilkan error / timeout, probe diabaikan secara silent (tanpa menggagalkan inspeksi metrik hardware inti Pantau).
