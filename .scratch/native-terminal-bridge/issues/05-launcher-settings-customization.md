# 05 — Terminal Launcher Settings & Custom Command Template

Type: grilling
Status: resolved
Blocked by: 01

## Question

Bagaimana skema penyimpanan preferensi terminal di Settings Pantau untuk mengakomodasi pengguna power-user/tiling window manager (misal template `alacritty -e %cmd%` atau `foot -e %cmd%`), validasi keamanan terhadap injeksi perintah sembarang (command injection prevention), dan mekanisme fallback saat executable yang dikonfigurasi tidak ditemukan pada OS?

## Answer

1. **Skema Penyimpanan Hybrid (Server Setting + Client Override)**:
   - Nilai default tersimpan di konfigurasi server Pantau (`terminal_launcher_mode`: `auto` | `custom`, `terminal_launcher_template`: string).
   - Antarmuka web mendukung penyimpanan preferensi per-perangkat di `localStorage` peramban klien, memungkinkan sysadmin menggunakan template terminal berbeda saat mengakses dari laptop Windows, Linux tiling WM, atau macOS.

2. **Validasi Keamanan Ketat (*Strict Pre-flight Check*)**:
   - Memvalidasi keberadaan placeholder `%cmd%` secara mutlak.
   - Mengurai token executable pertama dan memverifikasinya melalui `exec.LookPath` sebelum setting disimpan; menolak simpan jika biner tidak ditemukan atau mengandung karakter metakarakter shell berbahaya.
   - Pemisahan argumen (*tokenization*) dilakukan secara deterministik tanpa melewatkan string mentah ke `eval`.

3. **Mekanisme Graceful Fallback**:
   - Jika executable kustom tidak ditemukan pada saat runtime peluncuran (misal biner dihapus pasca konfigurasi), sistem otomatis beralih (*graceful degradation*) ke kaskade deteksi bawaan OS (Tiket 01) dan memunculkan notifikasi toast informatif kepada pengguna tanpa menghentikan eksekusi.
