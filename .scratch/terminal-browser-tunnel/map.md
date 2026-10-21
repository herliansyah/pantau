# Map: Terminal Link Detection & Remote Web Port Tunneling

Status: resolved

## Destination

Menghadirkan kemampuan interaksi web dari terminal xterm Pantau: mendeteksi dan mengklik link URL secara interaktif (buka di tab browser asli), serta menjembatani akses ke web service remote (`localhost:port` pada Host target) via SSH direct-tcpip reverse proxy di backend Pantau tanpa memerlukan agen tambahan di remote server.

## Notes

- Domain: Web Terminal (xterm.js), SSH Direct-TCPIP Tunneling, HTTP/WebSocket Reverse Proxy, Agentless Linux Management.
- Skills to consult: `/grilling`, `/domain-modeling`, `/research`, `/codebase-design`.
- Standing preferences: Prinsip Ponytail (Lazy senior dev) — gunakan native API xterm.js (`registerLinkProvider`) tanpa bloat build system baru, manfaatkan `ssh.Client.Dial` bawaan Go `golang.org/x/crypto/ssh` dan `net/http/httputil.ReverseProxy`, zero external daemon/agent pada remote host.

## Decisions so far

<!-- the index — one line per closed ticket -->

- [01 — Terminal Link Detection & Interaction UX](issues/01-terminal-link-interaction.md) — Menggunakan native `term.registerLinkProvider`, trigger pembukaan link via `Ctrl+Click` / `Cmd+Click`, pembukaan langsung tab baru untuk URL publik, dan modal konfirmasi untuk URL remote `localhost:port` sebelum membuka endpoint tunnel Pantau.
- [02 — Arsitektur SSH Direct-TCPIP Reverse Proxy di Go](issues/02-ssh-direct-tcpip-proxy.md) — Expose `DialContext` pada `sshrunner.Runner`, gunakan `httputil.ReverseProxy` dengan custom `http.Transport` via SSH channel `direct-tcpip`, native WebSocket hijacking (101 switching), dan subpath routing `/api/tunnel/:hostId/:port/`.
- [03 — Mekanisme Keamanan & Autentikasi Tunnel](issues/03-tunnel-security-and-auth.md) — Autentikasi penuh dengan `authMiddleware` (cookie sesi), kunci mutlak loopback `127.0.0.1:<port>` anti-SSRF, blacklist port SSH host, dan batas timeout koneksi idle.
- [04 — Integrasi UI Port Forwarding & Quick-Access](issues/04-ui-port-forwarding-integration.md) — Modal konfirmasi interaktif saat URL remote diklik di terminal, serta penambahan menu dropdown `🌐 Ports` di toolbar terminal untuk input manual dan riwayat port remote.

## Not yet specified

- Penanganan deep path rewriting untuk Single Page Apps (SPA) atau assets dengan absolute path (`/static/...`) yang berjalan di remote port jika diakses lewat subpath proxy Pantau.
- Auto-detection port listening aktif dari background inspector telemetri Pantau untuk menyarankan link tunnel secara otomatis saat user membuka terminal.

## Out of scope

- Menjalankan remote headless browser/Chromium/VNC di server master Pantau (melanggar prinsip lightweight & agentless).
- Arbitrary SOCKS5 / full L3 VPN proxy (terlalu luas, di luar kebutuhan inspeksi web service).
