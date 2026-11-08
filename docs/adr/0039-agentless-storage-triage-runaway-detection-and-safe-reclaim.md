# 0039. Agentless Storage Triage, Deteksi Runaway File, dan Katalog Safe Reclaim

Menyediakan kapabilitas diagnostik penyimpanan interaktif *on-demand* (*Storage Triage*) berbasis SSH tanpa remote agent untuk mengidentifikasi konsumsi kapasitas partisi (*Space Hog*), mendeteksi berkas terhapus yang masih tertahan oleh proses aktif (*Ghost File*), melacak berkas yang mengalami lonjakan ukuran abnormal (*Runaway File*), serta menyediakan katalog pembersihan aman terkurasi (*Reclaimable Storage*) dan pengosongan log in-place (*Log Truncation*) dengan transparansi perintah shell dan proteksi *concurrency guard*.

## Konteks dan Masalah

Saturasi ruang disk (*disk exhaustion* 90–100%) merupakan salah satu penyebab insiden paling sering dan berakibat fatal pada server Linux, yang dapat menghentikan database (MySQL/PostgreSQL), mematikan container Docker, atau memicu kegagalan sistemik OS. Dalam praktiknya, administrator kerap menghadapi kebingungan spesifik:

1. **Keterbatasan Visibilitas Metrik Dasar (`df`)**:
   - Inspeksi rutin Pantau membaca metrik `df` yang hanya menyajikan persentase kapasitas partisi (misal: `/` terpakai 98%), namun tidak memberikan informasi direktori mana atau berkas apa yang menghabiskan ruang disk tersebut.
2. **Jebakan *Ghost Files* (Unlinked Open File Descriptors)**:
   - Kesalahan umum operator adalah menghapus file log aktif menggunakan perintah `rm` (`rm /var/log/app.log`). Di Linux, ruang disk tidak dilepas oleh filesystem selama proses aplikasi masih membuka *file descriptor* tersebut. Akibatnya, `df` tetap melaporkan disk 100% penuh meski file tidak terlihat lagi saat di-`ls`.
3. **Ancaman Berkas Liar (*Runaway Files*)**:
   - Aplikasi yang mengalami *infinite error loop* dapat menulis jutaan baris log dalam hitungan menit, menghabiskan puluhan gigabyte ruang disk secara mendadak. Pelacakan manual file yang sedang tumbuh cepat sangat lambat tanpa perkakas khusus.
4. **Keraguan dan Risiko Salah Hapus Data**:
   - Administrator kerap ragu membebaskan ruang disk karena khawatir menghapus file krusial sistem operasi atau database. Tidak ada batasan yang jelas antara data sampah non-kritis yang aman dibersihkan dengan berkas aktif yang harus dipertahankan.
5. **Bahaya Pemindaian Rekursif pada Background Polling**:
   - Menjalankan pemindaian `du` rekursif mendalam ke seluruh partisi pada siklus inspeksi rutin (setiap 30–60 detik) adalah *anti-pattern* yang merusak cache I/O disk, memperlambat server target, dan berisiko mengalami kebuntuan (*hung I/O*) pada mount jaringan (NFS/CIFS).

## Keputusan Arsitektur

