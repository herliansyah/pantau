# 0026. Sub-Header Workspace Shelf, Flat Workspace Sessions, dan Smart Resume

Pengembangan arsitektur persistensi konteks kerja dan manajemen sesi modal Pantau melalui Sub-Header Workspace Shelf adaptif, pemulihan otomatis (Smart Resume), dan retensi draf editor in-memory.

## Konteks dan Masalah

Administrator server sering melakukan penelusuran mendalam pada host tertentu—seperti membuka SFTP File Manager hingga direktori spesifik (`/var/log/nginx` atau `/etc/systemd/system`) dan membuka berkas konfigurasi di Code Editor. Saat administrator perlu melirik host lain atau mengecek dashboard:

1. **Kehilangan Konteks Navigasi (Kerja Dua Kali)**:
   Menutup Workspace Modal Host Detail menyebabkan `currentHost` berganti dan status navigasi direktori ter-reset. Saat kembali ke host pertama, administrator harus mencari ulang folder dan berkas dari awal.
2. **Ketiadaan Mode Penangguhan (Minimize)**:
   Modal Host Detail dan Code Editor hanya memiliki tombol tutup (`✕`). Tidak ada mekanisme untuk melipat modal sementara tanpa mematikan alur kerja.
3. **Risiko Polusi Visual Floating Dock**:
   Menempatkan bilah dok atau tombol mengambang di pojok bawah layar telah ditolak pada [ADR 0020](0020-terminal-header-launcher-and-host-session-badge.md) karena menutupi kartu host terbawah dan footer dashboard, serta menciptakan inkonsistensi tata letak di mana kontrol terminal berada di header atas sementara dok modal berada di bawah.

## Keputusan Arsitektur

1. **Sub-Header Workspace Shelf (Zero-Clutter Flow Layout)**:
   - Menempatkan bilah rak horizontal ramping (tinggi ~32px) tepat di bawah navbar header utama.
   - Bilah ini bersifat dinamis adaptif: hanya muncul (*display: flex*) jika terdapat minimal 1 Workspace Session yang diminimalkan, dan tersembunyi sepenuhnya saat sesi kosong.
   - Menggunakan alur layout natural (*flow layout*), bukan elemen melayang (*fixed floating overlay*), sehingga tidak pernah menutupi kartu server atau pagination dashboard.
   - Menampilkan chip sesi interaktif dengan ikon jenis sesi, nama host, path direktori / berkas, dan tombol tutup cepat:
     `[🖥️ srv-prod: /var/log/nginx] [📝 srv-db: my.cnf *] [✕]`
   - Satu klik pada chip langsung memulihkan (*restore*) modal ke viewport peramban.

2. **Flat Workspace Sessions & Concurrency Ceiling**:
   - Status modal Host Detail dan Code Editor dikelola sebagai entitas sesi datar (*flat sessions*) yang independen, tanpa hierarki bersarang (*nested state*) yang kompleks.
   - Membatasi kuantitas sesi aktif bersamaan maksimal 5 item untuk mencegah pembengkakan memori peramban dan penumpukan chip di bilah navigasi.

3. **In-Memory Retention & Visual Dirty Indicator pada Code Editor**:
   - Konten Code Editor dipertahankan dalam memori CodeMirror saat diminimalkan tanpa melakukan penyimpanan otomatis (*auto-save*) ke server remote demi keamanan konfigurasi produksi.
   - Chip editor pada Workspace Shelf menampilkan indikator perubahan (titik oranye / tanda asterisk `*`) jika berkas memiliki suntingan yang belum disimpan.
   - Penutupan permanen (tombol `✕`) pada sesi yang memiliki suntingan memicu dialog konfirmasi eksplisit (*Confirmation Dialog*) untuk mencegah kehilangan data.

4. **Smart Resume pada Tombol View Kartu Host**:
   - Ketika administrator menekan tombol "View" pada kartu Host di dashboard, sistem memeriksa apakah host tersebut sudah memiliki sesi aktif di Workspace Shelf.
   - Jika ditemukan, sistem melakukan *Smart Resume*—memulihkan modal tepat pada tab aktif (misal File Manager) dan path direktori terakhir, alih-alih mereset tampilan ke tab Overview.
   - Tombol "View" pada kartu host menyematkan badge indikator sesi aktif berwarna aksen untuk visibilitas instan.

5. **Kontrol Standar Window Modal**:
   - Header pada Host Detail Modal dan Code Editor Modal dilengkapi tombol minimize (`—`) berdampingan dengan tombol tutup (`✕`).
   - Penekanan tombol `Escape` pada Workspace Modal memicu tindakan minimize secara aman tanpa membuang status kerja pengguna.
