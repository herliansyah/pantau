# 01 — Cross-Platform OS Terminal Matrix & Execution Templates

Type: research
Status: resolved
Blocked by:

## Question

Bagaimana Pantau secara andal mendeteksi emulator terminal bawaan di berbagai sistem operasi desktop (Windows: `wt.exe` / `powershell.exe` / `cmd.exe`; macOS: `Terminal.app` / `iTerm.app`; Linux: `x-terminal-emulator` / `gnome-terminal` / `konsole` / `alacritty` / `kitty`), dan bagaimana struktur format argumen CLI yang valid untuk meluncurkan terminal baru yang langsung mengeksekusi perintah SSH tanpa langsung menutup jendela saat proses selesai?

## Answer

Riset dan spesifikasi komprehensif telah didokumentasikan di `docs/research/01-os-terminal-matrix.md`.

Ringkasan temuan:
1. **Strategi Deteksi Andal**:
   - **Windows**: Menggunakan kaskade `exec.LookPath("wt.exe")` (mencakup alias `%LOCALAPPDATA%\Microsoft\WindowsApps\wt.exe`), fallback ke `pwsh.exe` / `powershell.exe`, dan fallback universal ke `%COMSPEC%` (`cmd.exe`).
   - **macOS**: Memeriksa direktori Application Bundle (`/Applications/iTerm.app` atau `~/Applications/iTerm.app` untuk iTerm2, dan `/System/Applications/Utilities/Terminal.app` atau `/Applications/Utilities/Terminal.app` untuk Apple Terminal bawaan).
   - **Linux**: Memeriksa `$TERMINAL`, lalu `update-alternatives` (`x-terminal-emulator`), disusul kueri desktop environment (GNOME/KDE/XFCE), dan pemindaian `$PATH` berurutan (`gnome-terminal`, `konsole`, `xfce4-terminal`, `alacritty`, `kitty`, `xterm`).
2. **Interactive Retention & Anti-Exit Syntax (Shell Chaining)**:
   - Menghindari flag bawaan `--hold` yang membekukan buffer layar (*frozen screen* tanpa input).
   - Menggunakan teknik *Shell Injection / Chaining*:
     - Linux: `<terminal> -e bash -c 'ssh ...; exec bash'` (atau `--` untuk `gnome-terminal`).
     - macOS: `osascript -e 'tell application "Terminal" to do script "ssh ...; exec $SHELL"'` atau file peluncur `.command`.
     - Windows: `wt.exe new-tab cmd.exe /k "ssh ..."` atau `powershell.exe -NoExit -Command "ssh ..."`.
   - Menjamin bahwa saat koneksi SSH terputus atau gagal (*connection refused*, *host unreachable*), jendela terminal tetap terbuka menampilkan pesan error dan langsung menyerahkan shell prompt interaktif lokal kepada pengguna.
3. **Pelepasan Proses (Process Detachment di Go)**:
   - Menggunakan `cmd.Start()` non-blocking.
   - Atribut proses `SysProcAttr`: `Setpgid: true` pada Unix (Linux/macOS) dan `CreationFlags: CREATE_NEW_PROCESS_GROUP | DETACHED_PROCESS` pada Windows agar sesi terminal pengguna tidak terbunuh saat daemon Pantau di-reload atau menerima sinyal SIGINT/SIGTERM.