1. **Model Eksekusi On-Demand Berbatas Waktu, Concurrency Guard & Load Throttling**:
   - Diagnostik penyimpanan diisolasi sebagai modul on-demand (**`Storage Triage`**) yang hanya dieksekusi atas inisiatif pengguna atau klik investigasi dari [Alert Banner](file:///home/ian/orca/workspaces/pantau/cetacean/CONTEXT.md#L272), bukan bagian dari background inspection berkala.
   - Dibatasi secara deterministik dengan batas waktu aman 40 detik menggunakan wrapper POSIX `timeout -k 2s 40s` (context timeout 45 detik).
   - Menerapkan **`Triage Concurrency Guard`** per Host di backend Pantau untuk mencegah penumpukan pemindaian paralel jika pengguna memicu triage berulang kali.
   - **Load-Aware Throttling & Cooldown Guard**:
     - Menerapkan cooldown 2 menit pada kondisi beban normal.
     - Cooldown diperpanjang adaptif menjadi 5 menit jika laju penulisan disk host berada pada rentang 15–40 MB/s.
     - Pemindaian otomatis ditolak sementara (`HTTP 429 Too Many Requests`) jika laju penulisan disk > 40 MB/s atau CPU load > 2× jumlah core.
     - Menyediakan opsi *Emergency Force Scan* (`?force=true`) dengan dialog konfirmasi risiko di UI untuk penanganan insiden darurat.
     - Reset cooldown seketika secara otomatis begitu aksi *safe reclaim* atau *log truncation* berhasil dijalankan, memungkinkan verifikasi pembebasan ruang disk tanpa jeda.

2. **Deteksi Diagnostik Bertingkat (Tiered POSIX Discovery)**:
   - Pemindaian direktori tingkat pertama menggunakan `du -kx --max-depth=1 /` dengan flag `-x` (*one-file-system*) untuk mengisolasi partisi target dan mencegah eksekusi menyeberang ke filesystem virtual (`/proc`, `/sys`) atau mount eksternal. Penanganan parser mendukung nama folder yang mengandung spasi tanpa terpotong.
   - Pemetaan 15 berkas terbesar (*Top Large Files*) di partisi target menggunakan kueri `find <mount> -xdev -type f -size +15M`.
   - Deteksi **`Ghost File`** menggunakan penelaahan berkas terhapus yang masih di-hold proses (`lsof +L1` atau audit `/proc/*/fd` berstatus `(deleted)` via inspeksi `stat -L -c %s "$fd"`), menyajikan nama proses, PID, path asli, dan kuantitas byte yang tertahan secara akurat.
   - Deteksi **`Runaway File`** dengan mengorelasikan lonjakan metrik `Disk I/O Write Rate` dan pemindaian berkas log/teks aktif yang termodifikasi dalam 60 menit terakhir (`mmin -60`), dengan pemangkasan (*pruning*) mutlak pada direktori mesin database (`mysql`, `mariadb`, `postgresql`, `postgres`, `redis`, `mongodb`, `docker/volumes`) serta eksklusi berkas tabel DBMS (`.ibd`, `.frm`, `.myd`, `.myi`, `.db`, `.sqlite*`, `.rdb`, `.wal`) agar transaksi normal database yang terus memperbarui mtime tidak pernah salah diklasifikasikan sebagai runaway files.

3. **Katalog Terkurasi Reclaimable Storage (Distro-Aware Whitelist)**:
   - Pantau mendeteksi utilitas dan sistem operasi pada Host target, dan hanya menyajikan kandidat pembersihan yang terbukti aman secara operasional (*idempotent & non-destructive*):
     - **Systemd Journal Vacuum**: `journalctl --vacuum-time=3d` atau `journalctl --vacuum-size=200M`.
     - **Package Manager Cache**: `apt-get clean` (Debian/Ubuntu), `dnf clean all` / `yum clean all` (RHEL/CentOS), `pacman -Scc --noconfirm` (Arch), atau `apk cache clean` (Alpine).
     - **Rotated Archive Logs**: Penghapusan berkas arsip rotasi log usang terkompresi di `/var/log/` yang berakhiran `.gz`, `.[0-9].gz`, atau `.1`.
     - **Systemd Core Dumps**: Pembersihan berkas crash dump di `/var/lib/systemd/coredump/*`.
     - **Container Reclaimable**: `docker system prune -af` (membersihkan image tak terpakai, stopped containers, build cache tanpa menyentuh volume data) dan `docker builder prune -f` (hanya jika daemon Docker terdeteksi aktif).
   - **Eksklusi Mutlak Folder `/tmp`**: Menghapus isi `/tmp` secara serampangan dilarang mutlak karena risiko memutus Unix domain socket (misal: MySQL, PHP-FPM) atau session file aktif.

4. **Remediasi Preskriptif dan Log Truncation Aman**:
   - Untuk berkas log aktif yang membengkak, Pantau menerapkan prosedur **`Log Truncation`** (`truncate -s 0 <path>` atau `: > <path>`), BUKAN perintah `rm`. Hal ini mengosongkan ukuran berkas seketika tanpa mengubah inode atau memutus *file descriptor* proses aplikasi yang sedang berjalan.
   - **Proteksi Path Traversal & Sistem**: Path log disanitasi dengan `path.Clean`, menolak traversal relatif (`..`), dan memblokir direktori krusial sistem (`/`, `/etc`, `/usr`, `/boot`, `/lib`, `/dev`, `/proc`, `/sys`).
   - Untuk *Ghost Files*, Pantau menyajikan rincian proses penahan dan menyediakan opsi *Graceful Service Reload* (`systemctl reload <service>`) atau tombol pintas navigasi ke sesi terminal interaktif ([Terminal From Path](file:///home/ian/orca/workspaces/pantau/cetacean/CONTEXT.md#L300)).
   - Setiap aksi pembersihan menyajikan [Confirmation Dialog](file:///home/ian/orca/workspaces/pantau/cetacean/CONTEXT.md#L168) transparan yang menampilkan perintah shell riil yang akan dikirim melalui SSH sebelum dieksekusi.

5. **Penanganan Hak Akses Adaptif (`sudo -n`)**:
   - Pemeriksaan otomatis `id -u`. Jika koneksi SSH menggunakan user non-root, perintah diagnostik dan pembersihan dijalankan dengan prefiks `sudo -n` (non-interaktif).
   - Jika hak sudo terbatas atau membutuhkan password, Pantau tidak memutus koneksi atau mengalami kegagalan fatal, melainkan menyajikan data yang berhasil diakses disertai anotasi izin terbatas.

6. **Integrasi Antarmuka Pengguna Terpadu & Snapshot View**:
   - Tab baru **Storage Triage** ditambahkan pada [Workspace Modal](file:///home/ian/orca/workspaces/pantau/cetacean/CONTEXT.md#L112) Host Detail.
   - Menyajikan tampilan snapshot instan dari hasil pemindaian terakhir (`/api/hosts/{id}/storage-triage-latest`) yang disimpan dalam cache in-memory tanpa membebani database atau melakukan auto-scan tak terduga.
   - Tombol pintas diagnostik (*deep-link*) disematkan langsung pada bilah [Alert Banner](file:///home/ian/orca/workspaces/pantau/cetacean/CONTEXT.md#L272) saat terjadi alert saturasi disk dan pada widget indikator disk kartu Host dashboard jika kapasitas melebihi ambang batas aman (> 85%).

7. **Ekstensi Dukungan Server Target Windows (Native PowerShell Agentless)**:
   - Menyediakan kapabilitas Storage Triage penuh untuk Host berbasis Windows Server tanpa instalasi remote agent, memanfaatkan eksekusi PowerShell berkode Base64 UTF-16LE via SSH:
     - **Partisi Drive Sistem**: Pembacaan kapasitas, ruang bebas, dan % keterisian drive `C:` menggunakan CIM query `Win32_LogicalDisk`.
     - **Space Hogs Berkecepatan Tinggi**: Menghitung ukuran folder tingkat atas di `C:\` (folder utama serta penemuan dinamis direktori non-sistem) menggunakan `robocopy /L` *list-only mode* tanpa I/O copy, dengan fallback aman ke `Get-ChildItem`.
     - **Top Large Files & Runaway Logs**: Pemindaian satu putaran (*single-pass*) pada direktori kerja umum untuk mendeteksi berkas besar (>15MB) dan runaway logs termodifikasi dalam 60 menit terakhir, dengan eksklusi mutlak pada berkas DBMS (`.mdf`, `.ldf`, `.ndf`, `.edb`, `.dit`, `.sqlite*`, `.vhdx`, `.avhdx`, `.bak`), arsip (`.7z`, `.rar`, `.xz`, `.bz2`, `.cab`), serta berkas konfigurasi/source code.
     - **Katalog Safe Reclaim Windows**: Pembersihan terkurasi untuk Windows Update Cache (`SoftwareDistribution\Download`), System & User Temp (`C:\Windows\Temp`, `%TEMP%`, `C:\Temp`), Error Reports & Crash Dumps (`WER`, `Minidump`, `CrashDumps`, `MEMORY.DMP`), Keranjang Sampah (`Clear-RecycleBin`), arsip log IIS lama (>7 hari), serta Docker on Windows.
     - **Log Truncation Windows Aman**: Pengosongan 0-byte in-place via `Clear-Content -LiteralPath '...' -Force` dengan batasan keamanan ketat yang menolak direktori sistem (`System32`, `SysWOW64`, `WinSxS`, `Boot`, `Recovery`), swapfiles (`pagefile.sys`, `swapfile.sys`, `hiberfil.sys`, `dumpstack.log`), target direktori, dan berkas database.

## Konsekuensi

- **Resolusi Masalah Disk Cepat & Terarah**: Administrator dapat mengidentifikasi penyebab saturasi ruang penyimpanan dan berkas liar dalam satu klik tanpa perlu mengingat atau mengetik perintah shell kompleks secara manual.
- **Pencegahan Downtime Akibat Salah Hapus**: Standarisasi katalog *Safe Reclaim* dan prosedur *Log Truncation* mengeliminasi risiko kerusakan sistem atau database akibat penghapusan berkas acak.
- **Eliminasi Masalah Ghost Files**: Deteksi eksplisit terhadap *file descriptor* terhapus yang masih ditahan proses menghentikan kebingungan "file sudah dihapus tapi disk tetap 100%".
- **Beban Server Minimal**: Pelaksanaan berbasis on-demand dengan timeout 20 detik dan *concurrency guard* melindungi server target dari saturasi I/O disk yang biasa timbul pada pemindaian rekursif terus-menerus.
