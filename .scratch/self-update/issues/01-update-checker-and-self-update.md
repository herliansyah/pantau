# Issue 01: Implementasi Update Checker dan Self-Update

Status: resolved

## Deskripsi
Implementasikan fitur Update Checker dan Self-Update semi-otomatis pada Pantau sesuai keputusan ADR 0019 dan spesifikasi di `.scratch/self-update/spec.md`.

## Kriteria Penerimaan
- [x] Deteksi rilis terbaru dari GitHub API dengan caching TTL 6 jam dan background check 12 jam.
- [x] UI Dashboard / Footer menampilkan badge status pembaruan jika versi baru tersedia.
- [x] Tombol manual "Periksa Pembaruan" dan "Perbarui Sekarang" di menu Settings / About.
- [x] Deteksi otomatis runtime container (Docker); jika di dalam Docker, sembunyikan/nonaktifkan tombol update biner dan tampilkan panduan `docker compose pull`.
- [x] Toleransi jaringan airgapped: fail-silent saat GitHub tidak dapat diakses atau timeout (3s).
- [x] Verifikasi checksum SHA-256 arsip terhadap `checksums.txt` rilis resmi sebelum ekstraksi.
- [x] Pre-flight validation dengan eksekusi `pantau.tmp -v` sebelum penimpaan file biner aktif.
- [x] Penggantian biner aman lintas platform (atomic rename di Linux, rename-to-old di Windows).
- [x] Graceful process handover dan rekoneksi otomatis pada antarmuka web.
- [x] Opsi CLI flag `-check-update`, `-update`, dan `-disable-update-check`.
- [x] Workflow GitHub Actions (`release.yml`) menyertakan pembuatan manifest `checksums.txt`.

## Comments
- Sesi perancangan disepakati melalui `/grill-with-docs` dengan prinsip lazy senior dev (ADR 0019).
- Implementasi tuntas pada package `internal/updater`, handler API di `internal/web/server.go`, UI di `internal/web/static/index.html`, dan CLI flags di `main.go`. Seluruh unit test lulus 100%.
