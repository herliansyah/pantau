# Spesifikasi: Terminal Link Detection & Remote Web Port Tunneling

Status: ready-for-agent

## Deskripsi
Fitur interaksi web dari terminal xterm Pantau yang memungkinkan pengguna mengklik link URL untuk membuka browser asli, serta menjembatani akses langsung ke web service internal/localhost pada remote host (`localhost:port`) melalui reverse proxy SSH `direct-tcpip` di backend Pantau secara 100% *agentless*.

## Keputusan Arsitektur
Sesuai hasil wayfinding di `.scratch/terminal-browser-tunnel/map.md`:

1. **Deteksi Link Terminal (Zero-Bloat xterm.js)**:
   - Menggunakan API bawaan `term.registerLinkProvider` tanpa dependensi file eksternal tambahan.
   - Wajib `Ctrl+Click` (Linux/Windows) atau `Cmd+Click` (macOS) agar tidak mengganggu seleksi teks log terminal.
   - URL publik (`https://...`) langsung dibuka di tab browser baru via `window.open`.
   - URL loopback remote (`localhost`, `127.0.0.1`, `0.0.0.0` ber-port) memicu dialog konfirmasi interaktif.

2. **Backend SSH Direct-TCPIP Reverse Proxy (Go)**:
   - Menambahkan method `DialContext(ctx context.Context, network, addr string) (net.Conn, error)` pada interface `sshrunner.Runner` dan `LiveSSHRunner` (`r.client.DialContext`).
   - Endpoint proxy: `/api/tunnel/{hostId}/{port}/*`.
   - Menggunakan `net/http/httputil.ReverseProxy` dengan custom `http.Transport{DialContext: ...}`.
   - Native support untuk HTTP/1.1 dan WebSocket (Go auto-hijack status `101 Switching Protocols`).
   - Menggunakan `ProxyRequest.Rewrite` untuk strip subpath prefix, menambahkan `X-Forwarded-Prefix`, dan `ModifyResponse` untuk rewrite redirect header `Location`.

3. **Keamanan & Restriksi**:
   - Dilindungi penuh oleh `authMiddleware` (validasi session cookie `pantau_session`).
   - Anti-SSRF: Alamat dial dikunci mutlak ke loopback remote `127.0.0.1:<port>`.
   - Validasi range port `1` - `65535` dan blacklist port SSH host target.
   - Timeout idle connection 90 detik untuk mencegah kebocoran channel SSH.

4. **UI Quick-Access**:
   - Modal konfirmasi interaktif saat URL remote diklik di xterm.
   - Tombol menu `🌐 Ports` di toolbar terminal dengan input manual port dan riwayat port yang pernah dibuka.

5. **Automatic Remote Browser Bridge (Zero-Configuration)**:
   - Pantau secara otomatis mem-provisioning script `/tmp/pantau-browser` pada remote host dan menyetel `BROWSER=/tmp/pantau-browser` serta `GH_BROWSER=/tmp/pantau-browser`.
   - Menghubungkan standar terminal OSC 1337 (`OpenURL`) dan OSC 777 di xterm.js: URL publik langsung dibuka di tab browser desktop klien, sedangkan URL loopback remote otomatis diarahkan via Pantau Tunnel.
   - Berlaku universal untuk semua tools di remote server (`gh auth login`, `xdg-open`, Python `webbrowser`, AWS SSO, `npm login`, dsb) tanpa perlu mengetik perintah export manual.
