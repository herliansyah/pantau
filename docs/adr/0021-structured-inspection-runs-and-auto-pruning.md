# 0021. Structured Inspection Runs, Zero-Overhead Telemetry, dan Rolling Auto-Pruning

Pencatatan riwayat eksekusi inspeksi berbasis data terstruktur pada SQLite internal dengan evaluasi overhead minim (durasi roundtrip dan beban CPU), eliminasi log mentah, dan pembersihan otomatis tanpa konfigurasi.

## Konteks dan Masalah

Pantau menjalankan inspeksi periodik berbasis SSH agentless untuk memvalidasi Desired State dan mengumpulkan metrik Host. Meskipun status terkini dan insiden Drift tercatat di basis data, sistem belum memiliki catatan audit dan riwayat eksekusi inspeksi (*Inspection Run*).

Kondisi ini menimbulkan beberapa ketidakpastian bagi administrator:
1. Tidak ada visibilitas riwayat apakah inspeksi terjadwal berjalan tepat waktu atau tertunda.
2. Tidak ada pembuktian transparan mengenai berapa lama waktu eksekusi inspeksi dan apakah proses SSH tersebut membebani (*overhead*) Host target.
3. Saat inspeksi gagal atau jaringan SSH bermasalah di luar konteks aturan Desired State (misal: SSH timeout, auth key ditolak), ketiadaan riwayat menyulitkan proses diagnosis.

Namun, menyimpan *raw console stdout/stderr* untuk setiap siklus inspeksi akan memicu *disk bloat* pada SQLite dan menghasilkan data sampah yang jarang dibaca.

## Keputusan Arsitektur

1. **Structured Inspection Run menggantikan Raw Log Stream**:
   - Menolak penyimpanan streaming teks mentah (*raw log dump*).
   - Memperkenalkan entitas terstruktur `inspection_runs` di SQLite internal:
     - `host_id`: Relasi ke Host target.
     - `started_at` & `duration_ms`: Timestamp dan durasi roundtrip inspeksi (dalam milidetik).
     - `status`: Hasil akhir eksekusi (`ok`, `drift`, `degraded`, `down`, `error`).
     - `summary`: Ringkasan manusiawi (contoh: "5 aturan terpenuhi, 0 drift").
     - `details`: Potongan pesan diagnostik (*Root Cause Excerpt*) hanya jika terjadi kegagalan atau Drift; string kosong jika kondisi normal.

2. **Pengukuran Overhead Transparan Tanpa Beban Tambahan**:
   - Overhead inspeksi diukur langsung dari total durasi eksekusi SSH (`duration_ms`) dipadukan dengan snapshot beban CPU Host (`cpu_load`).
   - Tidak menambahkan proses profiler/daemon monitoring terpisah di target agar tetap mempertahankan prinsip agentless zero-footprint.

3. **Penyimpanan Lokal Tanpa Enkripsi Runtime**:
   - Riwayat disimpan langsung pada tabel SQLite internal `inspection_runs`.
   - Tidak menerapkan enkripsi kolom/tabel runtime lokal guna menghindari beban CPU yang sia-sia, mengandalkan isolasi permission file sistem operasi (`0600`/`0700`).
   - Enkripsi at-rest penuh (AES-256-GCM) tetap terintegrasi secara otomatis saat basis data diekspor melalui mekanisme *System Snapshot*.

4. **Rolling Cap Auto-Pruning Zero-Config**:
   - Menolak formulir pengaturan retensi yang rumit di UI (YAGNI).
   - Menerapkan pembersihan otomatis sebaris (*inline prune*) pasca eksekusi inspeksi: mempertahankan maksimal 100 riwayat terbaru untuk setiap Host (`DELETE FROM inspection_runs WHERE host_id = ? AND id NOT IN (...)`).
   - Memberikan jaminan ukuran basis data terprediksi dan performa query tetap instan tanpa memerlukan background scheduler harian.

5. **Penyajian UI Terintegrasi pada Workspace Modal dan Kartu Host**:
   - Menambahkan tab "Riwayat Inspeksi" (*Inspection Runs*) pada Workspace Modal Host Detail berupa tabel kompak interaktif (Status Badge, Waktu, Durasi ms, Ringkasan) dengan accordion untuk rincian diagnostik jika terdapat error.
   - Menampilkan metrik durasi eksekusi terakhir (contoh: `320ms`) secara ringkas di kartu Host dashboard.
