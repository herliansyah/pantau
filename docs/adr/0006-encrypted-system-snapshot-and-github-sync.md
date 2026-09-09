# Encrypted System Snapshot dan GitHub Sync

Pencadangan (backup) dan pemulihan (restore) status sistem Pantau dilakukan menggunakan arsip terenkripsi AES-256-GCM (System Snapshot) yang dikirim ke repositori GitHub privat melalui GitHub REST API (HTTPS + Personal Access Token) tanpa dependensi binary git lokal.

Keputusan ini diambil agar pengguna dapat memulihkan seluruh konfigurasi Host, kredensial SSH, aturan Desired State, dan pengaturan aplikasi secara instan di mesin baru hanya dengan satu file lokal atau kredensial GitHub. System Snapshot hanya memuat konfigurasi vital dengan mengecualikan riwayat log/inspeksi lama untuk menjaga ukuran file tetap ringkas (< 1MB) dan transfer instan. Seluruh data sensitif wajib terenkripsi menggunakan derivasi frasa sandi (Snapshot Passphrase) sebelum meninggalkan mesin lokal.
