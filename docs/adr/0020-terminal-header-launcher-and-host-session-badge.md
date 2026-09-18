# 0020. Terminal Header Launcher, Eliminasi Redundansi Modal Tab, dan Host Session Badge

Penyempurnaan arsitektur akses antarmuka terminal Pantau melalui penyederhanaan titik akses global pada header utama, penghapusan tab terminal redundan di dalam modal observabilitas, dan penambahan badge sesi shell reaktif pada kartu host.

## Konteks dan Masalah

Pasca penerapan Global Multi-Tab Terminal Dock ([ADR 0015](0015-global-multi-tab-terminal-dock-and-lifecycle.md)), antarmuka terminal dipisahkan dari modal host menjadi dock multi-tab global. Namun, beberapa artefak desain lama menimbulkan redundansi operasional dan glitch visual:

1. **Tab Kosong Redundan pada Workspace Modal**:
   Tab "Terminal" pada navigasi tab Host Detail (`hostDetailTabs`) tidak lagi me-render shell di dalam modal, melainkan hanya placeholder statis yang memicu pembukaan dock global. Selain itu, status tab aktif modal (`currentTab`) yang tertahan pada `'terminal'` menyebabkan pembukaan detail host berikutnya langsung memunculkan dock terminal secara tidak sengaja.
2. **Ketiadaan Launcher Global Sebelum Sesi Aktif**:
   Satu-satunya cara membuka dock terminal adalah melalui tombol pada kartu host atau tab detail. Pengguna tidak memiliki akses global dari navigasi atas sebelum sesi terminal dibuat.
3. **Redundansi dan Clutter dari Floating Session Pill**:
   Mekanisme dock minimize sebelumnya mengandalkan floating pill di pojok kanan bawah (`terminalMinimizedPill`) yang menutupi konten kartu host terbawah dan elemen footer dashboard.
4. **Ketiadaan Indikator Sesi per Host**:
   Administrator yang mengelola banyak server tidak dapat mengetahui sekilas dari dashboard apakah sebuah host sedang memiliki sesi shell yang berjalan atau belum, sehingga berisiko membuka sesi ganda atau lupa menutup proses remote.

## Keputusan Arsitektur

1. **Eliminasi Tab Terminal dan Penegasan Modal Observabilitas Murni**:
   - Tab "Terminal" dihapus sepenuhnya dari daftar tab `hostDetailTabs`. Modal difokuskan murni untuk penyajian data observabilitas (*Overview, Network & Security, Desired State, Docker, Incident Timeline, File Manager*).
   - Tombol terminal pada modal di samping tombol tutup `✕` dieliminasi guna menghindari salah klik (*accidental click*) dan redundansi dengan kartu host serta header global.
   - Tombol "Run Immediate Inspection" yang sebelumnya berada terisolasi di bagian bawah tab Overview dihapus, dan direlokasi menjadi ikon putar kompak (`btn-inspect-quick`) tepat di samping badge status host pada header modal. Hal ini menyelaraskan pola UI dengan kartu host di dashboard dan memungkinkan inspeksi ulang langsung dari tab mana saja.
   - Variabel state `currentTab` di-reset ke `'overview'` setiap kali modal Host Detail dibuka jika tab sebelumnya tidak valid.

2. **Pusat Akses Global: Terminal Header Launcher**:
   - Menambahkan tombol aksi `btnHeaderTerminal` pada `<header>` utama dashboard sejajar dengan Cross-Host Transfers dan Global Notes.
   - Dilengkapi badge reaktif `activeTerminalBadge` yang menampilkan akumulasi sesi terminal yang sedang aktif di seluruh sistem.
   - Berperan sebagai kontrol toggle cerdas:
     - Jika dock terbuka: me-minimize/menyembunyikan dock.
     - Jika dock tertutup/minimized dan memiliki sesi aktif: me-restore dock serta memfokuskan tab terminal yang aktif.
     - Jika dock kosong (0 sesi): membuka dock dan langsung memunculkan menu dropdown host picker (`➕ New Tab`) untuk mempermudah pemilihan target server.

3. **Penghapusan Total Floating Session Pill (Zero-Clutter)**:
   - Elemen DOM `#terminalMinimizedPill` dan seluruh aturan gaya CSS-nya dihapus permanen.
   - Header utama mengambil alih seluruh fungsi monitoring status dock yang diminimalkan, menciptakan tampilan dashboard yang bersih dan konsisten tanpa widget mengambang.

4. **Reactive Host Session Badge pada Kartu Server**:
   - Tombol terminal pada kartu host (Grid View) dan baris tabel (List View) kini menyematkan elemen badge numerik (`host-terminal-badge`).
   - Prinsip *high signal-to-noise ratio*: badge disembunyikan saat jumlah sesi aktif adalah 0 (hanya menampilkan ikon `💻` polos), dan muncul dengan aksen warna saat terdapat 1 atau lebih sesi shell aktif untuk host tersebut.
   - Status badge di-update secara reaktif saat tab terminal baru dibuka, ditutup, atau dipulihkan dari `localStorage`.
