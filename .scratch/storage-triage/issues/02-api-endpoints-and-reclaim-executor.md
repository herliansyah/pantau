# 02 - API Endpoints and Reclaim Executor

Status: resolved

Implementasi endpoint REST API di `internal/web`:
- `GET /api/hosts/{id}/storage-triage`: Memicu dan mengembalikan hasil diagnosa Storage Triage.
- `POST /api/hosts/{id}/storage-reclaim`: Mengeksekusi aksi pembersihan aman terkurasi (journal vacuum, package cache clean, rotated logs prune, log truncation, service reload) via SSH dengan validasi perintah ketat.

## Resolution
- Ditambahkan rute `storage-triage` dan `storage-reclaim` ke dalam `handleHostDetailRoute` di `internal/web/server.go`.
- Ditambahkan method `SetStorageTriageService` pada `Server`.
- Ditambahkan unit test di `internal/web/storage_triage_test.go` memverifikasi rejection untuk path proteksi (`/etc/passwd`), injeksi perintah shell berbahaya pada nama unit systemd, dan kode respons HTTP.
