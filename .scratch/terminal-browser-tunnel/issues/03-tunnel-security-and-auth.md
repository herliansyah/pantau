# 03 — Mekanisme Keamanan & Autentikasi Tunnel

Type: grilling
Status: resolved
Blocked by: 02

## Question

Bagaimana mekanisme autentikasi dan restriksi keamanan untuk endpoint reverse proxy tunnel di Pantau (validasi session, rate limiting, mitigasi SSRF, dan proteksi port sensitif lokal/internal)?

## Answer

1. **Autentikasi Sesi Wajib (`authMiddleware`)**:
   Endpoint `/api/tunnel/:hostId/:port/*` dan WebSocket handshake dibungkus penuh oleh `s.authMiddleware`. Request tanpa cookie sesi `pantau_session` yang valid langsung ditolak dengan status HTTP `401 Unauthorized`.
2. **Mitigasi SSRF Mutlak (Strict Remote Loopback)**:
   Channel `direct-tcpip` SSH dikunci mati ke alamat `127.0.0.1:<port>` pada loopback internal host remote. Parameter target IP dari klien ditolak untuk mencegah Pantau disalahgunakan sebagai open proxy pemindaian LAN/VPC internal.
3. **Validasi & Blacklist Port**:
   - Port wajib berada pada rentang valid `1` hingga `65535`.
   - Blacklist port SSH host target (port SSH aktif host, default 22) untuk mencegah loop rekursif koneksi SSH.
   - Verifikasi host wajib: Memeriksa keberadaan host di database Pantau (`s.db.GetHost(hostId)`) sebelum menginisiasi koneksi runner.
4. **Proteksi Resource & Timeout**:
   - `Transport.IdleConnTimeout` (90 detik) dan `ResponseHeaderTimeout` (30 detik) diterapkan agar channel SSH direct-tcpip yang tidak aktif segera dilepas dari file descriptor remote `sshd`.
   - Menghandle response non-HTTP secara graceful dengan status `502 Bad Gateway`.
