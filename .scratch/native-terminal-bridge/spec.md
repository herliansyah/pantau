# Spesifikasi: Native Terminal Bridge

Status: resolved

## Deskripsi
Fitur "Buka di Native Terminal" pada Pantau yang memungkinkan administrator meluncurkan aplikasi terminal emulator bawaan sistem operasi (Windows Terminal, GNOME Terminal, iTerm2, Alacritty, dll.) yang langsung terhubung ke target Host secara terautentikasi tanpa perlu memasukkan kata sandi lagi, baik saat Pantau berjalan lokal pada workstation admin maupun saat diakses melalui peramban jarak jauh (remote web).

## Keputusan Arsitektur
Sesuai hasil wayfinding di `.scratch/native-terminal-bridge/map.md`:

### 1. Deteksi Terminal Lintas OS & Eksekusi Shell Chaining (Tiket 01)
- **Kaskade Deteksi Andal (`exec.LookPath`)**:
  - **Windows**: `exec.LookPath("wt.exe")` (mencakup alias Store App `%LOCALAPPDATA%\Microsoft\WindowsApps\wt.exe`), fallback ke `pwsh.exe` / `powershell.exe`, dan fallback universal ke `%COMSPEC%` (`cmd.exe`).
  - **macOS**: Memeriksa direktori Application Bundle (`/Applications/iTerm.app` atau `~/Applications/iTerm.app`), fallback ke Apple `Terminal.app` (`/System/Applications/Utilities/Terminal.app` atau `/Applications/Utilities/Terminal.app`).
  - **Linux**: Memeriksa env `$TERMINAL`, fallback ke `x-terminal-emulator` (`update-alternatives`), disusul kueri desktop environment (GNOME/KDE/XFCE), dan pemindaian `$PATH` berurutan (`gnome-terminal`, `konsole`, `xfce4-terminal`, `alacritty`, `kitty`, `xterm`).
- **Interactive Retention & Anti-Exit (Shell Chaining)**:
  - Menghindari flag `--hold`/`--noclose` (yang membekukan buffer layar dan menonaktifkan input shell).
  - Menggunakan teknik *Shell Chaining* agar jendela terminal tetap terbuka menampilkan pesan error dan langsung menyerahkan shell prompt lokal jika koneksi SSH gagal atau terputus:
    - Windows Terminal: `wt.exe new-tab cmd.exe /k "ssh -i \"<key>\" -p <port> user@host"`
    - PowerShell: `powershell.exe -NoExit -Command "ssh -i '<key>' -p <port> user@host"`
    - CMD: `cmd.exe /c start "Pantau" cmd.exe /k "ssh -i \"<key>\" -p <port> user@host"`
    - macOS: `osascript -e 'tell application "Terminal" to do script "ssh -i \"<key>\" -p <port> user@host; exec $SHELL"'`
    - Linux: `<terminal> -e bash -c 'ssh -i "<key>" -p <port> user@host; exec bash'` (`--` untuk `gnome-terminal`).
- **Pelepasan Proses Non-blocking di Go**:
  - Peluncuran via `cmd.Start()`.
  - Atribut proses `SysProcAttr`: `Setpgid: true` (Unix) dan `CreationFlags: CREATE_NEW_PROCESS_GROUP | DETACHED_PROCESS` (Windows) agar jendela terminal mandiri dari siklus hidup daemon Pantau.

### 2. Ephemeral SSH Key Injection & Auto-Cleanup (Tiket 02)
- **Pembuatan Kunci In-Memory**:
  - Dihasilkan murni di RAM menggunakan stdlib `crypto/ed25519`, `crypto/rand`, dan `golang.org/x/crypto/ssh` dalam ~20 mikrodetik (kunci 256-bit).
  - Private key tidak pernah ditulis ke disk server Pantau.
- **Injeksi Atomik via Active Session**:
  - Diinjeksi menggunakan `runner.ExecTimeout` yang sudah terautentikasi dalam 1 round-trip (~20–40ms).
  - Menggunakan komentar metadata: `ssh-ed25519 <key> pantau-ephemeral-<id>-<expiry>` (menghindari opsi `expiry-time` OpenSSH 7.7+ yang ditolak daemon legacy seperti CentOS 6/7/Dropbear).
  - Menegakkan permissions `chmod 700 ~/.ssh` dan `chmod 600 ~/.ssh/authorized_keys`.
  - Penanganan trailing newline aman (`[ -s ~/.ssh/authorized_keys ] && [ -n "$(tail -c1 ~/.ssh/authorized_keys)" ] && echo "" >> ~/.ssh/authorized_keys`).
