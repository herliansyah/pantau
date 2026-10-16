# 02 — Desain Tata Letak UI Workspace Modal Side-by-Side

Type: prototype
Status: resolved
Blocked by: 01

## Question

Bagaimana arsitektur antarmuka HTML/CSS dan struktur CSS Grid dalam kontainer Workspace Modal (75vw × 80vh) agar penyajian matriks 2, 3, hingga 4 Host tersusun berdampingan (side-by-side) secara ergonomis, dilengkapi pemilih host interaktif (host picker dropdown), penanda visual Comparison Winner (highlight baris terbaik & badge piala/bintang emas), serta penyesuaian adaptif tanpa merusak tata letak saat dibuka pada layar resolusi sedang?

## Answer

Berdasarkan validasi prototipe interaktif di [`.scratch/host-comparison/prototype.html`](../prototype.html), arsitektur antarmuka UI ditetapkan menggunakan **Variant A (Unified Comparative Matrix)** yang diperkaya dengan Executive Decision Summary:

### 1. Struktur Kontainer & Tata Letak CSS Grid
- Memanfaatkan styling bawaan Pantau `.modal-content.modal-workspace` (dimensi paten 75vw × 80vh) dengan scrolling vertikal internal pada `.modal-body`.
- Baris kartu ringkasan host teratas menggunakan CSS Grid dinamis:
  `grid-template-columns: 220px repeat(N, 1fr)` (dengan $N \in [2, 4]$ sesuai jumlah host yang dipilih).
- Kartu pemenang utama (*Overall Winner*) diberikan aksen border emas (`--gold: #fbbf24`), subtle glow shadow, dan badge `🏆 Overall Winner (X Poin)`.

### 2. Matriks Komparasi Baris & Indikator Pemenang
- Tabel matriks `.matrix-table` menggunakan kolom kiri tetap (lebar 220px) untuk nama metrik, dan $N$ kolom berikutnya untuk nilai telemetri masing-masing host.
- Setiap kategori dipisahkan oleh *header row* dengan warna latar aksen redup (`rgba(15, 23, 42, 0.7)`) yang mencantumkan nama kategori dan pemenang kategori tersebut.
- Sel pemenang metrik (*Best-in-Class*) diwarnai hijau transparan (`var(--success-bg)`) dengan ikon bintang ⭐, sementara nilai yang berada dalam batas toleransi 5% ditampilkan netral (*Draw*).
- Host berstatus `down` atau `stale` ditampilkan dengan teks merah redup (`cell-stale`) dan badge peringatan `⚠️ Unreachable / Stale`.

### 3. Header Interaktif & Host Picker
- Sub-bar di bawah judul modal memuat pil-pil host terpilih (`.host-pill`) dengan tombol silang (×) untuk menghapus host, serta dropdown seleksi untuk menambahkan host hingga batas maksimal 4 host.
- Tombol aksi header memuat "📋 Salin Ringkasan (MD)" untuk ekspor satu-klik langsung ke clipboard.
