# 0023. Dual-Bucket Inspection Retention, Floating Alert Banner, dan Auto-Resolution

Penyimpanan jejak audit deviasi historis tanpa disk bloat melalui retensi dual-bucket di SQLite, eliminasi penumpukan baris alert dashboard melalui floating overlay non-blocking, serta resolusi otomatis status peringatan saat Host pulih ke kondisi seharusnya.

## Konteks dan Masalah

Sebelum keputusan ini, Pantau menghadapi dua tantangan operasional terkait penanganan Drift dan riwayat inspeksi:
1. **Penumpukan Peringatan Alert di Dashboard**:
   Ketika terjadi drift pada banyak Host, deretan `alert-bar` di-render secara bertumpuk ke bawah di bagian atas dashboard. Operator harus melakukan dismiss manual satu per satu tanpa opsi pembersihan massal (*Dismiss All*), ketiadaan timestamp presisi, serta tumpukan baris yang mendorong toolbar dan kartu Host jauh ke bawah (*layout shift*).
2. **Kehilangan Jejak Audit Akibat Rolling Auto-Pruning Tunggal (ADR 0021)**:
   Mekanisme auto-pruning pada ADR 0021 hanya mempertahankan 100 eksekusi terakhir tanpa membedakan status (`ok` vs `drift/error`). Pada interval inspeksi 30 detik, 100 eksekusi terlampaui hanya dalam 50 menit. Jika suatu Host sempat mengalami drift/error di pagi hari lalu normal kembali selama 1 jam berikutnya, seluruh bukti audit deviasi historis terhapus permanen dari basis data.

## Keputusan Arsitektur

1. **Floating Alert Banner dengan Non-Blocking Overlay**:
   - Menghilangkan tumpukan baris flat alert di dashboard.
   - Menggantikannya dengan **Alert Summary Bar** tunggal yang ringkas: `⚠️ {count} Peringatan Aktif ({hostCount} Host)`.
   - Rincian alert disajikan melalui panel mengambang (**Floating Overlay Dropdown**) dengan `position: absolute`, bayangan halus, batas tinggi (`max-height: 340px; overflow-y: auto`), dan penutupan otomatis saat klik di luar (*outside-click*). Tidak ada pergeseran tata letak (*zero layout shift*) pada kartu Host.
   - Setiap entri alert menampilkan nama host, pesan deviasi, waktu relatif dengan tooltip tanggal & jam presisi ISO, pintasan langsung `[Buka Host]`, cuplikan *Root Cause Excerpt*, dan tombol dismiss individual.

2. **Bulk Dismissal dan Siklus Hidup Alert Auto-Resolution**:
   - Menyediakan tombol `[Dismiss All]` di banner dan header untuk meng-acknowledge seluruh alert aktif secara massal melalui endpoint `POST /api/alerts/ack-all`.
   - **Auto-Resolution**: Ketika inspeksi berikutnya pada suatu Host selesai dan memverifikasi bahwa seluruh Desired State telah terpenuhi tanpa deviasi (`driftFound == false` dan status `healthy`), seluruh alert aktif milik Host tersebut otomatis di-acknowledge oleh sistem (`AcknowledgeHostAlerts`).

3. **Dual-Bucket Auto-Pruning pada SQLite (`inspection_runs`)**:
   - Memodifikasi query pembersihan otomatis sebaris (*inline prune*) pasca inspeksi menjadi 2 jalur kuota per Host:
     1. **Bucket Normal**: Mempertahankan maksimal 100 eksekusi `status = 'ok'` terbaru untuk observabilitas tren latensi roundtrip SSH terkini.
     2. **Bucket Masalah**: Mempertahankan maksimal 100 eksekusi deviasi `status != 'ok'` (`drift`, `down`, `error`, `degraded`) terbaru per Host.
   - Memberikan jaminan ketersediaan jejak audit deviasi historis berhari-hari sebelumnya selama belum melampaui kuota 100 error, dengan batas atas ukuran penyimpanan terprediksi (maksimal 200 baris per Host, ~60 KB/host, total < 2 MB untuk puluhan server).

4. **Segmented Audit Trail Sub-Tab pada Workspace Modal**:
   - Menambahkan filter segmen pada tab "Riwayat Inspeksi" Workspace Modal:
     - `[ 📋 Semua Riwayat ]`
     - `[ ⚠️ 100 Masalah Terakhir (Audit Trail) ]`
   - Endpoint `GET /api/hosts/{id}/runs` mendukung parameter `?filter=issues` dan `?filter=all` dengan indeks performa komposit `idx_inspection_runs_status (host_id, status, id DESC)`.
   - Pada filter masalah, cuplikan diagnostik (*Root Cause Excerpt*) dibuka otomatis (`open`) dan tanggal/jam ditampilkan secara presisi untuk efisiensi investigasi insiden.
