# 0030. In-App Activity Console dan Live Event Stream

Menyediakan observabilitas operasional runtime secara real-time di dalam antarmuka Pantau melalui langganan Server-Sent Events (SSE) dan laci diagnostik interaktif (*Activity Console*), mempertahankan standar *zero-dependency* Ponytail tanpa pustaka pihak ketiga.

## Konteks dan Masalah

1. **Ketiadaan Visibilitas Operasional In-App Seketika**:
   Meskipun Pantau mencatat riwayat inspeksi ke basis data SQLite, aktivitas runtime lainnya (koneksi SSH, handshake, transfer SFTP, sesi terminal PTY, backup snapshot ke GitHub, dan peringatan drift/error) berlangsung secara tersembunyi tanpa umpan balik langsung ke pengguna di browser, kecuali pengguna melihat terminal server Pantau.
2. **Overhead & Ketergantungan Protokol Realtime**:
   Mengirimkan log peristiwa dari backend Go ke web browser kerap memicu godaan memakai pustaka WebSocket pihak ketiga (`gorilla/websocket` atau sejenisnya) atau polling berulang (`GET /api/logs?since=...`) yang menambah overhead CPU, latensi, dan chatter jaringan.
3. **Ergonomi dan Kenyamanan Pembacaan Operasional**:
   Saat aktivitas sistem tinggi, log yang terus mengalir (*running stream*) sulit dibaca jika tidak dapat di-pause (*freeze*). Administrator memerlukan kontrol viewport cerdas (*Smart Auto-Scroll*), penyaringan tingkat keparahan (*Level Filter*), pencarian cepat, penyensoran kredensial sensitif (*Redaction Filter*), dan ekspor data log ke format `.log` / `.json`.

## Keputusan Arsitektur

1. **Zero-Dependency SSE Telemetry Transport & In-Memory Ring Buffer**:
   - Mengimplementasikan `internal/logger` membungkus Go standard library `log/slog`.
   - Menggunakan `Telemetry Ring Buffer` in-memory dengan kapasitas tetap 100 entri dan `Broadcaster` non-blocking berbasis Go channels (`chan Event`).
   - Menyediakan endpoint SSE `GET /api/logs/stream` menggunakan `http.Flusher` bawaan `net/http` dan native browser `EventSource`.
   - Terlindungi oleh middleware autentikasi sesi administrator Pantau.

2. **Penyensoran Kredensial Sensitif (*Redaction Filter*)**:
   - Filter deterministik sebelum log dipancarkan ke Live Event Stream untuk menyensor kata sandi SSH, passphrase snapshot, private key, token GitHub, dan secret 2FA menjadi `[REDACTED]`.

3. **Activity Console Collapsible Bottom Drawer**:
   - Laci diagnostik yang disematkan tepat di atas footer Pantau dengan tinggi default 260px (dapat di-minimize/close atau di-maximize hingga 50vh).
   - Menampilkan tabel berjalan 5-kolom: `Time`, `Level`, `Module`, `Host`, dan `Message & Details`.
   - Tombol status terpadu di footer `⚡ Activity` dengan indikator pulse hijau dan badge hitungan unread error merah, serta shortcut global `Ctrl + \`` (backtick).
   - Fitur toolbar: Pause / Resume (Smart Auto-Scroll), Level Filter (`ALL`, `ERROR`, `WARN`, `INFO`), Search Box instan, Clear view, serta tombol Export (`.log` & `.json`).

4. **Optimasi Performa & Throttling Render**:
   - Membatasi retensi baris tabel di browser hingga maksimum 500 baris (FIFO).
   - Melewatkan 100% manipulasi DOM saat drawer sedang ditutup (`hidden`), dan melakukan perenderan batch menggunakan `requestAnimationFrame` saat drawer dibuka kembali guna mencegah layout thrashing dan pemborosan CPU.

## Konsekuensi

- Administrator mendapatkan transparansi penuh atas apa yang sedang dilakukan Pantau setiap saat secara real-time langsung dari antarmuka web.
- Arsitektur tetap murni standar library Go tanpa menambah dependensi eksternal apa pun (`go.mod` tetap bersih).
- Keamanan terjamin dengan proteksi autentikasi sesi dan sensor kredensial otomatis.
