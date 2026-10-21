# 04 — Integrasi UI Port Forwarding & Quick-Access

Type: grilling
Status: resolved
Blocked by: 01, 02

## Question

Bagaimana desain antarmuka pengguna untuk mengelola port yang sedang di-forward (daftar port aktif di tab Terminal/Host Detail, indikator status, tombol quick-open browser, dan modal konfirmasi saat URL localhost diklik di xterm)?

## Answer

1. **Modal Konfirmasi Link Terminal (Interaktif)**:
   Saat pengguna menekan `Ctrl+Click` (atau `Cmd+Click`) pada teks URL `localhost:port` di xterm terminal:
   - Muncul dialog modal ringkas:
     - Header: 🌐 **Buka Web Service Remote**
     - Keterangan: Nama Host target, Port remote (misal `3000` / `8080`), dan Path URL tujuan.
     - Aksi Utama: Tombol **"Buka via Tunnel"** yang membuka `/api/tunnel/{hostId}/{port}/{path}` di tab browser baru.
     - Aksi Sekunder: Tombol **"Salin Link Tunnel"** dan tombol **"Batal"**.
2. **Menu Quick-Access "Ports" di Toolbar Terminal**:
   - Ditambahkan tombol `🌐 Ports` di toolbar atas terminal (bersebelahan dengan tombol *File Manager* dan dropdown *Pins*).
   - Menampilkan dropdown dengan:
     - Input manual port number (1-65535) + tombol **"Buka"**.
     - Daftar riwayat port yang baru saja dibuka untuk host tersebut agar pengguna dapat membuka kembali layanan web remote tanpa harus mengetik ulang nomor port.
