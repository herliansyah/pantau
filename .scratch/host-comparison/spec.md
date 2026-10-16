# Spec: Host Comparison

## Ringkasan Eksekutif

Fitur **Host Comparison** menyediakan antarmuka evaluasi komparatif multi-host (2 hingga 4 Host) secara berdampingan (*side-by-side*) di dalam **Workspace Modal** (75vw × 80vh) Pantau. Fitur ini dirancang sebagai instrumen pendukung pengambilan keputusan administrator (misal: migrasi beban kerja, *hardware refresh*, audit kapasitas, atau komparasi performa server) berbasis data telemetri inspeksi riil tanpa memerlukan endpoint backend baru (pure client-side computing dari `state.hosts`).

## Ruang Lingkup & Kebutuhan Inti

1. **Seleksi Host (2–4 Host)**:
   - Tombol peluncur global di Top Navigation Header (ikon ⚖️ `Compare`).
   - Modal seleksi interaktif yang memungkinkan pemilihan minimal 2 dan maksimal 4 Host untuk dibandingkan.
   - Penanganan Host yang berstatus *Down*, *Stale*, atau memiliki data parsial dengan penanda peringatan (*soft penalty* & badge N/A).

2. **Empat Dimensi Evaluasi (Comparison Categories)**:
   - **Compute**: Kuantitas core CPU, utilisasi Load Average, sisa memori fisik (Free RAM %), sisa memori virtual (Free Swap %).
   - **Storage & I/O**: Sisa kapasitas penyimpanan root (Free Disk % dan Free Disk GB), laju throughput baca/tulis disk (`Disk I/O Rate`).
   - **Network Health**: Status keterhubungan keluar (*Network Egress*), latensi roundtrip (ms), serta rasio isolasi port aman (*Port Exposure*).
   - **System Hygiene & Lifecycle**: Jumlah deviasi aturan (*Drift*), skor kelayakan siklus hidup (*Lifecycle Score* 0–100), masa operasional perangkat keras (*Productive Lifespan*), dan status EOL sistem operasi.

3. **Sistem Skoring Head-to-Head & Pemenang (Comparison Winner)**:
   - Poin berbasis *Best-in-Class* per baris metrik (1 poin untuk host paling unggul per metrik terukur).
   - Penentuan pemenang untuk setiap *Comparison Category* berdasarkan akumulasi poin di kategori tersebut.
   - Penentuan *Overall Comparison Winner* beserta skor total.

4. **Visualisasi & Output Pengambilan Keputusan**:
   - Kolom berdampingan (*side-by-side columns*) yang responsif.
   - Indikator visual pemenang (badge bintang/piala emas dan highlight sel).
   - Generator narasi kesimpulan otomatis ("Host X unggul di kategori Y karena...").
   - Tombol salin rangkuman komparasi dalam format Markdown ke *clipboard* peramban.
