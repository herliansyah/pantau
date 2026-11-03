# Spec: Storage Triage & Runaway Detection

## Problem

Server Linux kerap mengalami insiden kehabisan ruang disk (*disk saturation* 90–100%). Administrator sering kesulitan mengetahui direktori atau berkas mana yang menghabiskan ruang penyimpanan, berkas apa yang sedang mengalami akselerasi pembengkakan ukuran (*Runaway File*), berkas terhapus yang masih tertahan oleh proses aktif (*Ghost File*), serta ragu membedakan mana berkas sampah/cache yang aman dibersihkan (*Reclaimable Storage*) vs data krusial sistem.

## Goals

1. **Storage Triage On-Demand**: Modul investigasi berbasis SSH tanpa agen untuk menganalisis partisi root/mount point target dengan batas waktu maksimal 20 detik dan proteksi *Triage Concurrency Guard*.
2. **Tiered Discovery**:
   - Direktori terbesar tingkat pertama (*Space Hog*) via `du -kx --max-depth=1 /`.
   - 15 Berkas terbesar di atas 50MB.
   - Deteksi *Runaway Files* (berkas berukuran besar yang termodifikasi dalam 60 menit terakhir) dikorelasikan dengan `Disk I/O Write Rate`.
   - Deteksi *Ghost Files* (berkas berstatus `(deleted)` yang masih di-hold *file descriptor* proses aktif via `lsof` atau `/proc/*/fd`).
3. **Katalog Safe Reclaim & Log Truncation**:
   - Katalog pembersihan aman terkurasi: Systemd Journal vacuum, Package Cache clean (APT, DNF, Pacman, APK), Rotated Logs (`/var/log/*.gz`, `*.1`), Core Dumps, dan Docker Prune.
   - Prosedur aman *Log Truncation* (`truncate -s 0 <path>`) alih-alih `rm` untuk mengosongkan log aktif secara in-place.
   - Graceful reload atau preskriptif command untuk *Ghost Files*.
4. **Transparansi Perintah Shell**:
   - Menampilkan perintah shell riil sebelum eksekusi pembersihan melalui Confirmation Dialog.
5. **Antarmuka UI Terpadu**:
   - Tab baru `Storage Triage` di Workspace Modal Host Detail.
   - Quick deep-link dari Alert Banner dan kartu Host dashboard saat disk > 85%.

## Architecture & Constraints

- Mengikuti ADR [0039-agentless-storage-triage-runaway-detection-and-safe-reclaim.md](file:///home/ian/orca/workspaces/pantau/cetacean/docs/adr/0039-agentless-storage-triage-runaway-detection-and-safe-reclaim.md).
- Single-binary, 0 external runtime dependencies, Go standard library.
- Eksklusi mutlak direktori `/tmp` dari penghapusan otomatis.
