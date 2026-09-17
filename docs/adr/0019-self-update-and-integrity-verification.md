# 0019. Self-Update Semi-Otomatis dan Verifikasi Integritas Binary

Pantau mendukung fitur deteksi versi rilis baru (Update Checker) dan pembaruan mandiri satu-klik (Self-Update) dengan verifikasi checksum SHA-256 dan penggantian biner in-place yang aman lintas platform (Linux dan Windows).

## Konteks dan Masalah

Pantau didistribusikan sebagai biner tunggal mandiri (single binary) tanpa manajer paket sistem operasi (seperti APT, RPM, atau Homebrew). Tanpa mekanisme pembaruan internal:
1. Administrator harus secara manual memantau repositori GitHub untuk mengetahui adanya rilis perbaikan bug atau fitur baru.
2. Proses pembaruan manual (mengunduh arsip dari GitHub Releases, mengekstrak, menghentikan server, menimpa biner lama, dan menjalankan ulang) menimbulkan beban operasional berulang dan berisiko salah menempatkan arsitektur biner yang tidak kompatibel.
3. Mengganti biner yang sedang berjalan (*in-place binary replacement*) memiliki tantangan spesifik platform:
   - Pada Linux: file yang sedang dieksekusi dapat di-unlink/ditimpa secara atomik, namun proses yang berjalan tetap memegang inode lama sampai di-restart.
   - Pada Windows: file `.exe` yang sedang berjalan di-*lock* oleh sistem operasi sehingga operasi penulisan langsung (`write`/`overwrite`) akan ditolak dengan galat izin (*permission denied*). Namun, Windows mengizinkan biner yang sedang berjalan untuk di-*rename*.
4. Pada lingkungan Docker, menimpa file biner di dalam container adalah *anti-pattern* dan perubahan biner akan hilang ketika container di-redeploy/recreate.
5. Pada lingkungan terisolasi (*airgapped*) sesuai [ADR 0013](0013-airgapped-self-contained-web-assets.md), pengecekan pembaruan tidak boleh memblokir startup atau menghasilkan kegagalan fatal saat tidak ada akses internet publik ke GitHub API.

## Keputusan Arsitektur

1. **Model Operasi: Semi-Otomatis (One-Click) dengan Kendali Penuh Administrator**:
   - Pengecekan versi dilakukan secara periodik di latar belakang (background ticker 12 jam dengan cache 6 jam) atau on-demand saat administrator menekan tombol "Periksa Pembaruan" di antarmuka web.
   - Pantau **tidak melakukan pembaruan latar belakang tanpa konfirmasi** (*no silent background update*). Pembaruan biner hanya dieksekusi saat administrator mengonfirmasi tindakan tersebut secara eksplisit di antarmuka web atau baris perintah (`pantau -update`).

2. **Diferensiasi Lingkungan Standalone vs Docker**:
   - Sistem mendeteksi keberadaan file penanda container (`/.dockerenv` atau konfigurasi cgroup).
   - Pada lingkungan Docker: tombol Self-Update biner dinonaktifkan. Antarmuka hanya menampilkan pemberitahuan ketersediaan rilis baru dan menyajikan panduan penarikan image resmi (`docker compose pull && docker compose up -d`).
   - Pada lingkungan Standalone (Linux biner & Windows portable): proses pengunduhan dan penggantian biner aktif diizinkan.

3. **Verifikasi Integritas SHA-256 dan Pre-flight Smoke Test**:
   - Alur kerja rilis GitHub Actions menghasilkan manifest `checksums.txt` resmi berisi nilai hash SHA-256 untuk setiap paket distribusi.
   - Sebelum biner diekstrak dan dipasang, Pantau memvalidasi hash paket rilis yang diunduh terhadap manifest resmi.
   - Setelah ekstraksi ke biner sementara (`pantau.tmp`), Pantau menjalankan *pre-flight smoke test* dengan mengeksekusi `pantau.tmp -v`. Jika biner gagal dijalankan (misal: arsitektur tidak kompatibel atau segmentation fault), operasi update dibatalkan seketika tanpa menyentuh biner aktif.

4. **Atomic Rename Swap Lintas Platform**:
   - **Linux**: Menggunakan atomic rename (`os.Rename`) dari biner sementara ke jalur eksekutabel aktif.
   - **Windows**: Mengubah nama biner aktif yang sedang terkunci (`pantau.exe` -> `pantau.exe.old`), kemudian memindahkan biner baru menjadi `pantau.exe`. File usang (`pantau.exe.old`) dibersihkan secara otomatis pada inisialisasi server berikutnya.

5. **Graceful Handover dan Restart**:
   - Setelah biner baru siap, antarmuka menyajikan konfirmasi akhir "Restart Pantau Sekarang".
   - Saat dikonfirmasi, server menutup koneksi HTTP dan koneksi basis data SQLite secara aman (*graceful shutdown*), mengeksekusi biner baru dengan argumen baris perintah (`os.Args`) serta direktori kerja yang sama, lalu menghentikan proses lama. Antarmuka web secara otomatis melakukan rekoneksi dan memuat ulang halaman.

6. **Toleransi Jaringan Airgapped (Fail-Silent)**:
   - Permintaan ke GitHub API dibatasi dengan batas waktu ketat (3 detik). Jika server tidak memiliki akses internet atau terkena pembatasan kuota (rate limit) GitHub, Update Checker mencatat status kegagalan secara senyap (*fail-silent*) tanpa memblokir fungsi utama atau menampilkan popup peringatan yang mengganggu.
   - Pengecekan pembaruan otomatis dapat dinonaktifkan sepenuhnya melalui opsi Pengaturan atau flag `-disable-update-check`.