- **Two-Tier Auto-Cleanup**:
  - *Tier 1 (Proactive Go Timer)*: `time.AfterFunc` (TTL default 5 menit) atau saat terminal ditutup, menghapus entri Session ID spesifik (`pantau-ephemeral-<id>-*`).
  - *Tier 2 (Reactive On-Host Sweep)*: Filter POSIX `awk` dengan atomic rename (`mv -f .tmp.$$ authorized_keys`) saat inspeksi berkala atau sebelum injeksi baru dengan grace period 10 menit (anti-drift).
  - *Sifat OpenSSH*: Kunci hanya divalidasi pada saat handshake awal; penghapusan kunci ephemeral setelah koneksi terbentuk tidak memutus sesi shell aktif.

### 3. Remote Web Launcher Script & One-Click Command (Tiket 03)
- **Tiket Sekali Pakai (Burn-After-Read Token)**:
  - Web meminta token via `POST /api/terminal/native-ticket` dengan cookie sesi aktif. Token 32-byte acak ber-TTL 60 detik di memori RAM server.
  - Pengunduhan berkas via `GET /api/terminal/native-download?ticket=<token>&os=<win|mac|linux>` langsung menghapus (*burn-after-read*) entri dari RAM server seketika untuk mencegah *replay attack*.
- **Self-Extracting & Self-Deleting Client Script**:
  - **Windows (`.cmd`)**: Menyematkan PEM key, mengekstrak ke `%TEMP%\pantau_<id>.key` dengan `icacls "%KEYFILE%" /inheritance:r /grant:r "%USERNAME%:(R)"` (khusus user aktif agar lolos OpenSSH StrictModes), menjalankan SSH, lalu menghapus file key dan file `.cmd` itu sendiri (`del "%~f0"`).
  - **macOS (`.command`) & Linux (`.sh`)**: Menggunakan `trap 'rm -f "$KEYFILE" "$0"' EXIT INT TERM` untuk menjamin file key di `mktemp` (`chmod 600`) dan berkas script launcher langsung terhapus saat SSH selesai, gagal, atau di-interupsi (`Ctrl+C`).
- **Modal One-Click Copy Command Streaming**:
  - Endpoint streaming sekali pakai:
    - Linux/macOS: `curl -sSL https://<pantau-url>/api/terminal/native-run?ticket=<token> | bash`
    - Windows PowerShell: `irm https://<pantau-url>/api/terminal/native-run?ticket=<token> | iex`

### 4. Pengaturan & Kustomisasi Template Terminal (Tiket 05)
- **Skema Penyimpanan Hybrid**:
  - Konfigurasi server: `terminal_launcher_mode` (`"auto"` | `"custom"`), `terminal_launcher_template` (string).
  - Override lokal per-perangkat melalui `localStorage` peramban.
- **Validasi Keamanan Ketat (Strict Pre-flight Check)**:
  - Wajib mengandung placeholder `%cmd%`.
  - Verifikasi token biner pertama via `exec.LookPath` sebelum simpan; menolak jika biner tidak ditemukan atau mengandung karakter metakarakter berbahaya.
- **Mekanisme Graceful Fallback**:
  - Jika executable kustom tidak ditemukan pada saat runtime peluncuran, sistem otomatis beralih ke deteksi bawaan OS (Tiket 01) dan memunculkan toast pemberitahuan tanpa menghentikan peluncuran terminal.

### 5. Integrasi Antarmuka UI & UX (Tiket 04)
- **Titik Akses**:
  - Tombol popout `↗` (`btnTerminalDockNative`) pada toolbar Terminal Dock di samping tombol maximize.
  - Tombol menu `↗ Buka di Native Terminal` pada dropdown `⋮` Host Card.
- **Deteksi Mode Adaptif**:
  - Jika `window.location.hostname` adalah `localhost` / `127.0.0.1` / `::1`: memanggil `POST /api/terminal/native-open`, backend langsung meluncurkan proses terminal di desktop, dan memunculkan toast sukses.
  - Jika diakses dari luar localhost (remote web): memanggil `POST /api/terminal/native-ticket` dan menampilkan modal dialog *Native Terminal Launcher* yang berisi tombol unduh script otomatis dan tab salin perintah streaming.

## Standar Keamanan & Keandalan (Bug-Free & Security Assurances)
1. **Zero Key Persistence**: Private key tidak pernah disimpan permanen di disk server maupun disk komputer klien.
2. **Strict File Permissions**: ACL Windows (`icacls`) dan Unix (`chmod 600`) menjamin kunci tidak dapat dibaca oleh user lain pada sistem multi-user.
3. **Anti-Replay**: Token tiket download dihapus seketika setelah pembacaan pertama (*burn-after-read*) dengan expiry 60 detik.
4. **Command Injection Prevention**: Semua parameter (hostname, username, port, template command) divalidasi ketat menggunakan regex sebelum dieksekusi.
5. **No Broken Host Keys**: Rutin pembersihan `authorized_keys` menggunakan POSIX `awk` atomik (`mv -f .tmp.$$ authorized_keys`) sehingga tidak memicu kegagalan exit code `grep -v` dan tidak merusak kunci SSH yang sudah ada.
