# 0036. Native Terminal Bridge dan Ephemeral Key Injector

Menyediakan jembatan peluncuran terminal emulator native sistem operasi (Windows Terminal, GNOME Terminal, iTerm2, Alacritty, dll.) yang terhubung langsung ke Host Linux terkelola secara instan dan aman tanpa login manual, didukung oleh injeksi kunci SSH sementara (Ephemeral RSA Keypair) berbasis memori, protokol URL khusus `pantau://`, skrip peluncur self-extracting sekali pakai, pewarisan direktori kerja aktif (Active Path Navigation), dan deteksi kaskade lintas platform.

## Konteks dan Masalah

Pantau menyediakan terminal web berbasis xterm.js di dalam peramban untuk kebutuhan observabilitas dan pemecahan masalah cepat. Namun, untuk sesi administrasi intensif, sysadmin dan engineer DevOps sering kali memerlukan terminal emulator bawaan sistem operasi (native terminal):
1. **Ergonomi & Kebiasaan Kerja**: Terminal native memiliki akselerasi hardware grafis, render font kustom, pintasan keyboard sistem (seperti `Ctrl+W`, `Ctrl+N`, tab management), serta integrasi erat dengan perkakas CLI lokal (tmux, Neovim, fzf, copy-paste OS native).
2. **Keterbatasan Web Terminal**: Web terminal di dalam browser rentan terhadap tab crash, batas penanganan buffering besar, serta intersepsi tombol pintas browser (misal `Ctrl+T`, `Ctrl+W`, `Ctrl+Tab`).
3. **Tantangan Batas Kepercayaan & Keamanan Autentikasi**:
   - Host target diautentikasi menggunakan kredensial master Pantau (kunci SSH universal Pantau atau password terenkripsi di database).
   - Membuka terminal eksternal dari workstation pengguna tidak boleh membocorkan kunci privat master Pantau ke komputer klien atau ke disk server.
   - Mengharuskan pengguna mengetik password SSH target secara manual akan merusak pengalaman pengguna dan tidak mungkin dilakukan jika host hanya mengizinkan public key.
4. **Dilema Akses Lokal vs Akses Jarak Jauh (Remote Web)**:
   - Saat Pantau dijalankan secara lokal di laptop/workstation admin (`localhost`), backend Go dapat meluncurkan proses terminal desktop secara langsung.
   - Saat Pantau dijalankan di server terpusat/VPS/Docker container dan diakses melalui remote browser, backend di remote tidak memiliki akses langsung untuk memunculkan jendela terminal di komputer desktop klien.

## Keputusan Arsitektur

1. **Injeksi Kunci SSH Ephemeral Murni di Memori (Zero Disk Persistence)**:
   - Setiap permintaan peluncuran terminal native membuat pasangan kunci SSH RSA 2048-bit sementara di memori RAM server menggunakan pustaka Go standar (`crypto/rsa`, `crypto/rand`, `golang.org/x/crypto/ssh`).
   - Format RSA 2048-bit dipilih karena memiliki kompatibilitas universal dari OpenSSH lama (CentOS 6 / OpenSSH 5.3+) hingga OpenSSH modern, tanpa memerlukan opsi modern yang dapat ditolak oleh daemon legacy.
   - Kunci privat tidak pernah ditulis ke disk server Pantau dan hanya diserahkan sekali ke proses terminal atau skrip peluncur.
   - Kunci publik disuntikkan secara atomik ke `~/.ssh/authorized_keys` di Host target melalui koneksi SSH aktif yang sudah terverifikasi dengan komentar terstruktur: `ssh-rsa <key> pantau-ephemeral-<id>-<expiry>`.
   - Menegakkan izin ketat `chmod 700 ~/.ssh` dan `chmod 600 ~/.ssh/authorized_keys`.

2. **Dua Tingkat Pembersihan Kunci Otomatis (Two-Tier Auto-Cleanup)**:
   - *Tier 1 (Proaktif - Go Timer)*: Timer latar belakang di backend Go (`time.AfterFunc`) dengan TTL 5 menit (atau saat sesi selesai) langsung mengeksekusi penghapusan kunci ephemeral dari `~/.ssh/authorized_keys` di remote host.
   - *Tier 2 (Reaktif - On-Host Sweep)*: Filter POSIX `awk` dengan operasi rename atomik (`mv -f .tmp.$$ authorized_keys`) dieksekusi sebelum injeksi baru atau selama inspeksi berkala untuk membersihkan kunci yatim yang telah kedaluwarsa dengan *grace period* 10 menit.
   - Sesuai protokol SSH RFC 4252, autentikasi kunci publik hanya diverifikasi pada saat jabat tangan awal (*initial handshake*). Penghapusan kunci dari `authorized_keys` setelah koneksi terbentuk tidak memutus sesi shell yang sedang berlangsung.

