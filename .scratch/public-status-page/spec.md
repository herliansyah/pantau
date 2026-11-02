# Specification: Public Status Page

Status: ready

## Overview

Public Status Page adalah fitur halaman publik mandiri tanpa autentikasi yang dapat diakses melalui path `/status` pada server Pantau. Fitur ini dirancang untuk menyajikan transparansi kesehatan operasional sistem (ketersediaan Host dan Desired Rule yang di-opt-in), visual bar historis ketersediaan (uptime) 90 hari, serta pengumuman insiden dan pemeliharaan resmi langsung kepada pengguna atau klien eksternal tanpa mengekspos kredensial, alamat IP privat, atau antarmuka administratif Pantau.

## Referensi Keputusan & Peta Tiket

Seluruh keputusan arsitektur dan spesifikasi implementasi telah disepakati dan dikunci pada [Map: Public Status Page](map.md):
- [01 — Model Data Status Target & Skema Agregasi Rollup](issues/01-target-opt-in-and-data-model.md)
- [02 — Pekerja Agregasi Daily Uptime Rollup & Status Resolution Engine](issues/02-daily-uptime-rollup-worker-and-aggregation.md)
- [03 — Endpoint API Publik & Batas Keamanan Zero-Trust](issues/03-public-status-api-and-zero-trust-boundary.md) ([Laporan Riset](../../docs/research/03-public-status-api-and-zero-trust-boundary.md))
- [04 — Desain Antarmuka Publik `/status` & Visual Bar Uptime 90 Hari](issues/04-public-status-page-ui-ux-and-90d-uptime-bars.md) ([Prototipe HTML](prototype.html))
- [05 — Antarmuka Admin Incident Publisher & Manajemen Status Target](issues/05-admin-incident-publisher-and-status-settings-ui.md)

## Arsitektur Inti

1. **Batas Keamanan Zero-Trust**:
   - Disajikan di path `/status` dengan middleware pengamanan yang sepenuhnya mengabaikan cookie/header sesi admin.
   - Proyeksi data yang dikirim melalui `/api/status-page/summary` menerapkan *data masking* mutlak: hanya mengekspos `public_name`, status kualitatif (`operational`, `degraded`, `outage`), dan agregasi uptime persentase. Tidak ada alamat IP, port SSH, username, path direktori, atau log mentah yang dikirim.
   - In-memory response caching (snapshot diperbarui setiap ada siklus inspeksi baru atau invalidasi manual) dan ETag support untuk menangani lonjakan trafik publik saat terjadi downtime.

2. **Model Status Target**:
   - Status Target adalah entitas opt-in eksplisit (Host atau Desired Rule).
   - Setiap target memiliki `public_name`, `group_name` (kategori grup tampilan, misal "Core Services", "Public Websites"), dan urutan tampilan (*display order*).

3. **Daily Uptime Rollup (90 Hari)**:
   - Tabel `status_page_daily_rollups` menyimpan ringkasan harian per target: total pemeriksaan, jumlah sukses, dan jumlah deviasi/gagal.
   - Data historis 90 hari dirender menggunakan 90 batang visual interaktif (SVG/CSS murni, zero-dependency) dengan tooltip status ketersediaan harian.

4. **Pengumuman Insiden (Status Incident)**:
   - Administrator dapat membuat catatan insiden: judul, tingkat keparahan (*investigating*, *identified*, *monitoring*, *resolved*), pesan penjelasan Markdown ringkas, serta daftar Status Target terdampak.
   - Status Incident yang aktif (*unresolved*) disematkan di bagian atas halaman publik.

5. **Kustomisasi & Branding Airgapped**:
   - Pengaturan global: toggle Status Page aktif/nonaktif, nama organisasi/layanan, logo/favicon, deskripsi header, tautan bantuan/support.
   - Sepenuhnya self-contained (zero-CDN), responsive, dan adaptif light/dark mode.
