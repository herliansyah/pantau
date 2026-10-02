# 0028. File Duplication, Disk Space Safety Check, Editor Modal Maximize, dan UI Polish

Penggandaan berkas/folder lokal pada SFTP File Manager dengan proteksi kepenuhan disk, pembesaran modal penyunting berkas (CSS full-viewport) bertema VS Code Dark+, serta navigasi breadcrumb interaktif.

## Konteks dan Masalah

Modul SFTP File Manager pada Pantau sebelumnya mendukung penjelajahan direktori, unduh/unggah berkas, ubah izin (*chmod*), dan penyuntingan kode. Namun terdapat beberapa keterbatasan operasional:

1. **Ketiadaan Fitur Kloning / Duplikasi Lokal (*File/Folder Duplication*)**:
   Administrator sering kali perlu mencadangkan berkas konfigurasi (seperti `nginx.conf`, `.env`, atau direktori aplikasi) sebelum melakukan perubahan. Tanpa fitur duplikasi lokal, pengguna harus mengunduh berkas ke komputer lokal lalu mengunggahnya kembali dengan nama berbeda, atau membuka terminal shell secara manual untuk menjalankan perintah `cp`.
2. **Risiko Kepenuhan Disk pada Host Target (*Disk Exhaustion Risk*)**:
   Menggandakan berkas atau folder berukuran besar (misal direktori database, log, atau arsip) tanpa memverifikasi sisa kapasitas partisi target berisiko membuat disk 100% penuh, yang dapat melumpuhkan layanan produksi (seperti MySQL/PostgreSQL yang *crash* saat disk penuh).
3. **Risiko Tabrakan Nama (*Name Collision / Accidental Overwrite*)**:
   Operasi duplikasi tidak boleh secara diam-diam menimpa file/folder yang sudah ada karena dapat mengakibatkan kehilangan data (*data loss*).
4. **Keterbatasan Ruang Kerja Modal Editor Berkas**:
   Modal editor berkas CodeMirror sebelumnya berukuran tetap (75vw × 80vh). Untuk file konfigurasi panjang atau penelaahan kode rumit, ruang pandang tersebut terasa sempit. Selain itu, tema Nord sebelumnya memiliki kontras warna yang kurang tajam dibanding tema modern yang biasa digunakan developer (seperti VS Code Dark+).
5. **Kepadatan Kolom Aksi dan Navigasi Jalur Statis**:
   Navigasi jalur direktori hanya berupa teks statis dan tombol satu tingkat ke atas (*Up Dir*). Penambahan aksi baru pada tabel berkas jika tidak ditata rapi akan membuat baris tabel terpotong atau bertumpuk canggung.

## Keputusan Arsitektur

1. **Eksekusi Duplikasi Native Remote (`cp -a`) Tanpa Aliran Pipa Master**:
   - Sumber dan tujuan duplikasi berada pada Host yang sama.
   - Pantau mengeksekusi perintah shell `cp -a -- <src> <dest>` langsung di remote host melalui SSH runner.
   - Menghemat bandwidth jaringan master, mengeksekusi kloning secara instan pada sistem berkas lokal host, dan mempertahankan perizinan (*permissions*), kepemilikan (*ownership*), serta timestamp asli berkas.

2. **Format Penamaan Default Berbasis Timestamp Presisi**:
   - Untuk berkas berektensi: `<nama_dasar>_<YYYYMMDDHHmmss>.<ekstensi>` (misal: `nginx_20261002043000.conf`). Posisi timestamp disisipkan sebelum ekstensi agar asosiasi format file dan *syntax highlighting* CodeMirror tetap utuh.
   - Untuk direktori atau berkas tanpa ekstensi: `<nama>_<YYYYMMDDHHmmss>` (misal: `backup_20261002043000`).
   - Dialog konfirmasi (`openDuplicateModal`) menyajikan input nama yang sudah terisi default nama ber-timestamp sehingga pengguna dapat langsung menekan Enter atau menyesuaikan nama sebelum proses berjalan.

3. **Disk Space Safety Check & Proteksi Ambang Ukuran**:
   - Sebelum eksekusi `cp -a`, sistem mengukur ukuran item sumber (`du -sb`) dan sisa kapasitas ruang kosong pada partisi tujuan (`df -PB1`).
   - **Safety Buffer 100 MB**: Duplikasi ditolak dengan status HTTP 507 Insufficient Storage jika sisa kapasitas partisi $< (\text{ukuran item} + 100\text{ MB})$.
   - Pada sisi antarmuka, jika kapasitas tidak cukup, tombol duplikasi dinonaktifkan dengan banner kesalahan merah.
   - **Ambang 500 MB**: Jika item berukuran $\ge$ 500 MB, dialog menampilkan *warning badge* kuning yang menginformasikan bahwa proses mungkin memakan waktu beberapa detik.

4. **Collision Prevention (Strict Reject)**:
   - Sistem memverifikasi keberadaan path tujuan via SFTP `Stat`. Jika path tujuan sudah ada, proses ditolak dengan HTTP 409 Conflict (*"Target file or folder already exists"*).

5. **Editor Modal Maximize (CSS Full-Viewport 100vw × 100vh)**:
   - Mengikuti pola `Workspace Modal Maximize` (ADR-0026) dengan tombol maximize/restore `⛶` / `🗗` di header modal, pintasan *double-click* pada header, dan penyelarasan ulang ukuran editor (`codeEditor.refresh()`).
   - Tidak menggunakan API `requestFullscreen()` native browser guna menghindari prompt izin peramban dan memelihara kebebasan navigasi keyboard.

6. **Tema Tersemat VS Code Dark+ & Deteksi Mode Bahasa Dinamis**:
   - Menambahkan tema CSS tersemat `.cm-s-vscode-dark` sesuai prinsip *Airgapped Self-Contained Web Assets* (ADR-0013) tanpa CDN eksternal: latar belakang `#1e1e1e`, font monospace modern (`JetBrains Mono`, `Fira Code`, `Consolas`), serta pewarnaan sintaks kontras tinggi.
   - Fungsi `getEditorModeForPath` secara otomatis memilih mode bahasa CodeMirror yang tepat berdasarkan ekstensi berkas (`.php`, `.js`, `.sh`, `.yml`, `.html`, `.css`, dll.).

7. **Interactive Breadcrumb Navigation & File Manager UI Polish**:
   - Mengganti teks path statis dengan deretan tautan segmen direktori interaktif (`file-breadcrumb-bar`). Pengguna dapat mengklik segmen mana saja (misal: `📁 /` &rarr; `var` &rarr; `www`) untuk melompat langsung ke direktori induk.
   - Menata ulang kolom aksi dengan 6 kolom presisi (`action-grid`: Terminal, Edit, Duplicate, Download, Copy to host, Delete).
   - Menambahkan ikon kontekstual per ekstensi (`getFileIcon`) dan wadah tabel berkas dengan *sticky header* (`files-table-container`).
