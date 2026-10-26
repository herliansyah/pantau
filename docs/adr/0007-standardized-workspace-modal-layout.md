# Standardized Workspace Modal Layout dan Action Dialog

Pemisahan kontainer modal antarmuka Pantau menjadi dua kategori baku: **Workspace Modal** (dimensi tetap 75vw × 80vh pada desktop, fullscreen pada mobile < 768px, untuk panel kerja intensif seperti Host Detail, Editor, dan Antrean Transfer) dan **Action Dialog** (lebar tetap ~520px - 740px untuk dialog aksi formulir dan dialog Pengaturan).

Keputusan ini diambil untuk mengeliminasi lonjakan layout (*cumulative layout shift*) saat berpindah tab pada panel yang memiliki data minim (misal Desired State kosong) dibandingkan tab berkonten tinggi (Terminal atau File Manager SFTP). Dengan mengunci dimensi kontainer dan membatasi scrolling hanya pada `modal-body`, fokus pandangan pengguna dan posisi tombol navigasi tetap stabil. Selain itu, status online dan metrik transfer pada kartu Host dibakukan menggunakan inline-flex tanpa pembungkusan karakter acak (*nowrap*) agar ikon indikator tidak terpisah dari label statusnya.

Komponen kerja di dalam Workspace Modal (seperti xterm.js Terminal dan editor CodeMirror) dikonfigurasi fleksibel (`flex: 1; height: 100%`) agar memanfaatkan penuh ruang vertikal 80vh. Modal Daftar Transfer dipecah menjadi dua tab (*Active & Queued* dan *History*). Modal Pengaturan (Settings) menggunakan kelas `modal-dialog-lg` (`max-width: 740px`) agar 6 tab konfigurasi (General, Security, Notifications, Backup, Presets, Updates) dapat tersusun dalam satu baris horizontal tanpa tab wrapping dan konten kartu dapat ditata berdampingan secara responsif. Sementara dialog penambahan aturan Desired State (`#ruleModal`), dialog ekspor enkripsi snapshot (`#snapshotExportModal`), dialog **Key Provisioning** (`#injectKeyModal`), dan **Confirmation Dialog** generik (`#confirmModal`) diklasifikasikan sebagai Action Dialog ringkas (`max-width: 440px - 600px`). Seluruh dialog bawaan peramban (`alert()`, `prompt()`, dan `confirm()`) dieliminasi total dari sistem dan digantikan oleh **Toast Notification** non-blocking serta dialog aksi resmi bergaya dark-glassmorphism untuk menjaga konsistensi antarmuka aplikasi desktop native serta integritas alur kerja pengguna. Pada kondisi tab tanpa isi (misal kontainer Docker kosong atau aturan belum ada), tampilan disajikan melalui wadah terpusat (*Centered Empty State Box*) dengan ikon dan panduan aksi alih-alih area kosong polos.

## Addendum: Modal Stacking, Z-Index Layering, dan Hierarchical Keyboard Dismissal

Untuk mendukung alur kerja di mana sebuah Action Dialog (misal form penambahan preset, aturan Desired State, dialog catatan host, atau ekspor snapshot) dibuka dari dalam Workspace Modal atau saat Terminal Dock sedang dalam mode maximize:

1. **Dynamic Z-Index Stacking (`openModalStack`)**:
   Sistem mengelola tumpukan modal aktif (`openModalStack`). Setiap kali modal baru dibuka melalui `openModal()`, elemen kontainer diberi nilai `z-index` dinamis yang lebih tinggi dari modal sebelumnya (`baseZ + stack.length * 10`). Hal ini menjamin child action dialog selalu muncul di atas parent workspace modal dan backdrop modal terminal dock tanpa konflik visual.

2. **Hierarchical Keyboard Dismissal (`Escape`)**:
   Penekanan tombol `Escape` di-handle secara terpusat dan berjenjang:
   - Jika terdapat modal di dalam stack, `Escape` hanya akan menutup modal teratas (*top-most active dialog*) dan memunculkan kembali interaksi pada modal di bawahnya.
   - Jika stack modal telah kosong, `Escape` baru akan mengaktifkan pintasan terminal dock (seperti minimize dock saat maximized per ADR 0011).

## Addendum: Top-Anchored Modal Alignment dan Promosi Network Diagnostic ke Workspace Modal

Untuk mengeliminasi pergeseran vertikal (*vertical layout jump*) yang dialami saat membuka dialog berbeda ukuran atau berganti sub-view (seperti *Interactive Probes* berukuran ringkas versus *Network Triage Matrix* berukuran tinggi):

1. **Top-Anchored Alignment Konsisten (`.modal`)**:
   Kontainer modal beralih dari pemusatan otomatis (`align-items: center`) menjadi penjangkaran atas (`align-items: flex-start; justify-content: center; padding: clamp(24px, 5vh, 60px) 20px 20px 20px; overflow-y: auto;`). Seluruh modal (Action Dialog dan Workspace Modal) memiliki batas atas yang seragam dan stabil dari puncak layar. Saat isi formulir memanjang atau validasi kesalahan muncul, batas atas dan header dialog tetap tenang tanpa melompat.

2. **Promosi Network Diagnostic (`#netDiagModal`) ke Workspace Modal**:
   Network Diagnostic dipromosikan dari kelas dialog elastis (`.modal-dialog-xl`) menjadi kelas **Workspace Modal** (`.modal-workspace`), mengunci dimensi ke `85vw × 84vh` dengan internal scrolling pada `.modal-body`. Pergantian antara sub-view Probes dan Triage Matrix berjalan mulus tanpa mengubah koordinat tepi atas atau dimensi kontainer.

