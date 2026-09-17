# Encrypted System Snapshot dan GitHub Sync

Pencadangan (backup) dan pemulihan (restore) status sistem Pantau dilakukan menggunakan arsip terenkripsi AES-256-GCM (System Snapshot) yang dikirim ke repositori GitHub privat melalui GitHub REST API (HTTPS + Personal Access Token) tanpa dependensi binary git lokal.

Keputusan ini diambil agar pengguna dapat memulihkan seluruh konfigurasi Host, kredensial SSH, aturan Desired State, dan pengaturan aplikasi secara instan di mesin baru hanya dengan satu file lokal atau kredensial GitHub. System Snapshot hanya memuat konfigurasi vital dengan mengecualikan riwayat log/inspeksi lama untuk menjaga ukuran file tetap ringkas (< 1MB) dan transfer instan. Seluruh data sensitif wajib terenkripsi menggunakan derivasi frasa sandi (Snapshot Passphrase) sebelum meninggalkan mesin lokal.

## Isolasi Jalur File Multi-Server (Multi-Instance Deployment)

Ketika administrator mengoperasikan lebih dari satu instance server Pantau independen (misalnya server kantor dan server home lab) dengan satu repositori GitHub yang sama, setiap instance wajib mengonfigurasi jalur file repositori (`github_file_path`) yang berbeda (contoh: `snapshots/kantor.enc` dan `snapshots/homelab.enc`). Hal ini mencegah terjadinya tabrakan penimpaan data (*Last-Write-Wins overwrite*) pada saat backup terjadwal berkala dieksekusi, sekaligus memungkinkan pemulihan (Disaster Recovery) dilakukan secara spesifik per-lingkungan tanpa memerlukan repositori GitHub terpisah.

