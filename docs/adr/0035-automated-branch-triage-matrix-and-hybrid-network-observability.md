# 0035. Automated Network Triage Matrix dan Observabilitas Jaringan Hybrid Agentless

Menyediakan mesin diagnosis komprehensif satu-klik (*Network Triage Matrix*) yang adaptif untuk berbagai topologi jaringan (Private VPN, Public WAN, FQDN/DDNS, atau Hybrid) untuk membedakan secara objektif dan instan apakah gangguan aplikasi web disebabkan oleh kelebihan beban server aplikasi (Host), kemacetan rute transit ISP/VPN, Path MTU Blackhole, atau penolakan firewall, dilengkapi dengan pengujian L7 HTTP, MTU Sweep, fallback TCP probe pintar, pencatatan log insiden ke SQLite, dan generator tiket insiden siap kirim.

## Konteks dan Masalah

Pantau didistribusikan sebagai proyek *open-source* dengan lingkungan pengguna yang sangat heterogen:
1. **Keberagaman Topologi Akses**:
   - Sebagian pengguna mengoperasikan server pada jaringan privat murni (Site-to-Site VPN, WireGuard, IPsec, Tailscale, SD-WAN, atau MPLS).
   - Sebagian pengguna mengoperasikan server di VPS/Cloud publik dengan FQDN/domain yang diakses cabang lewat internet publik (ISP Indihome, Biznet, dsb.).
   - Sebagian kantor cabang menggunakan IP publik dinamis dengan Dynamic DNS (DDNS) atau berada di balik CGNAT modem GSM/LTE.
2. **Debat Klise "Server vs Jaringan"**: Saat pengguna cabang mengeluhkan aplikasi web lemot atau down, sysadmin dan developer saling melempar tanggung jawab tanpa data komparatif yang memisahkan beban lokal Host dengan jalur transmisi.
3. **Keterbatasan Diagnostik Statis L3/L4**: Uji ping (ICMP) sering diblokir oleh firewall ISP atau router cabang sehingga memicu *false alarm* bahwa cabang mati, padahal port web/VPN aktif. Sebaliknya, ping yang sukses sering menipu sysadmin padahal aplikasi web mengalami deadlock (HTTP 502/504) atau *Path MTU Blackhole* (paket HTML kecil lolos, paket tabel data besar >1420 bytes macet).
4. **Ketiadaan Bukti Rekam Jejak (Audit Incident Log)**: Tanpa riwayat tersimpan, sysadmin kesulitan membuktikan ke penyedia layanan transit/ISP cabang bahwa gangguan konektivitas terjadi pada jam tertentu.

## Keputusan Arsitektur

1. **Format Sasaran Universal (Universal Auto-Sensing Target)**:
   - Field target pada `ProbeTargetPreset` menerima format fleksibel:
     - **IP Privat** (RFC 1918 `10.x`, `172.16.x`, `192.168.x`): Diidentifikasi otomatis sebagai rute *Private/VPN/Intranet*.
     - **IP Publik**: Diidentifikasi otomatis sebagai rute *Direct Public WAN*.
     - **Hostname / FQDN / DDNS** (misal: `cabang.ddns.net` atau `app.corp.internal`): Sistem otomatis menyisipkan resolusi DNS (`getent hosts` / `nslookup`) di awal untuk mendapatkan IP aktif terkini sebelum probe diluncurkan.

2. **Pipeline Eksekusi Diagnostik Dinamis (Dynamic Pipeline)**:
   - **Tahap 1: Host & Application Self-Check (Universal)**:
     - Menilai saturasi CPU/RAM/Swap, I/O disk, dan koneksi socket lokal.
     - **Dual L7 HTTP Probe**: Host A mengeksekusi `curl` via SSH ke `http://127.0.0.1:<port>/<path>` (mengukur HTTP code dan TTFB lokal), sementara Master Pantau mem-probe endpoint dari luar untuk memvalidasi integritas reverse proxy/firewall lokal.
   - **Tahap 2: Route & Interface Discovery**:
     - Mengeksekusi `ip route get <target_ip>` di Host A untuk memverifikasi antarmuka transmisi (`dev wg0`/`dev eth0`), gateway perantara, dan MTU aktif.
   - **Tahap 3: Branch Reachability & Path Analysis**:
     - *Jika Jalur Privat/VPN*: Menjalankan **MTU Sweep Probe** (`ping -M do -s 1300/1420/1472 <target_ip>`) untuk memvalidasi fragmentasi paket.
     - *Jika Jalur Internet Publik*: Menjalankan **Hop Analysis** (traceroute/tracepath) untuk mendeteksi titik lonjakan latensi atau *blackhole drop* pada hop transit ISP.
   - **Tahap 4: Intelligent Firewall Fallback (Anti-False Alarm)**:
     - Jika ICMP ping mengalami 100% loss, sistem otomatis menjalankan **TCP Port Probe** ke port layanan. Jika port terbuka, status tidak ditandai sebagai down, melainkan `ICMP_FILTERED_TCP_HEALTHY`.

