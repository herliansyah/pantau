# 01 — Matriks Metrik & Logika Kalkulasi Skor Head-to-Head

Type: grilling
Status: resolved
Blocked by:

## Question

Bagaimana spesifikasi teknis pemetaan metrik di 4 Comparison Category (Compute, Storage & I/O, Network Health, System Hygiene & Lifecycle), aturan evaluasi (apakah nilai lebih tinggi lebih baik seperti RAM/Cores, atau lebih rendah lebih baik seperti Latency/Load Avg/Drift), serta rumus penentuan Best-in-Class point dan penanganan nilai seri (tie) atau data tidak tersedia (N/A) pada fungsi komputasi client-side JavaScript?

## Answer

### 1. Pemetaan 4 Comparison Category & Polaritas Metrik

Perhitungan dilakukan murni di *client-side* JavaScript menggunakan data objek `Host` yang sudah ada di `state.hosts`:

| Kategori | Nama Metrik | Field Sumber | Polaritas | Keterangan Evaluasi |
| :--- | :--- | :--- | :--- | :--- |
| **Compute** | CPU Cores | `h.cpu_cores` | **HIB** (Higher) | Kapasitas inti pemrosesan paralel |
| | CPU Load / Core | `parseLoad1(h.cpu_load) / h.cpu_cores` | **LIB** (Lower) | Beban antrian dinamis dinormalisasi |
| | Total RAM | `h.ram_total_bytes` | **HIB** (Higher) | Kapasitas memori fisik terpasang |
| | Free RAM % | `(h.ram_total_bytes - h.ram_used_bytes) / h.ram_total_bytes * 100` | **HIB** (Higher) | Ruang napas memori bebas |
| **Storage & I/O** | Total Disk | `h.disk_total_bytes` | **HIB** (Higher) | Kapasitas volume penyimpanan root |
| | Free Disk % | `(h.disk_total_bytes - h.disk_used_bytes) / h.disk_total_bytes * 100` | **HIB** (Higher) | Sisa ruang penyimpanan bebas |
| | Disk I/O Rate | `h.disk_read_speed_bps + h.disk_write_speed_bps` | **LIB** (Lower) | Beban throughput I/O disk saat ini |
| **Network Health** | Egress Latency | `h.internet_latency_ms` | **LIB** (Lower) | Kecepatan latensi respon internet global |
| | Auth Safety | `h.failed_logins_count` | **LIB** (Lower) | Jumlah percobaan login gagal (0 = optimal) |
| | Egress Online | `h.internet_online` | **HIB** (Boolean) | Status keterhubungan internet publik |
| **System Hygiene** | Lifecycle Score | `h.lifecycle_score` | **HIB** (Higher) | Skor kelayakan komparatif (0–100) |
| | System Status | `h.status === 'healthy'` | **HIB** (Boolean) | Integritas kondisi operasional Desired State |
| | Freshness | `Date.now() - new Date(h.last_inspected)` | **LIB** (Lower) | Waktu jeda sejak inspeksi terakhir (ms) |

### 2. Aturan Toleransi Perbedaan (5% Significance Deadband)
Untuk menghindari false victory akibat fluktuasi telemetri sesaat:
- Nilai dua host dianggap berbeda secara signifikan jika:
  $$\frac{|\text{Val}_A - \text{Val}_B|}{\max(|\text{Val}_A|, |\text{Val}_B|)} \ge 0.05$$
- Jika selisih `< 5%`, metrik tersebut dinyatakan **Imbang (Tie)** dan tidak ada host yang memperoleh poin bonus (0 poin).
- Host terbaik yang melampaui toleransi 5% memperoleh **1 Poin Best-in-Class**.

### 3. Penentuan Pemenang Kategori & Nilai Seri
- **Category Winner**: Host dengan akumulasi poin terbanyak pada kategori tersebut memperoleh badge kategori. Jika terdapat perolehan poin yang sama antar 2 host atau lebih, kategori tersebut dinyatakan **Shared (Seri)** tanpa memaksakan peringkat semu.
- **Overall Comparison Winner**: Host dengan akumulasi total poin terbanyak dari seluruh kategori. Jika seri, kedua host berstatus co-winner.

### 4. Penanganan Host Offline / Stale / Data Parsial
- Host dengan status `down` atau `stale` tetap ditampilkan dengan menyajikan spesifikasi statis yang tercatat (Cores, Total RAM, Total Disk).
- Kolom host tersebut diberi label peringatan visual kuning/merah (*"Unreachable / Stale"*).
- Seluruh metrik telemetri dinamis bernilai `N/A`, menghasilkan 0 poin, dan poin metrik otomatis dimenangkan oleh host aktif yang valid.
