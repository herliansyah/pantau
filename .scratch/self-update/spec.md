# Spec: Update Checker & Self-Update

## Ringkasan Fitur
Menyediakan mekanisme deteksi versi rilis terbaru secara periodik dan aman (Update Checker) serta alur pembaruan mandiri satu-klik (Self-Update) untuk instalasi Pantau biner standalone di Linux dan Windows, dengan verifikasi integritas SHA-256 dan perlakuan khusus pada lingkungan Docker dan airgapped.

## Arsitektur & Keputusan Desain
Mengacu pada [ADR 0019](../../docs/adr/0019-self-update-and-integrity-verification.md):
1. **Pengecekan Versi (Update Checker)**:
   - Mengakses GitHub Releases API (`https://api.github.com/repos/herliansyah/pantau/releases/latest`).
   - Berjalan pada inisialisasi server dan background worker setiap 12 jam (TTL cache 6 jam).
   - Non-blocking dan fail-silent (timeout 3s) untuk mendukung lingkungan airgapped.
2. **Lingkungan Deployment**:
   - Standalone binary (Linux / Windows): Izinkan tombol One-Click Update.
   - Docker container: Nonaktifkan tombol update biner, tampilkan panduan `docker compose pull`.
3. **Validasi & Verifikasi Integritas**:
   - Unduh arsip rilis sesuai `runtime.GOOS` dan `runtime.GOARCH`.
   - Validasi SHA-256 terhadap `checksums.txt` dari GitHub Release.
   - Pre-flight smoke test: jalankan `pantau.tmp -v` untuk memverifikasi kompatibilitas biner sebelum ditimpa.
4. **Penggantian Biner (In-Place Swap)**:
   - Linux: `os.Rename(temp, target)`.
   - Windows: `os.Rename(target, target + ".old")`, lalu pindahkan biner baru.
5. **Graceful Handover / Restart**:
   - Konfirmasi eksplisit pengguna untuk restart.
   - Graceful shutdown server & spawn proses baru dengan argumen yang sama (`os.Args`).