3. **Mesin Penentu Kesimpulan Netral (Triage Verdict Engine)**:
   - Mengklasifikasikan kesimpulan secara deterministik ke dalam 7 status baku:
     - `HOST_SATURATED`: Host A kehabisan sumber daya komputasi (CPU/RAM/IO load tinggi atau socket starvation).
     - `APPLICATION_DEGRADED`: Host A sehat, rute transmisi lancar, namun respons L7 HTTP gagal (HTTP 5xx) atau TTFB lokal melebihi batas SLA (>2000ms).
     - `TRANSIT_PATH_DEGRADED`: Host A dan aplikasi sehat, namun jalur transmisi ke cabang mengalami packet loss (>2%) atau lonjakan latensi tinggi (dengan rincian kontekstual: apakah VPN bottleneck atau ISP transit drop).
     - `BRANCH_GATEWAY_DOWN`: Target cabang tidak merespons sama sekali baik via ICMP maupun TCP port probe.
     - `MTU_BLACKHOLE_DETECTED`: Ping paket kecil lolos, namun paket ukuran MTU tunnel standar (>1400 bytes) di-drop karena restriksi DF bit.
     - `ICMP_FILTERED_TCP_HEALTHY`: Ping ICMP diblokir oleh firewall target/ISP, namun konektivitas port TCP layanan terbuka normal.
     - `HEALTHY`: Seluruh parameter (Host, L7 App, dan Jalur Jaringan) beroperasi optimal.

4. **Perluasan Entitas `ProbeTargetPreset`**:
   - Menambahkan metadata operasional pada tabel `probe_target_presets`:
     - `app_endpoint`: URL atau path target HTTP aplikasi (default: `/`).
     - `baseline_mtu`: Batas ambang MTU terowongan (default: `1420` untuk VPN, `1500` untuk direct WAN).
     - `max_latency_ms`: Ambang batas SLA latensi (default: `80`).
     - `max_loss_pct`: Ambang batas toleransi packet loss (default: `2.0`).

5. **Persistensi Riwayat Insiden (`Triage Incident Log`)**:
   - Menyimpan setiap eksekusi Triage Matrix ke dalam tabel `triage_incident_logs` di basis data SQLite lokal Pantau.
   - Menyimpan snapshot metrik telemetri (waktu, host sumber, preset cabang, rincian TTFB, packet loss, status MTU, verdict, dan raw excerpt) dengan retensi otomatis.

6. **Generator Tiket Insiden Cepat (*Incident Ticket Snippet*)**:
   - Menyediakan fitur satu-klik pada antarmuka web untuk menyalin ringkasan insiden dalam format plain text/Markdown siap kirim ke WhatsApp, Telegram, atau tiket keluhan ISP.

7. **Perlindungan Concurrency & Mode Canary Hybrid**:
   - Menggunakan mekanisme singleflight per Host sumber dan per target cabang dengan timeout maksimal 20 detik untuk mencegah penumpukan proses SSH.
   - Mendukung eksekusi berkala terjadwal (*Canary Triage*) dengan auto-backoff jika status cabang terdeteksi down.

## Konsekuensi

- **Fleksibilitas Open Source Maksimal**: Bekerja mulus di berbagai macam lingkungan pengguna (VPN korporat, server VPS publik, cabang DDNS/CGNAT) tanpa konfigurasi manual yang rumit.
- **Bebas False Alarm**: Fallback ke TCP probe mencegah laporan palsu bahwa cabang down saat ICMP diblokir firewall.
- **Isolasi Masalah Seketika**: Sysadmin dan tim dev dapat membedakan kendala server vs kendala ISP/VPN dalam waktu kurang dari 15 detik dengan 1 klik.
- **Tetap Agentless & Zero Dependency**: Seluruh fitur memanfaatkan utilitas standar Linux (`curl`, `ip`, `ping`, `tracepath`) tanpa agen di remote maupun router cabang.
