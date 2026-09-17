# In-Memory Gzip Compression dan ETag Revalidation untuk Aset UI Web

Optimasi efisiensi transfer jaringan antarmuka web Pantau (`index.html`) melalui pre-kompresi Gzip di memori dan validasi cache ETag tanpa memperkenalkan build toolchain eksternal, bundler Node.js, maupun dependensi minifikasi pihak ketiga.

## Konteks dan Masalah

Antarmuka web Pantau dibangun sebagai aplikasi satu halaman (*Single-Page Application / SPA*) yang memuat CSS, struktur HTML, dan logika JavaScript vanilla dalam satu file terpadu: `internal/web/static/index.html` (~261 KB, ~5.850 baris). File ini di-embed langsung ke dalam binary Go melalui `embed.FS` sesuai prinsip arsitektur *zero-ops single binary* ([ADR 0002](0002-golang-single-binary-sqlite.md)) dan aset lokal mandiri tanpa CDN ([ADR 0013](0013-airgapped-self-contained-web-assets.md)).

Sebelumnya:
1. **Transfer Jaringan Mentah (Uncompressed Wire Payload)**: Endpoint root `/` menyajikan `index.html` mentah sebesar 261 KB pada setiap request awal tanpa kompresi HTTP.
2. **Ketiadaan Revalidasi Cache**: Header `Cache-Control: no-cache` dikirim tanpa menyertakan header `ETag` atau `Last-Modified`. Akibatnya, setiap kali pengguna melakukan refresh halaman atau membuka tab baru, peramban selalu mengunduh ulang 261 KB penuh meskipun isi kode tidak berubah sama sekali.
3. **Dilema Minifikasi vs Kompleksitas Tooling**:
   - Memasukkan perkakas frontend (Node.js, npm, `esbuild`, `terser`, atau HTML minifier) melanggar komitmen *Zero-Node* pada ADR 0013 dan menambah kompleksitas Dockerfile, CI, serta siklus rilis.
   - Melakukan minifikasi langsung pada source code repositori menghancurkan *Developer Experience* (DX) dan menyulitkan proses debugging di browser console.
   - Menambahkan library Go pihak ketiga (seperti `tdewolff/minify`) menambah beban dependensi `go.mod` dan alokasi CPU saat kompilasi atau inisialisasi.

Analisis komparatif menunjukkan:
- Uncompressed: 261 KB
- Minify saja (tanpa Gzip): ~165 KB
- **Gzip saja (stdlib Go `compress/gzip`): ~56 KB (penghematan ~78%)**
- Minify + Gzip: ~44 KB
- Delta antara "Gzip saja" dan "Minify + Gzip" di jaringan hanya **~12 KB**, yang tidak sebanding dengan biaya pemeliharaan build toolchain frontend.

## Keputusan Arsitektur

1. **Penolakan Toolchain Minifier (Zero-Dependency & Zero-Build)**:
   - Repositori mempertahankan `index.html` apa adanya (terbaca, mudah di-debug, dan terpusat) tanpa menambahkan `package.json`, Node.js, atau pustaka Go minifier eksternal.
   - Siklus kompilasi Go (`go build`) tetap 100% mandiri dan instan.

2. **Pre-Compressed In-Memory Gzip (Zero Runtime CPU Overhead)**:
   - Server memproses kompresi Gzip satu kali saja pada memori saat inisialisasi (`NewServer`) dan saat nomor versi diubah (`SetVersion`).
   - Kompresi dilakukan menggunakan pustaka standar Go `compress/gzip` dan disimpan dalam buffer in-memory `htmlContentGz []byte`.
   - Ketika peramban mengirim header `Accept-Encoding: gzip`, server langsung menuliskan buffer `htmlContentGz` ke koneksi HTTP dengan header `Content-Encoding: gzip`.
   - Mengeliminasi overhead alokasi CPU dan alokasi memori per-request yang biasanya terjadi pada dynamic gzip middleware.

3. **HTTP ETag dan Revalidasi `304 Not Modified`**:
   - Server menghitung hash SHA-256 ringkas dari konten HTML saat startup dan menetapkannya sebagai header `ETag`.
   - Ketika peramban melakukan revalidasi dengan header `If-None-Match`, server membandingkan hash tersebut. Jika cocok, server segera merespons dengan status **`304 Not Modified`** dan body kosong (**0 KB transfer**).
   - Ketika versi aplikasi diperbarui (`SetVersion`), hash ETag otomatis diperbarui sehingga peramban langsung mengunduh HTML versi terbaru tanpa risiko *stale cache*.

4. **Preservasi Keamanan & Kompatibilitas**:
   - Header keamanan *Content Security Policy* (CSP) ketat dari [ADR 0013](0013-airgapped-self-contained-web-assets.md) dan `Cache-Control: no-cache` tetap ditegakkan secara utuh.
   - Klien HTTP lama atau alat inspeksi yang tidak mendukung gzip secara otomatis menerima fallback konten mentah `htmlContent`.

5. **Verifikasi Pengujian Otomatis**:
   - Pengujian otomatis (`TestIndexGzipAndETagRevalidation`) ditambahkan ke `internal/web/server_test.go` untuk memvalidasi:
     - Header `ETag` selalu hadir.
     - Payload terkompresi valid dan dapat didekompresi sempurna dengan rasio kompresi tinggi (< 80 KB).
     - Permintaan revalidasi `If-None-Match` berhasil menghasilkan `304 Not Modified` berukuran 0 byte.
     - Pembaruan versi via `SetVersion` otomatis memicu perubahan ETag.
