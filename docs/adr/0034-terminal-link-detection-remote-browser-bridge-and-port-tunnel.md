# 0034. Deteksi Link Terminal, Remote Browser Bridge, dan Web Port Tunneling Agentless

Menyediakan interaksi web dua arah dari terminal xterm Pantau: deteksi link URL interaktif (`Ctrl+Click`/`Cmd+Click`), reverse proxy tunneling port remote (`localhost:port`) berbasis channel SSH `direct-tcpip`, dan otomatisasi jembatan browser remote (*Remote Browser Bridge*) berbasis OSC 1337/777 tanpa instalasi agen remote.

## Konteks dan Masalah

Saat administrator mengoperasikan server Linux melalui web terminal xterm:
1. **Link Teks Terisolasi**: URL yang tercetak di terminal (link dokumentasi, URL pull request, dashboard web, dsb.) harus di-copy manual dengan mouse ke tab browser lain.
2. **Keterbatasan Remote Localhost**: Layanan web yang berjalan di port lokal server remote (misal `http://localhost:3000` atau `http://127.0.0.1:8080`) tidak dapat diakses langsung oleh browser laptop/desktop klien karena keterpisahan jaringan.
3. **Jebakan Browser Teks Headless**: Perintah CLI modern (seperti `gh auth login`, `xdg-open`, Python `webbrowser`, AWS SSO, `npm login`) secara otomatis mencoba membuka browser grafis. Pada server Linux remote tanpa GUI, Linux memicu browser teks berbasis konsol (seperti `w3m`, `links`, atau `lynx`) yang mengacaukan tampilan terminal dan membingungkan operator.
4. **Prinsip Non-Invasif (Agentless & Ponytail)**: Solusi tidak boleh memerlukan instalasi agen latar belakang, dependensi NPM baru di frontend, atau instalasi browser headless (Chromium/VNC) di backend master yang boros resource.

## Keputusan Arsitektur

1. **Deteksi Link Terminal Native (`term.registerLinkProvider`)**:
   - Memanfaatkan API bawaan core `xterm.js` tanpa menambah package/vendor eksternal baru.
   - Menggunakan regex RFC 3986 untuk mendeteksi URL `http://` dan `https://` serta alamat loopback ber-port (`localhost:PORT`, `127.0.0.1:PORT`).
   - Wajib menggunakan kombinasi **`Ctrl+Click`** (Linux/Windows) atau **`Cmd+Click`** (macOS) agar tidak mengganggu seleksi teks log dan copy-paste standar terminal.

2. **Reverse Proxy SSH Direct-TCPIP Agentless**:
   - Menambahkan method `DialContext(ctx context.Context, network, addr string) (net.Conn, error)` pada interface `sshrunner.Runner` dan `LiveSSHRunner` memanfaatkan channel standar OpenSSH RFC 4254 `direct-tcpip`.
   - Endpoint proxy: `/api/tunnel/:hostId/:port/*`.
   - Menggunakan `net/http/httputil.ReverseProxy` Go standar dengan custom `http.Transport` yang mengalirkan koneksi langsung ke `127.0.0.1:<port>` remote.
   - **Connection Pooling & Self-Healing**: Transport di-cache per host untuk menghindari latency handshake SSH (100–300ms) pada request aset web (CSS/JS/gambar) dengan auto-reconnect fallback jika channel idle terputus.
   - **Native WebSocket Hijacking**: `httputil.ReverseProxy` menangani respons `101 Switching Protocols` secara native untuk mendukung dev server modern (Vite HMR, Jupyter, dsb).
   - **Rewrite Prefix & Redirect**: Rewrite path request, injeksi `X-Forwarded-Prefix`, dan rewrite header `Location` pada HTTP redirect (301/302).

3. **Keamanan & Pencegahan SSRF**:
   - Proteksi penuh oleh `authMiddleware` (validasi sesi `pantau_session`).
   - Kunci keras loopback remote `127.0.0.1:<port>` untuk mencegah pemindaian IP internal LAN/VPC (anti-SSRF).
   - Validasi range port `1` s/d `65535` dan blacklist port SSH host target untuk mencegah loop koneksi rekursif.
   - Enforce trailing slash redirect (`/api/tunnel/:id/:port` ➔ `/api/tunnel/:id/:port/`) agar path relatif HTML resolve dengan benar.

4. **Zero-Configuration Remote Browser Bridge (OSC 1337 / OSC 777)**:
   - Pantau secara otomatis mem-provisioning script `/tmp/pantau-browser` pada host target dan mengekspor environment `BROWSER=/tmp/pantau-browser` serta `GH_BROWSER=/tmp/pantau-browser`.
   - Menyuntikkan konfigurasi secara senyap ke `~/.bashrc`, `~/.zshrc`, dan `~/.profile` serta `session.Setenv` tanpa menimbulkan echo perintah di layar terminal.
   - xterm.js mendaftarkan OSC Handler `1337` (`OpenURL=...`) dan `777`:
     - URL publik (misal `https://github.com/login/device`) langsung membuka tab browser desktop klien secara otomatis.
     - URL loopback remote otomatis dialihkan ke Remote Web Port Tunnel Pantau.

5. **Antarmuka Pengguna Terintegrasi**:
   - **Modal Konfirmasi Interaktif (`#tunnelModal`)**: Saat URL localhost diklik di terminal, muncul dialog ringkas berisi informasi host, nomor port, URL asli, tombol buka tunnel, dan tombol salin link.
   - **Toolbar Dropdown `🌐 Ports`**: Tombol menu cepat di samping *File Manager* dan *Pins* untuk membuka port remote secara bebas dan menyimpan riwayat port di *localStorage*.

## Konsekuensi

- **Pengalaman Otentikasi & Dev Mulus**: CLI tools seperti `gh auth login`, AWS SSO, dan dev server kini dapat membuka browser desktop pengguna secara instan tanpa glitch browser teks.
- **Observabilitas Web Service Tanpa Firewall Exposure**: Sysadmin dapat mengakses dashboard internal web pada remote server (port 3000, 8080, 9090) tanpa harus membuka port firewall ke publik.
- **100% Zero-Agent & Zero-Bloat**: Tidak ada binary atau service baru yang dipasang di server remote, menjaga portabilitas penuh Pantau.
