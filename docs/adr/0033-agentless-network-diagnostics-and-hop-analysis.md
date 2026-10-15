# 0033. Perkakas Diagnostik Jaringan Interaktif dan Hop Analysis Agentless

Menyediakan modul pengujian jaringan on-demand berbasis SSH (Ping, Traceroute, Port Probe TCP, dan DNS Resolve) dengan analisis heuristik otomatis (*Hop Analysis*), proteksi singleflight, dan antarmuka visual terpadu tanpa instalasi agen remote.

## Konteks dan Masalah

Saat administrator menghadapi kendala konektivitas antar-server atau keterhubungan keluar (internet):
1. **ICMP Sering Diblokir**: Ping gagal belum tentu menandakan layanan/aplikasi mati karena banyak provider cloud dan firewall memblokir ICMP. Dibutuhkan uji port level transport (TCP Port Probe) untuk memastikan keterjangkauan service (HTTP/HTTPS, SSH, MySQL, Postgres, Redis).
2. **Keterbatasan Distro Minimalis**: Distro Linux minimalis (Ubuntu minimal, Alpine, cloud images) sering tidak memiliki binary `traceroute` bawaan atau memerlukan hak akses *raw socket* (root). Namun mereka biasanya memiliki `tracepath` bawaan atau utilitas standar lainnya.
3. **Kompleksitas Pembacaan Output**: Output mentah traceroute di terminal sering membingungkan sysadmin pemula maupun membuang waktu sysadmin senior dalam mengidentifikasi di mana paket terhenti (*blackhole drop*), apakah hop tersebut gateway lokal (RFC 1918) atau ISP transit, dan apakah terjadi lonjakan latensi (*latency spike*).
4. **Risiko Spam Proses SSH**: Eksekusi pengujian jaringan jarak jauh tanpa pembatas konkurensi dapat membebani koneksi SSH remote dan memicu penumpukan proses.

## Keputusan Arsitektur

1. **Paket Khusus `internal/netdiag`**:
   - Berisi definisi tipe domain (`ProbeType`, `ProbeRequest`, `HopInfo`, `HopAnalysis`, `PingStats`, `PortStats`, `DNSStats`), parser stream POSIX, dan `Service`.
   - Menghindari modifikasi destruktif pada loop inspeksi periodik Pantau yang sudah ada.

2. **Perintah Standar POSIX Agentless**:
   - **Ping**: `timeout 14 ping -c 4 -W 2 <target>`
   - **Traceroute**: Auto-fallback cerdas `command -v tracepath >/dev/null 2>&1 && (timeout 15 tracepath -n -m 20 <target>) || (timeout 15 traceroute -n -m 20 -q 1 -w 1 <target>)` (tidak memerlukan hak root).
   - **Port Probe (TCP)**: `timeout 3 bash -c '</dev/tcp/<target>/<port>'` (zero-dependency, tanpa butuh `nc`, `telnet`, atau `nmap`).
   - **DNS Resolve**: `getent hosts <target> || nslookup <target>` (menguji konfigurasi DNS aktif `/etc/resolv.conf`).

3. **Streaming Progresif Real-Time (SSE)**:
   - Endpoint `/api/netdiag/stream` menggunakan Server-Sent Events (`text/event-stream`) standar Go `http.Flusher` untuk menyalurkan event baris demi baris, kartu hop, dan ringkasan akhir secara instan tanpa websocket overhead.

4. **Heuristik Otomatis Hop Analysis**:
   - Mengklasifikasikan hop ke dalam zona: *Local/Private VPC* (RFC 1918 & CGNAT 100.64.0.0/10), *ISP / Upstream Transit*, dan *Target Destination*.
   - Deteksi *Blackhole Drop*: Mengidentifikasi titik persis di mana paket terhenti jika target tidak tercapai.
   - Deteksi *Latency Spike*: Menyoroti lompatan RTT signifikan (>60ms) antar hop berurutan.

5. **Proteksi Concurrency Guard (Singleflight)**:
   - Mencegah lebih dari satu sesi pengujian jaringan berjalan bersamaan pada Host sumber yang sama menggunakan `sync.Map`.
   - Timeout terkontrol (15s untuk path/ping, 5s untuk port/dns) dan auto-abort SSH runner saat request web dibatalkan.

6. **Titik Akses UI Ganda**:
   - **Host Detail Tab**: Tab baru `Network Diagnostics` pada Workspace Modal dengan Host sumber otomatis terkunci.
   - **Global Action Dialog**: Tombol akses cepat di header cluster navbar dengan pilihan Host sumber yang aktif online.
   - **Pencegahan Human-Error**: Host yang berstatus offline secara otomatis dinonaktifkan (`disabled`) dari daftar target inter-host.

## Konsekuensi

- **Diagnostik Instan & Akurat**: Sysadmin dapat mendiagnosis akar masalah konektivitas (apakah masalah DNS, firewall port, rute putus, atau host down) dalam hitungan detik.
- **Tanpa Beban Database**: Pengujian bersifat *transient in-memory*, tidak membebani tabel basis data SQLite Pantau.
- **Portabilitas Tinggi**: Berjalan di distro Linux apapun tanpa menginstal paket tambahan di server target.
