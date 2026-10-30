# 04 — Spesifikasi API Query Time-Series & Strategi Downsampling

Type: grilling
Status: resolved
Blocked by: 01, 02

## Question

Bagaimana kontrak endpoint internal Pantau (`/api/hosts/:id/metrics`) dalam menyajikan data time-series ke frontend (parameter filter rentang waktu `from`/`to`, pemilihan metrik, format JSON respons, serta strategi dynamic bucketing/downsampling di level SQL saat rentang 7 hari diminta agar respons tidak mengirim jutaan titik ke browser)?

## Answer

Disepakati spesifikasi kontrak API dan strategi downsampling sebagai berikut:

1. **Format Respon JSON Columnar**:
   - Struktur payload sejajar:
     ```json
     {
       "timestamps": [1730000000, 1730000060, 1730000120],
       "series": {
         "cpu": [15.2, 18.0, 14.5],
         "ram": [62.1, 62.1, 62.4],
         "disk_read_bps": [10240, 0, 4096],
         "disk_write_bps": [40960, 81920, 20480],
         "net_rx_bps": [12000, 15000, 11000],
         "net_tx_bps": [8000, 9500, 7800],
         "nginx_active": [12, 14, 15]
       }
     }
     ```
   - Menghemat ukuran transfer jaringan 60-70% dan langsung kompatibel dengan format array uPlot tanpa looping transformasi di browser.

2. **Strategi Dynamic Bucketing di Level SQL**:
   - Menghitung lebar interval (*step*) otomatis berdasarkan rentang waktu:
     - `1h` dan `6h`: Raw data tanpa downsampling (`step = 0`).
     - `24h`: Rata-rata per 5 menit (`step = 300 detik`), ~288 data points.
     - `7d`: Rata-rata per 30 menit (`step = 1800 detik`), ~336 data points.
   - Query SQL:
     ```sql
     SELECT (recorded_at / :step) * :step AS bucket_ts, AVG(value)
     FROM metric_samples 
     WHERE host_id = ? AND metric = ? AND recorded_at >= ?
     GROUP BY bucket_ts ORDER BY bucket_ts ASC;
     ```
   - Menjamin jumlah data point per series selalu stabil di bawah 400 titik dengan waktu query sub-milidetik.

3. **Parameter Query Endpoint**:
   - `GET /api/hosts/:id/metrics`
   - `range`: `1h`, `6h`, `24h`, `7d` (default `24h`).
   - `from` & `to`: Unix epoch timestamp opsional untuk custom zoom.
   - `category`: `all`, `hardware`, `applications` (default `all`).
