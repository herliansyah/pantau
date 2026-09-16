# Embedded In-App Documentation Modal dan Bilingual Airgapped Guides

Penyematan dokumentasi resmi Pantau (README dan User Guide dwibahasa ID/EN) ke dalam single binary via `go:embed`, penyajian dokumen melalui endpoint publik `/api/docs`, dan penampil antarmuka berbasis Workspace Modal dengan client-side micro-parser tanpa dependensi eksternal.

## Konteks dan Masalah

Pantau merupakan sistem pemantauan dan manajemen server mandiri (*self-hosted*) yang beroperasi di lingkungan terisolasi (*airgapped*) atau intranet privat ([ADR 0002](0002-golang-single-binary-sqlite.md) dan [ADR 0013](0013-airgapped-self-contained-web-assets.md)). Repositori Pantau telah dilengkapi dengan dokumentasi menyeluruh (`README.md`, `README.id.md`, `docs/user-guide.md`, dan `docs/user-guide.id.md`).

Namun sebelumnya, dokumentasi ini hanya dapat diakses melalui repositori Git eksternal atau pembaca teks manual pada server master. Hal ini menimbulkan hambatan operasional:
1. **Kendala Airgapped & Bootstrap**: Operator yang menjalankan Pantau di jaringan tanpa akses internet tidak dapat membuka repositori online untuk membaca panduan setup, restore, atau konfigurasi Desired State.
2. **Ketiadaan Panduan Pra-Login**: Operator baru yang belum memiliki sesi login tidak memiliki akses ke panduan in-app jika mengalami kendala pada alur *Initial Setup* atau *Snapshot Restore*.
3. **Risiko Kembung Dependensi (Dependency Bloat)**: Menambahkan library markdown parser besar (baik di backend maupun bundler npm di frontend) melanggar prinsip desain *lazy senior dev* dan aturan *zero-npm* pada aset Pantau.

## Keputusan Arsitektur

1. **Embedded Single-Source-of-Truth (`go:embed`)**:
   - File markdown otoritatif di root repositori dan direktori `docs/` disematkan langsung ke dalam executable binary Pantau saat kompilasi (`main.go`).
   - Mencegah desinkronisasi atau duplikasi konten manual antara repositori kode dan dokumentasi aplikasi.

2. **Batas Autentikasi Publik & Privat (Dual Entry Point)**:
   - Endpoint backend `GET /api/docs?name={readme|guide}&lang={en|id}` dirancang sebagai rute publik (tanpa token sesi) sehingga dapat diakses sebelum maupun setelah login.
   - Titik akses antarmuka disediakan di dua tempat: tautan bantuan di layar Login/Restore dan tombol `(?)` di Header utama dashboard Pantau.

3. **Workspace Modal Container & Dynamic TOC**:
   - Menampilkan dokumentasi menggunakan standar *Workspace Modal* (75vw × 80vh) sesuai [ADR 0007](0007-standardized-workspace-modal-layout.md) agar konsisten dengan observabilitas Pantau tanpa memutus koneksi websocket Terminal Dock.
   - Bilah sisi kiri menyajikan pemilih dokumen (*README* vs *User Guide*) serta daftar isi dinamis (*Table of Contents*) berbasis heading (`h2`, `h3`) yang mendukung *smooth-scroll* langsung ke bagian dokumen.

4. **Zero-Dependency Vanilla JS Micro-Renderer**:
   - Menggunakan micro-parser Markdown ringkas (< 80 baris vanilla JS) di peramban untuk mengonversi heading, blok kode, inline code, kutipan, list, dan penataan teks standar.
   - Dilengkapi sanitasi teks (HTML entity escaping) untuk memitigasi risiko *Cross-Site Scripting* (XSS).
   - Menggunakan tema warna Nord dan CSS bawaan Pantau tanpa menambah pustaka vendor baru ke dalam `static/vendor/`.

5. **Client-Side In-Memory Cache**:
   - Dokumen yang telah diunduh oleh peramban disimpan dalam variabel cache memori per sesi (`docsCache`), mengeliminasi request jaringan berulang saat pengguna berpindah tab dokumen atau mengganti bahasa.
