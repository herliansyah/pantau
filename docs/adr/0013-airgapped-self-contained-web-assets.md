# Airgapped Self-Contained Web Assets dan Eliminasi Ketergantungan CDN Eksternal

Lokalisasi penuh seluruh aset pustaka web antarmuka pengguna (xterm.js dan CodeMirror) ke dalam binary Pantau, penyajian lokal melalui endpoint `/vendor/*` berbasis `embed.FS`, dan penegakan *Content Security Policy* (CSP) ketat untuk menjamin ketersediaan antarmuka di jaringan terisolasi (*airgapped*).

## Konteks dan Masalah

Sebelumnya, antarmuka web Pantau mengandalkan CDN publik eksternal (`cdn.jsdelivr.net` dan `cdnjs.cloudflare.com`) untuk memuat stylesheet CSS dan skrip JavaScript pihak ketiga:
- Pustaka terminal: `xterm.js`, `xterm-addon-fit.js`, dan `xterm.css`.
- Pustaka editor file SFTP: `codemirror.min.js`, `codemirror.min.css`, tema `nord.min.css`, serta 8 mode bahasa pemrograman (`xml`, `javascript`, `css`, `htmlmixed`, `clike`, `php`, `shell`, `yaml`).

Dalam skenario deployment server produksi korporat, server pemantau sering kali ditempatkan pada jaringan intranet privat, VPC terisolasi, atau lingkungan tanpa akses internet langsung (*airgapped*). Pada kondisi ini:
1. Peramban pengguna gagal mengunduh pustaka dari CDN publik, menyebabkan fitur Web Terminal dan Editor File SFTP tidak berfungsi sama sekali (*silent failure* / UI rusak).
2. Terdapat risiko privasi dan keamanan di mana peramban membocorkan alamat IP atau metadata saat meminta aset ke server CDN pihak ketiga.
3. Inkonsistensi arsitektur dengan prinsip *zero-ops single binary* pada [ADR 0002](file:///home/ian/emdash/worktrees/pantau-48c85891/emdash-kind-hounds-win-18nr4/docs/adr/0002-golang-single-binary-sqlite.md) yang mengedepankan binary mandiri tanpa ketergantungan runtime eksternal.

## Keputusan Arsitektur

1. **Penyimpanan Lokal Mandiri (Zero-Node / Direct Vendor Commit)**:
   - Seluruh aset vendor minified disimpan langsung di direktori repositori: `internal/web/static/vendor/` dan `internal/web/static/vendor/mode/`.
   - Tidak menambahkan perkakas Node.js, `package.json`, atau bundler eksternal di dalam siklus kompilasi. Kompilasi Go (`go build`) tetap 100% mandiri dan langsung menyematkan aset melalui pustaka standar `embed.FS`.

2. **Penyajian Endpoint `/vendor/*` dan Diferensiasi Caching**:
   - Direktori `static/` di-embed menggunakan `//go:embed static` dan diakses melalui sub-filesystem `fs.Sub(embeddedFS, "static/vendor")`.
   - Endpoint `/vendor/*` disajikan oleh `http.FileServer` dengan header cache permanen:
     ```http
     Cache-Control: public, max-age=31536000, immutable
     ```
     Hal ini memastikan browser tidak melakukan revalidasi HTTP berulang untuk library pihak ketiga, menghasilkan waktu buka (*load time*) instan.
   - Endpoint root `/` (HTML utama) disajikan dengan `Cache-Control: no-cache` untuk menjamin rilis pembaruan UI Pantau langsung diterima oleh klien tanpa hambatan cache usang.

3. **Penegakan Protokol Keamanan Ketat (Content Security Policy)**:
   - Server menyisipkan header HTTP keamanan pada handler antarmuka root `/`:
     ```http
     Content-Security-Policy: default-src 'self'; script-src 'self' 'unsafe-inline'; style-src 'self' 'unsafe-inline'; connect-src 'self' ws: wss:; font-src 'self' data:; img-src 'self' data:;
     ```
   - Browser secara aktif akan menolak koneksi keluar apa pun ke CDN atau domain pihak ketiga jika ada skrip liar yang mencoba memuat aset eksternal.

4. **Preservasi 8 Mode Sintaksis CodeMirror**:
   - Seluruh 8 mode syntax (`xml`, `javascript`, `css`, `htmlmixed`, `clike`, `php`, `shell`, `yaml`) dipertahankan secara lokal untuk memastikan pengalaman editing konfigurasi Linux (Nginx, PHP-FPM, Docker compose, bash script, config C) tetap utuh tanpa degradasi fitur.

5. **Pengujian Regresi Otomatis**:
   - Pengujian unit otomatis (`TestAirgappedSelfContainedAssets`) ditambahkan pada suite pengujian untuk memverifikasi secara ketat ketiadaan referensi CDN publik di dalam HTML, keaktifan header CSP, dan integritas respon HTTP 200 pada seluruh file vendor lokal.
