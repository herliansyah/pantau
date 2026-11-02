# 02 — Pekerja Agregasi Daily Uptime Rollup & Status Resolution Engine

Type: grilling
Status: resolved
Blocked by: 01

## Question

Kapan dan bagaimana kalkulasi agregasi harian (Daily Uptime Rollup) dieksekusi oleh Pantau? Bagaimana formula evaluasi status target menentukan klasifikasi status (`Operational`, `Degraded`, `Partial Outage`, `Major Outage`) berdasarkan hasil pembacaan inspeksi host / desired rules, bagaimana menangani window toleransi downtime singkat (misal transient packet loss 1 siklus), serta bagaimana komputasi persentase uptime 90 hari dihitung tanpa akumulasi drift pembulatan?

## Answer

### 1. Arsitektur Eksekusi Dual-Trigger (Real-Time Incremental + Prune Harian)

Agregasi tidak ditunda sampai tengah malam, melainkan diperbarui secara real-time setiap kali siklus inspeksi selesai:

1. **Incremental UPSERT Pasca-Inspeksi**:
   - Di akhir fungsi `ins.InspectHost(hostID)` pada `internal/inspector/inspector.go`:
   - Inspector mencari seluruh Status Target aktif yang terhubung dengan `hostID` (baik target tipe `host` maupun target tipe `desired_rule` pada host tersebut).
   - Menjalankan kueri UPSERT atomik SQLite ke tabel `status_page_daily_rollups` untuk tanggal UTC hari ini (`YYYY-MM-DD`):
     ```sql
     INSERT INTO status_page_daily_rollups (
         target_id, date, total_checks, successful_checks, degraded_checks, outage_checks, uptime_percentage, updated_at
     ) VALUES (
         ?, ?, 1, ?, ?, ?, ?, CURRENT_TIMESTAMP
     ) ON CONFLICT(target_id, date) DO UPDATE SET
         total_checks = total_checks + 1,
         successful_checks = successful_checks + excluded.successful_checks,
         degraded_checks = degraded_checks + excluded.degraded_checks,
         outage_checks = outage_checks + excluded.outage_checks,
         uptime_percentage = ROUND(((successful_checks + excluded.successful_checks + 0.5 * (degraded_checks + excluded.degraded_checks)) * 100.0) / (total_checks + 1), 2),
         updated_at = CURRENT_TIMESTAMP;
     ```
   - Memanggil `server.InvalidateStatusPageCache()` agar cache RAM snapshot diperbarui secara debounced.

2. **Pekerja Pembersihan Berkala (> 90 Hari)**:
   - Worker background periodik yang berjalan setiap 1 jam (bersamaan dengan `startMetricsPruneWorker` di `main.go`) mengeksekusi pembersihan baris usang:
     ```sql
     DELETE FROM status_page_daily_rollups WHERE date < date('now', '-90 days');
     ```
   - Menjaga ukuran tabel tetap sangat kecil (~900 baris untuk 10 target, memakan < 50 KB disk).

---

### 2. Klasifikasi Status Target (Host vs Desired Rule)

1. **Target Tipe `host`**:
   - `hosts.status == "healthy"` $\rightarrow$ **`operational`**
   - `hosts.status == "degraded"` $\rightarrow$ **`degraded`** (terjadi drift non-kritis atau peringatan utilisasi)
   - `hosts.status == "down"` $\rightarrow$ **`outage`** (koneksi SSH gagal / server unreachable)
   - `hosts.status == "unknown"` atau stale $\rightarrow$ **`degraded`**

2. **Target Tipe `desired_rule`**:
   - Kondisi aktual sesuai ekspektasi $\rightarrow$ **`operational`**
   - Host target `down` $\rightarrow$ otomatis **`outage`**
   - Host `healthy` namun rule mengalami Drift:
     - Kind kritis (`service`, `port`, `container`, `process`) $\rightarrow$ **`outage`**
     - Kind non-kritis (`disk`, `backup`, `cron`) $\rightarrow$ **`degraded`**

---

### 3. Mitigasi Transient Glitch (Anti-Flapping & Consecutive Failures)

- Untuk mencegah status berkedip (*flapping*) akibat packet drop sesaat (1 kegagalan inspeksi transien):
  - Target hanya ditandai berstatus publik **`outage`** jika mengalami **minimal 2 kali kegagalan berturut-turut** (*2 consecutive failures*).
  - Pada kegagalan pertama (1x fail), status publik sementara diklasifikasikan sebagai `degraded` ("Verifying Connectivity") untuk menghindari false alarm ke publik.
  - Segera setelah 1 kali inspeksi sukses kembali diperoleh, status target langsung dipulihkan ke `operational`.

---

### 4. Perhitungan Akumulatif Uptime 90 Hari (Anti-Drift Pembulatan)

- **Eliminasi Kesalahan `AVG(daily_uptime)`**:
  - Merata-ratakan persentase harian secara naif menghasilkan drift matematis karena jumlah pemeriksaan per hari bisa berbeda jika frekuensi inspeksi diubah oleh admin.
- **Formula Sum of Checks**:
  Persentase uptime 90 hari dihitung dari total akumulasi seluruh pemeriksaan dalam jendela 90 hari:
  ```sql
  SELECT 
      COALESCE(SUM(total_checks), 0) AS total,
      COALESCE(SUM(successful_checks), 0) AS successful,
      COALESCE(SUM(degraded_checks), 0) AS degraded
  FROM status_page_daily_rollups
  WHERE target_id = ? AND date >= date('now', '-90 days');
  ```
  $$\text{Uptime}_{90d} = \frac{\text{successful} + (0.5 \times \text{degraded})}{\text{total}} \times 100$$
  - Dibulatkan ke 2 desimal (`math.Round(val * 100) / 100`).
  - Jika `total == 0` (target baru terdaftar hari ini), nilai uptime default adalah `100.0%`.
  - Bobot `0.5` untuk status `degraded` mengikuti standar industri (ketersediaan fungsional sebagian).

---

### 5. Status Indikator Global Sistem

Status global pada header halaman publik ditentukan dengan kaskade hierarkis:
1. **`major_outage`**: Ada minimal 1 target publik berstatus `outage`, ATAU ada insiden aktif dengan impact `major`/`critical`.
2. **`degraded`**: Tidak ada outage, tetapi ada target berstatus `degraded`, ATAU ada insiden aktif dengan impact `minor`.
3. **`maintenance`**: Ada insiden aktif bertipe pemeliharaan terjadwal (impact `none`).
4. **`operational`**: Seluruh target publik `operational` dan tidak ada insiden aktif.