3. **Arsitektur Peluncuran Ganda (Dual Launch Modes)**:
   - **Mode Workstation Lokal (`localhost` / `127.0.0.1` / `::1`)**:
     - Memanggil `POST /api/terminal/native-open`.
     - Backend Go mendeteksi terminal emulator terpasang di OS host, menulis kunci privat sementara ke berkas temporer lokal berizin ketat, melepaskan proses terminal mandiri (*detached process* via `Setpgid` di POSIX atau `CREATE_NEW_PROCESS_GROUP | DETACHED_PROCESS` di Windows), lalu membersihkan berkas kunci setelah delay singkat.
   - **Mode Peramban Jarak Jauh (Remote Web)**:
     - Memanggil `POST /api/terminal/native-ticket` dengan cookie sesi aktif. Server menerbitkan tiket 32-byte acak dengan TTL 60 detik di memori (*burn-after-read*).
     - Menampilkan modal dialog interaktif dengan tiga opsi peluncuran:
       a. **Protokol URL Kustom (`pantau://`)**: Skema URI bawaan yang memicu peluncur lokal tanpa perlu mengunduh berkas.
       b. **Skrip Peluncur Self-Extracting**: Mengunduh berkas skrip berumur pendek via `GET /api/terminal/native-download?ticket=<token>&os=<os>` (`.cmd` untuk Windows dengan isolasi ACL `icacls`, `.command` untuk macOS, dan `.sh` untuk Linux dengan `trap` pembersihan otomatis).
       c. **Perintah Salin Satu-Klik (Streaming Command)**: Perintah eksekusi langsung via pipeline memori tanpa menyimpan file ke disk (`curl -sSL ... | bash` untuk Linux/macOS atau `irm ... | iex` untuk Windows PowerShell).

4. **Pewarisan Direktori Kerja Aktif (Active Path Navigation)**:
   - Sesi native terminal mewarisi direktori kerja aktif (*working directory*) saat diluncurkan:
     - Dari Terminal Dock: direktori aktif yang dilacak via ANSI OSC 7 atau prompt tracking.
     - Dari SFTP File Manager: folder yang sedang dijelajahi.
     - Dari Global Bookmarks: direktori induk dari berkas tersemat.
   - Parameter path divalidasi dan disanitasi (`cleanInitialDir`), kemudian dimasukkan ke dalam rangkaian perintah SSH (`cd '<dir>' && exec $SHELL -l` atau fallback aman ke `$HOME` jika path tidak ditemukan).

5. **Kaskade Deteksi Terminal & Retensi Interaktif (Interactive Shell Chaining)**:
   - Kaskade deteksi otomatis:
     - **Windows**: Windows Terminal (`wt.exe`), PowerShell Core (`pwsh.exe`), Windows PowerShell (`powershell.exe`), dan Command Prompt (`cmd.exe`).
     - **macOS**: iTerm2 (`/Applications/iTerm.app`), Apple Terminal (`Terminal.app`).
     - **Linux**: `$TERMINAL`, `x-terminal-emulator`, disusul deteksi lingkungan desktop dan urutan `$PATH` (`gnome-terminal`, `konsole`, `xfce4-terminal`, `alacritty`, `kitty`, `xterm`).
   - Mencegah jendela terminal langsung tertutup jika SSH gagal atau koneksi terputus dengan menggunakan teknik *Shell Chaining* (`cmd /k`, `powershell -NoExit`, `exec bash`), sehingga pesan kegagalan tetap terbaca dan kontrol diserahkan ke shell lokal.
   - Pengaturan kustomisasi template terminal pengguna (misal: `alacritty -e %cmd%`) dengan verifikasi `exec.LookPath` dan pencegahan *command injection*.

6. **Integrasi Antarmuka UI Komprehensif**:
   - Tombol popout `↗` pada bilah alat Terminal Dock di samping tombol maximize.
   - Item menu `↗ Buka di Native Terminal` pada menu aksi `⋮` Host Card di tampilan Grid dan List.
   - Tombol aksi `↗ Native Terminal` pada bilah alat Pengelola Berkas SFTP.
   - Tombol peluncur cepat pada modal Global Bookmarks untuk membuka terminal langsung ke folder berkas tersemat.

## Konsekuensi

- **Keamanan Tanpa Kompromi**: Kunci privat master Pantau tidak pernah dibagikan atau diekspos; kunci ephemeral berumur 5 menit dan terhapus otomatis secara atomik.
- **Pengalaman Pengguna Tanpa Hambatan**: Administrator dapat beralih dari dashboard web ke terminal native favorit dalam satu klik tanpa autentikasi manual.
- **Portabilitas Lintas Lingkungan**: Bekerja mulus baik di workstation lokal pengembang maupun server VPS produksi yang diakses via browser jarak jauh.
- **Pembersihan Bersih & Anti-Bocor**: Kombinasi timer Go proaktif dan pemindaian posix awk reaktif menjamin `authorized_keys` tetap bersih dari kunci kedaluwarsa.
