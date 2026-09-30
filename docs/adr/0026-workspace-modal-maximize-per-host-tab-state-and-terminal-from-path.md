# Workspace Modal Maximize, Per-Host Tab State, dan Terminal From Path

Penerapan mode CSS full-viewport pada Workspace Modal (Host Detail), isolasi state navigasi tab per host berbasis browser session, dan integrasi peluncuran terminal kontekstual dari SFTP File Manager.

## Konteks & Masalah

1. **Keterbatasan Ruang Kerja Workspace Modal**: Dimensi default Workspace Modal (`75vw × 80vh` per ADR 0007) ideal untuk mencegah *cumulative layout shift*, tetapi pada monitor resolusi tinggi atau saat inspeksi berkas intensif (SFTP File Manager) dan penelaahan log panjang, administrator memerlukan opsi tampilan layar penuh (100% viewport) tanpa terganggu oleh margin atau backdrop modal.
2. **Kebocoran State Tab Antar-Host**: Sebelumnya, state tab aktif (`currentTab`) disimpan pada variabel global tunggal. Ketika administrator membuka Host A dan berpindah ke tab *File Manager*, lalu menutupnya dan membuka Host B, Host B langsung membuka tab *File Manager* alih-alih memulai dari tab *Overview*. Hal ini mengganggu alur kerja umum di mana administrator mengekspektasikan gambaran umum kesehatan sistem saat memeriksa server baru.
3. **Keterputusan Alur SFTP ke Shell**: Saat menginspeksi direktori atau berkas konfigurasi pada SFTP File Manager, administrator sering kali perlu segera mengeksekusi perintah terminal pada direktori tersebut. Membuka terminal secara manual lalu mengetik ulang path direktori remote memperlambat operasional dan rentan kesalahan ketik (*typo*).

## Keputusan Arsitektur

1. **Workspace Modal Maximize (`100vw × 100vh`)**:
   - Menerapkan mode CSS Full-Viewport (`position: fixed; inset: 0; width: 100vw; height: 100vh; border-radius: 0; padding: 0;`) yang konsisten dengan ADR 0011 (Terminal Maximize), alih-alih API `requestFullscreen()` bawaan peramban.
   - Menyediakan tombol toggle pembesaran (`⛶` / `🗗`) di samping tombol tutup (`✕`) pada baris header modal, serta dukungan interaksi *double-click* pada header.
   - Status maximize disimpan pada `localStorage` (`pantau_host_detail_maximized`) untuk mempertahankan preferensi pengguna sepanjang sesi tanpa memaksa pengguna mengklik maximize berulang kali.
   - Menjaga integritas tumpukan modal (`openModalStack` per ADR 0007) sehingga Action Dialog anak (seperti dialog konfirmasi dan edit catatan) tetap tersusun di atas modal yang sedang maximized.

2. **Per-Host Tab & Path State Isolation**:
   - Menggantikan variabel tab global tunggal dengan penyimpanan berbasis sesi (`sessionStorage`) terisolasi per Host ID (`pantau_host_tab_<id>` dan `pantau_host_files_path_<id>`).
   - Setiap Host yang belum pernah dibuka dalam sesi peramban selalu dimulai dari tab default `overview`.
   - Host yang telah dikunjungi akan mengingat tab dan path direktori SFTP terakhirnya tanpa mempengaruhi state host lain.
   - Pemilihan `sessionStorage` (alih-alih `localStorage`) menjamin state tab tidak bocor saat membuka host baru, bertahan saat halaman di-reload (F5), dan otomatis bersih saat tab peramban ditutup.

3. **Terminal From Path**:
   - Menambahkan tombol aksi terminal (`💻`) pada setiap baris direktori/berkas di tabel SFTP File Manager dan tombol `💻 Open Terminal Here` pada breadcrumb toolbar atas.
   - Untuk baris berkas, target path secara otomatis diarahkan ke direktori induk yang memuat berkas tersebut.
   - Peluncuran terminal selalu membuka **Terminal Tab baru** (`forceNew = true`) di Global Multi-Tab Terminal Dock untuk mencegah interupsi proses interaktif (seperti `vim` atau `top`) yang sedang berjalan di tab lain.
   - Endpoint backend `/ws/terminal` diperluas untuk menerima parameter query `&dir=<path>`. Setelah bridge SSH terhubung, backend secara otomatis mengirim perintah `cd '<escaped_dir>'\n` dengan *escaping* POSIX yang aman.
   - Tab terminal baru diberi judul kontekstual berbasis direktori (`<HostName>: <NamaFolder>`) dan metadata direktori disimpan ke penyimpanan lokal untuk pemulihan sesi.
