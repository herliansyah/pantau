# 01 — Terminal Link Detection & Interaction UX

Type: grilling
Status: resolved
Blocked by:

## Question

Bagaimana xterm mendeteksi URL dan menentukan interaksi klik (native `registerLinkProvider` vs vendored addon, modifier key click vs single click), serta bagaimana membedakan URL publik (langsung buka tab baru) vs URL localhost/internal host (ditawarkan untuk dibuka via Pantau Tunnel)?

## Answer

1. **Deteksi Link Native Zero-Bloat**:
   Menggunakan API bawaan xterm.js `term.registerLinkProvider` (sudah tersedia di file xterm vendor Pantau tanpa perlu mengunduh package/addon eksternal). Regex mengenali URL dengan protokol `http://` dan `https://`.
2. **Modifier Key Trigger (`Ctrl+Click` / `Cmd+Click`)**:
   Untuk mencegah bentrok dengan fitur seleksi dan copy teks di terminal, pembukaan link diwajibkan menggunakan kombinasi:
   - `Ctrl+Click` pada Windows dan Linux.
   - `Cmd+Click` pada macOS.
   - Hover saat modifier key aktif menampilkan underline dan pointer cursor serta tooltip URL. Klik biasa tanpa modifier tetap berperan sebagai seleksi teks standar.
3. **Pembedaan URL Publik vs Localhost**:
   - **URL Publik** (`https://...` atau host non-loopback): Langsung dibuka di tab browser baru via `window.open(url, '_blank', 'noopener,noreferrer')`.
   - **URL Loopback / Localhost** (`localhost`, `127.0.0.1`, `0.0.0.0` dengan port spesifik): Menampilkan prompt/toast konfirmasi ringkas (Opsi B):
     > *"Deteksi Web Service Remote: Port {port}. Buka via Pantau Tunnel?"*
     Pengguna dapat memilih **"Buka Tunnel"** (yang akan membuka `/api/tunnel/{hostId}/{port}/{path}` di tab baru) atau **"Batal"**.
