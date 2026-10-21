# 02 — Arsitektur SSH Direct-TCPIP Reverse Proxy di Go

Type: research
Status: resolved
Blocked by:

## Question

Bagaimana implementasi teknis Go backend untuk melakukan HTTP reverse proxy dan WebSocket relay ke remote port target (`127.0.0.1:<port>`) menggunakan koneksi SSH client yang sudah ada (`ssh.Client.Dial` / direct-tcpip) tanpa menginstal software atau membuka port firewall publik di remote server?

## Answer

Go backend dapat melakukan reverse proxy HTTP dan streaming WebSocket dua arah secara *agentless* memanfaatkan standar SSH RFC 4254 `direct-tcpip` menggunakan Go standard library:

1. **Expose `DialContext` pada `sshrunner.Runner`**:
   `golang.org/x/crypto/ssh.Client` memiliki method bawaan `DialContext(ctx, "tcp", addr)`. Ketika dipanggil, SSH client meminta channel `direct-tcpip` ke remote `sshd` untuk membuka koneksi TCP lokal ke `127.0.0.1:<port>` dari loopback remote host itu sendiri, mengembalikan `net.Conn`.
2. **Konfigurasi `httputil.ReverseProxy`**:
   Gunakan custom `http.Transport` yang mengarahkan `DialContext` melalui SSH runner:
   ```go
   transport := &http.Transport{
       DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
           return runner.DialContext(ctx, "tcp", fmt.Sprintf("127.0.0.1:%d", remotePort))
       },
   }
   ```
3. **Dukungan WebSocket Bawaan**:
   `httputil.ReverseProxy` Go secara otomatis mendeteksi respons `101 Switching Protocols`, melakukan socket hijacking, dan mem-pipe stream TCP/WebSocket secara full-duplex melalui SSH direct-tcpip channel tanpa dependensi library tambahan.
4. **Strategi Routing Subpath & Absolute Assets**:
   Proxy dipetakan pada endpoint `/api/tunnel/:hostId/:port/`. Menggunakan `ProxyRequest.Rewrite` untuk strip routing prefix, menambahkan `X-Forwarded-Prefix`, dan `ModifyResponse` untuk rewrite header `Location` (redirect 301/302).
