# 0031. Penguatan Keamanan Sistem dan Kompatibilitas OS Linux Lawas

Memperkuat postur keamanan seluruh subsistem Pantau (pencegahan command injection, proteksi WebSocket/CSWSH, token sesi kriptografis, verifikasi TOFU SSH, dan proteksi bootstrap) dengan tetap mempertahankan 100% kompatibilitas terhadap CentOS 5 / Linux lawas dan akses plain HTTP (tanpa SSL/HTTPS).

## Konteks dan Masalah

Berdasarkan audit keamanan komprehensif terhadap basis kode Pantau, ditemukan beberapa potensi celah keamanan:
1. **Risiko Command Injection pada Operasi Shell Remote**:
   Penggunaan pemformatan string Go `%q` (double quotes) pada perintah remote (`du`, `df`, `cp`, `tar`, `cat`, `sha256sum`, `docker logs`, `docker inspect`, dll.) tidak menetralkan karakter ekspansi variabel shell seperti `$()` dan backtick dalam spesifikasi POSIX shell.
2. **Ketiadaan Autentikasi dan Proteksi CSWSH pada WebSocket**:
   Endpoint `/ws/terminal` dan `/ws/docker/logs` dapat diakses tanpa sesi autentikasi dan `CheckOrigin` mengizinkan seluruh origin tanpa validasi, membuka risiko pembajakan sesi interaktif dari situs pihak ketiga (Cross-Site WebSocket Hijacking).
3. **Pembangkitan Token Sesi Berbasis Timestamp**:
   Token sesi dan tantangan 2FA sementara sebelumnya dibentuk menggunakan kombinasi `time.Now().UnixNano()` dan `Unix()` yang memiliki entropi rendah dan rentan ditebak oleh penyerang lokal/jaringan.
4. **Celah Wipe / Impor pada Sistem Baru (Unauthenticated Bootstrap Wipe)**:
   Pemeriksaan kondisi sistem segar (`IsFresh()`) hanya memeriksa jumlah host (`hosts == 0`), sehingga penyerang di jaringan lokal dapat memanggil `/api/snapshot/import` atau start-fresh untuk menghapus basis data sebelum admin menambahkan host.
5. **Kebutuhan Kompatibilitas Mutlak dengan Server Lawas (CentOS 5) & Akses Plain HTTP**:
   Pengguna mengelola server produksi warisan berbasis CentOS 5 (OpenSSH 4.3p2) dan CentOS 6 tanpa dukungan cipher modern. Selain itu, Pantau diakses melalui jaringan internal/LAN menggunakan protokol plain HTTP tanpa sertifikat SSL/HTTPS, sehingga pemaksaan flag `Secure` pada cookie akan memutus akses pengguna.

## Keputusan Arsitektur

1. **Universal POSIX Single-Quoting Escaping (`shellQuote`)**:
   - Menerapkan fungsi helper `shellQuote(s)`: `'` + `strings.ReplaceAll(s, "'", "'\\''")` + `'`.
   - Menggantikan seluruh Go `%q` pada pembentukan perintah remote di `internal/web/server.go`, `internal/transfer/transfer.go`, dan `internal/inspector/inspector.go`.
   - Single-quote adalah standar mutlak Bourne shell sejak 1979 yang dijamin 100% universal dan aman di seluruh distribusi Linux (dari CentOS 5 hingga Ubuntu modern) tanpa memerlukan dependensi eksternal.

2. **Proteksi Autentikasi dan Validasi Host Origin pada WebSocket**:
   - Membungkus rute `/ws/terminal` dan `/ws/docker/logs` dengan `authMiddleware` sehingga hanya klien dengan sesi login valid yang dapat membuka shell interaktif atau streaming log.
   - Mengganti `CheckOrigin: return true` dengan pencocokan host `strings.EqualFold(u.Host, r.Host)` (mengizinkan empty origin untuk klien non-browser).
   - Menghindari pemaksaan skema `https://` sehingga kompatibel penuh dengan akses web berbasis plain HTTP pada IP/domain dan port apa pun, sekaligus memblokir serangan CSWSH dari domain pihak ketiga (misal: `http://evil.com`).

3. **Pembangkitan Token Entropi Tinggi (`crypto/rand`) dengan Preservasi Plain HTTP**:
   - Menggunakan `crypto/rand` (32 bytes hex untuk sesi, 16 bytes hex untuk token 2FA sementara).
   - Mempertahankan atribut cookie sesi `HttpOnly: true`, `SameSite: Lax`, dan `Secure: false` agar browser tetap menyimpan dan mengirimkan cookie sesi pada koneksi plain HTTP.

4. **Validasi Input Ketat pada Rule Target dan Container**:
   - Memvalidasi nama container pada `/ws/docker/logs` dan inspeksi menggunakan regex aman `^[a-zA-Z0-9][a-zA-Z0-9_.-]+$`.
   - Memblokir metakarakter shell (`;&|`$\n\r`) pada pendaftaran Desired State rule target.
   - Memvalidasi target port agar hanya menerima angka port valid (1–65535).

5. **Verifikasi Host Key TOFU (Trust On First Use) & Dukungan Host Key DSA**:
   - Mengganti `ssh.InsecureIgnoreHostKey()` dengan `TOFUHostKeyCallback()`. Koneksi pertama ke host mana pun (termasuk CentOS 5) otomatis diterima dan dicatat tanpa gesekan manual `known_hosts`. Koneksi berikutnya divalidasi terhadap fingerprint yang tercatat untuk menangkal serangan MITM aktif.
   - Menambahkan algoritma host key legacy `ssh.KeyAlgoDSA` (`"ssh-dss"`) ke dalam profil kriptografi SSH untuk mendukung OpenSSH 4.3p2 (CentOS 5) bersama `ssh-rsa` dan algoritma modern.

6. **Pengerasan Status Keaslian Sistem (`IsFresh`)**:
   - `IsFresh()` memeriksa apakah `admin_password_hash` telah diubah dari password default `"admin"`. Jika password telah dikonfigurasi atau penanda `bootstrap_completed` aktif, sistem dianggap tidak segar dan impor snapshot tanpa autentikasi otomatis ditolak (HTTP 401).

## Konsekuensi

- **Keamanan Komprehensif**: Menutup 8 temuan kerentanan audit tanpa menyisakan celah command injection atau akses tak berwenang.
- **Kompatibilitas 100%**: Server warisan (CentOS 5/6) tetap dapat dihubungkan, dipantau, dan dikelola tanpa kendala negosiasi cipher atau kegagalan shell POSIX.
- **Dukungan Plain HTTP Utuh**: Pengguna internal yang mengakses via IP LAN tanpa TLS tidak terganggu; login dan sesi tetap stabil dan aman.
- **Zero-Dependency**: Seluruh perbaikan memanfaatkan Go standard library (`crypto/rand`, `encoding/hex`, `net/url`, `regexp`).
