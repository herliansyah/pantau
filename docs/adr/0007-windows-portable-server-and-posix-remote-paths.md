# Windows Portable Server dan Separasi POSIX Remote Path

Pantau master server mendukung eksekusi mandiri (portable CLI binary) pada sistem operasi Windows tanpa agen latar belakang, dengan cakupan pemantauan tetap dibatasi secara eksklusif pada target Host berbasis Linux via SSH.

Untuk menjamin portabilitas tanpa merusak integritas operasi remote di target Linux, manipulasi jalur file dibagi secara ketat: paket `path` (standar POSIX `/`) wajib digunakan untuk semua jalur remote (SFTP, streaming transfer, direktori remote), sedangkan paket `filepath` hanya digunakan untuk jalur file lokal pada disk master (seperti basis data SQLite). Basis data lokal secara default tetap disimpan pada direktori kerja aktif (`./pantau.db`) guna mempertahankan portabilitas zero-ops.

Saat binary dijalankan di Windows tanpa argumen tambahan, server langsung berjalan dengan nilai default (`port=8080`, `db=pantau.db`) dan secara otomatis membuka peramban web bawaan ke `http://localhost:8080`. Perilaku pembukaan peramban ini dapat dinonaktifkan melalui flag `-open=false` untuk kebutuhan otomasi atau eksekusi latar belakang.
