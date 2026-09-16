# Airgapped TOTP Two-Factor Authentication dan Emergency Recovery

Penerapan autentikasi dua faktor (2FA) berbasis Time-based One-Time Password (TOTP, RFC 6238) pada sistem login Pantau, penyajian QR code mandiri tanpa dependensi eksternal, mitigasi brute force, dan mekanisme pemulihan darurat (*emergency recovery*).

## Konteks dan Masalah

Sebelumnya, otentikasi Pantau hanya mengandalkan satu kata sandi administrator (`admin_password_hash`) yang disimpan dalam basis data SQLite lokal. Jika kata sandi tersebut bocor atau dieksploitasi melalui serangan *credential stuffing*, penyerang langsung memperoleh kendali penuh atas infrastruktur Host yang terhubung.

Karakteristik arsitektur Pantau:
1. **Single-Binary & Airgapped Compliance** ([ADR 0002](0002-golang-single-binary-sqlite.md) & [ADR 0013](0013-airgapped-self-contained-web-assets.md)): Pantau beroperasi di jaringan privat/terisolasi tanpa ketergantungan API pihak ketiga (tidak dapat mengandalkan layanan SMS, email gateway eksternal, atau CDN publik untuk render QR code).
2. **Single-Admin Model**: Pantau tidak menggunakan sistem multi-user; hanya ada satu sesi administrator aktif.
3. **Risiko Lockout**: Pada aplikasi self-hosted, jika perangkat autentikasi administrator rusak atau hilang, administrator dapat terkunci permanen dari sistemnya sendiri jika tidak ada jalan keluar pemulihan.

## Keputusan Arsitektur

1. **Protokol TOTP Murni (RFC 6238)**:
   - Menggunakan algoritma Time-based One-Time Password (TOTP) standar 6-digit dengan interval waktu 30 detik dan HMAC-SHA1.
   - Bersifat 100% luring (*offline*); tidak memerlukan sambungan keluar ke server autentikasi pihak ketiga.
   - Sifat aktivasi adalah opsional (*opt-in*), dikonfigurasi melalui Action Dialog Settings oleh administrator.

2. **Self-Contained Local QR Code & Secret Key Display**:
   - Backend Pantau menyajikan kode pairing langsung dalam bentuk vector SVG murni atau Base32 Secret Key teks tanpa memanggil layanan eksternal (seperti Google Charts API).
   - Menjaga kepatuhan penuh terhadap prinsip lingkungan terisolasi (*airgapped*).

3. **Alur Login Dua Langkah (Two-Step Challenge Flow)**:
   - Langkah 1: Verifikasi kata sandi utama. Jika benar dan 2FA aktif, server menerbitkan token sementara (`temp_token`) dalam memori berdurasi 5 menit.
   - Langkah 2: Antarmuka beralih ke input 6-digit TOTP (atau link Recovery Code). Setelah token divalidasi, cookie resmi `pantau_session` diterbitkan.

4. **Toleransi Desinkronisasi Waktu dan Anti-Replay**:
   - Toleransi jam server dan ponsel ditetapkan sebesar ±1 langkah (±30 detik), mencakup jendela 90 detik ($T_{-1}, T_0, T_{+1}$).
   - Pencatatan langkah waktu terakhir yang berhasil (`last_totp_step`) untuk mencegah serangan *token replay* pada sisa detik interval yang sama.

5. **Mitigasi Brute Force**:
   - Setiap `temp_token` dibatasi maksimal 5 kali percobaan verifikasi kode gagal.
   - Jika batas 5 kali terlampaui, token sementara langsung dimusnahkan dan pengguna dipaksa mengulang autentikasi dari kata sandi awal.

6. **Mekanisme Pemulihan Bertingkat (Dual-Channel Emergency Recovery)**:
   - **Web Recovery Codes**: Saat 2FA diaktifkan, sistem menghasilkan 8 kode pemulihan acak satu-kali-pakai. Kode disimpan di basis data dalam bentuk *hash* SHA-256 dan langsung dihapus saat berhasil digunakan.
   - **CLI Bypass Flag (`-disable-2fa`)**: Argumen baris perintah darurat pada binary Pantau untuk menonaktifkan 2FA secara instan bagi administrator yang memiliki akses shell ke host master.

7. **Siklus Hidup Terintegrasi System Snapshot**:
   - Status 2FA dan hash kode pemulihan tersimpan dalam tabel `settings` sehingga otomatis terlindungi dalam arsip terenkripsi AES-256-GCM pada System Snapshot ([ADR 0006](0006-encrypted-system-snapshot-and-github-sync.md)).
