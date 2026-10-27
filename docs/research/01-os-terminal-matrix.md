# Research: Cross-Platform OS Terminal Matrix & Execution Templates

| Dokumen | Keterangan |
| :--- | :--- |
| **Nomor Tiket** | `.scratch/native-terminal-bridge/issues/01-os-terminal-matrix.md` |
| **Topik** | Deteksi Andal Emulator Terminal OS & Sintaks Argumen CLI Eksekusi SSH Interaktif |
| **Status** | Selesai / Terverifikasi |
| **Target OS** | Windows (10/11), macOS (11+), Linux (X11 / Wayland) |

---

## 1. Ringkasan Eksekutif & Strategi Arsitektur

Fitur **Native Terminal Bridge** pada Pantau memungkinkan pengguna membuka sesi terminal eksternal bawaan sistem operasi yang langsung terhubung ke Host target via SSH tanpa memasukkan sandi manual. Untuk mewujudkan hal ini secara andal di berbagai platform desktop tanpa dependensi biner pihak ketiga (*zero external dependencies*, prinsip *Ponytail*), Pantau memerlukan dua komponen utama:

1. **Detection Engine**: Mekanisme deteksi terminal emulator bawaan/terpasang yang cepat, akurat, dan memiliki rantai fallback (*fallback cascade*) bertingkat jika terminal utama tidak tersedia.
2. **Execution Template & Shell Wrapper**: Format perintah CLI yang mampu meluncurkan terminal baru, mengeksekusi koneksi SSH, dan **mencegah jendela terminal langsung menutup jika koneksi gagal atau terputus** (*error hold / interactive retention*), sekaligus mempertahankan sesi tetap interaktif.

---

## 2. Analisis Kritis: *Shell Chaining* vs *Terminal-Specific Hold Flags*

Sebagian emulator terminal Linux menyediakan *flag* bawaan untuk menahan jendela tetap terbuka setelah proses anak selesai (contoh: `konsole --noclose`, `alacritty --hold`, `kitty --hold`, `xfce4-terminal --hold`). Namun, evaluasi mendalam menunjukkan kelemahan fatal pada opsi bawaan tersebut:
- **Tampilan Mati (*Frozen/Dead Buffer*)**: Sebagian besar flag `--hold` hanya membekukan jendela terminal pada frame terakhir. Pengguna tidak dapat mengetik, tidak dapat menjalankan perintah diagnostik lokal (seperti `ping` atau `traceroute`), dan harus menutup jendela secara paksa via mouse.
- **Inkonsistensi Lintas Terminal**: Flag penahan jendela tidak terstandardisasi (ada yang bernama `--hold`, `--noclose`, `-H`, dan ada yang sama sekali tidak memilikinya seperti GNOME Terminal modern).

### Solusi Standar: *Interactive Shell Injection / Chaining*
Pendekatan terbaik dan paling andal adalah membungkus perintah SSH ke dalam sub-shell lokal:
- **Unix (Linux & macOS)**:
  ```bash
  <terminal> <args> -- bash -c 'ssh -i "<key>" <user>@<host> -p <port>; exec bash'
  ```
  *Mekanisme*: Bila sesi SSH berakhir (baik karena logout normal maupun kegagalan koneksi seperti *connection refused* atau *host unreachable*), perintah `exec bash` (atau `exec $SHELL`) akan langsung mengambil alih proses dan memberikan prompt shell interaktif lokal kepada pengguna. Output error SSH tetap tercetak di layar dan pengguna dapat langsung memeriksa masalah tanpa kehilangan jendela.
- **Windows (CMD & PowerShell)**:
  - Pada CMD: Menggunakan switch `/k` (`cmd.exe /k "ssh ..."`) yang secara spesifik menjalankan perintah lalu mempertahankan sesi prompt CMD tetap aktif.
  - Pada PowerShell: Menggunakan switch `-NoExit` (`powershell.exe -NoExit -Command "ssh ..."`).

---

## 3. Matriks Deteksi & Argumen CLI Berdasarkan Sistem Operasi

### 3.1 Windows

Windows 10 (versi 1903+) dan Windows 11 menghadirkan variasi terminal dari konsol klasik hingga tab modern.

#### Rantai Prioritas Deteksi (Fallback Cascade)
1. **Windows Terminal (`wt.exe`)**: Terminal modern standar Windows 11 dan Windows 10 modern.
2. **PowerShell (`pwsh.exe` lalu `powershell.exe`)**: Shell standar berfitur ANSI escape sequence lengkap.
3. **Command Prompt (`cmd.exe`)**: Konsol dasar NT yang dijamin selalu ada di seluruh versi Windows.

#### Detail Teknis Deteksi di Go
```go
// 1. Cek Windows Terminal
// Catatan: Store app Windows Terminal teregistrasi di PATH pengguna via App Execution Aliases:
// %LOCALAPPDATA%\Microsoft\WindowsApps\wt.exe
if path, err := exec.LookPath("wt.exe"); err == nil {
    return "windows-terminal", path
}

// 2. Cek PowerShell Core (pwsh.exe) kemudian Windows PowerShell bawaan
if path, err := exec.LookPath("pwsh.exe"); err == nil {
    return "powershell-core", path
}
if path, err := exec.LookPath("powershell.exe"); err == nil {
    return "powershell", path
}

// 3. Fallback universal: CMD via %COMSPEC%
if comspec := os.Getenv("COMSPEC"); comspec != "" {
    return "cmd", comspec
}
if path, err := exec.LookPath("cmd.exe"); err == nil {
    return "cmd", path
}
```

#### Template Eksekusi CLI

| Terminal | Format Perintah Peluncuran | Penjelasan & Penanganan Error |
| :--- | :--- | :--- |
| **Windows Terminal (`wt.exe`)** | `wt.exe new-tab cmd.exe /k "ssh -i \"{{.KeyPath}}\" -p {{.Port}} {{.User}}@{{.Host}}"` | Membuka tab baru di jendela Windows Terminal yang ada. Membungkus SSH dengan `cmd /k` memastikan tab tidak tertutup bila SSH gagal. |
| **Windows Terminal (PS Style)** | `wt.exe new-tab powershell.exe -NoExit -Command "ssh -i '{{.KeyPath}}' -p {{.Port}} {{.User}}@{{.Host}}"` | Menggunakan profil PowerShell dengan parameter `-NoExit`. |
| **PowerShell Standalone** | `powershell.exe -NoExit -Command "ssh -i '{{.KeyPath}}' -p {{.Port}} {{.User}}@{{.Host}}"` | Membuka jendela PowerShell mandiri; `-NoExit` menjaga shell tetap hidup bila koneksi SSH putus. |
| **Command Prompt (`cmd.exe`)** | `cmd.exe /c start "Pantau - {{.Host}}" cmd.exe /k "ssh -i \"{{.KeyPath}}\" -p {{.Port}} {{.User}}@{{.Host}}"` | Menggunakan `start` untuk meluncurkan jendela konsol independen; `/k` mempertahankan prompt aktif saat SSH keluar. |

---

### 3.2 macOS

Pada macOS, aplikasi terminal didistribusikan dalam bentuk Application Bundle (`.app`) dan interaksi peluncuran command-line umumnya dikelola melalui `open` atau `osascript` (AppleScript).

#### Rantai Prioritas Deteksi (Fallback Cascade)
1. **iTerm2 (`iTerm.app`)**: Terminal paling populer di kalangan developer macOS dengan dukungan tab, profil, dan split pane.
2. **Apple Terminal (`Terminal.app`)**: Terminal bawaan sistem operasi macOS yang terpasang di setiap mesin Mac.

#### Detail Teknis Deteksi di Go
```go
// 1. Deteksi iTerm2 pada direktori aplikasi sistem maupun pengguna
itermPaths := []string{
    "/Applications/iTerm.app",
    filepath.Join(os.Getenv("HOME"), "Applications/iTerm.app"),
}
for _, p := range itermPaths {
    if fi, err := os.Stat(p); err == nil && fi.IsDir() {
        return "iterm2", p
    }
}

// 2. Deteksi Terminal.app bawaan (macOS Catalina 10.15+ di /System/Applications, versi lawas di /Applications)
appleTerminalPaths := []string{
    "/System/Applications/Utilities/Terminal.app",
    "/Applications/Utilities/Terminal.app",
}
for _, p := range appleTerminalPaths {
    if fi, err := os.Stat(p); err == nil && fi.IsDir() {
        return "apple-terminal", p
    }
}
```

#### Template Eksekusi CLI

Ada 2 strategi eksekusi untuk macOS:

##### Opsi A: Menggunakan AppleScript (`osascript`)
Sangat cocok untuk pemanggilan langsung tanpa membuat berkas sementara tambahan:

- **Apple Terminal (`Terminal.app`)**:
  ```bash
  osascript \
    -e 'tell application "Terminal" to do script "ssh -i \"{{.KeyPath}}\" -p {{.Port}} {{.User}}@{{.Host}}; exec $SHELL"' \
    -e 'tell application "Terminal" to activate'
  ```
- **iTerm2 (`iTerm.app`)**:
  ```bash
  osascript \
    -e 'tell application "iTerm2"' \
    -e '  set newWindow to (create window with default profile)' \
    -e '  tell current session of newWindow to write text "ssh -i \"{{.KeyPath}}\" -p {{.Port}} {{.User}}@{{.Host}}; exec $SHELL"' \
    -e '  activate' \
    -e 'end tell'
  ```

##### Opsi B: Menggunakan File Peluncur `.command` (Rekomendasi untuk Remote Web / Browser Bridge)
macOS mengasosiasikan ekstensi `.command` secara otomatis dengan Terminal bawaan. File skrip bash sederhana berekstensi `.command` yang diberi izin eksekusi (`chmod +x`) dapat dipicu cukup dengan perintah:
```bash
open -a Terminal /path/to/launcher.command
# atau untuk iTerm:
open -a iTerm /path/to/launcher.command
```
Isi skrip `launcher.command`:
```bash
#!/usr/bin/env bash
ssh -i "{{.KeyPath}}" -p {{.Port}} {{.User}}@{{.Host}}
echo ""
echo "[Pantau] Sesi SSH berakhir. Menjaga terminal tetap terbuka..."
exec $SHELL
```

---

### 3.3 Linux

Ekosistem Linux memiliki keragaman *Desktop Environment* (GNOME, KDE Plasma, XFCE, Sway/i3, Cinnamon). Deteksi terminal harus menghormati konfigurasi pengguna, standar distribusi, dan variasi instalasi biner.

#### Rantai Prioritas Deteksi (Fallback Cascade)
1. **Variabel Lingkungan `$TERMINAL`**: Konvensi standar Linux (diatur oleh pengguna atau skrip window manager seperti `i3-sensible-terminal`).
2. **Debian / Ubuntu Alternatives (`x-terminal-emulator`)**: Symlink sistem terkelola via `update-alternatives`.
3. **Kueri Desktop Environment Aktif**:
   - GNOME: `gsettings get org.gnome.desktop.default-applications.terminal exec`
   - KDE: `kreadconfig6 --group General --key TerminalApplication` (atau `kreadconfig5`)
   - XFCE: `xfce4-terminal` / `exo-open --launch TerminalEmulator`
4. **Pemeriksaan Biner di `$PATH` (Urutan Prioritas Pantau)**:
   - `gnome-terminal`
   - `konsole`
   - `xfce4-terminal`
   - `alacritty`
   - `kitty`
   - `xterm`

#### Detail Teknis Deteksi di Go
```go
// 1. Cek variabel lingkungan $TERMINAL
if termEnv := os.Getenv("TERMINAL"); termEnv != "" {
    if path, err := exec.LookPath(termEnv); err == nil {
        return filepath.Base(termEnv), path
    }
}

// 2. Cek update-alternatives standar Debian/Ubuntu
if path, err := exec.LookPath("x-terminal-emulator"); err == nil {
    return "x-terminal-emulator", path
}

// 3. Fallback pemindaian biner populer di $PATH
candidates := []string{
    "gnome-terminal",
    "konsole",
    "xfce4-terminal",
    "alacritty",
    "kitty",
    "xterm",
}
for _, bin := range candidates {
    if path, err := exec.LookPath(bin); err == nil {
        return bin, path
    }
}
```

#### Template Eksekusi CLI

| Terminal | Sintaks Perintah Peluncuran | Catatan Penanganan Argumen & Hold |
| :--- | :--- | :--- |
| **`x-terminal-emulator`** | `x-terminal-emulator -e bash -c 'ssh -i "{{.KeyPath}}" -p {{.Port}} {{.User}}@{{.Host}}; exec bash'` | Opsi `-e` didukung oleh seluruh terminal yang menjadi alternatif Debian. |
| **`gnome-terminal`** | `gnome-terminal -- bash -c 'ssh -i "{{.KeyPath}}" -p {{.Port}} {{.User}}@{{.Host}}; exec bash'` | Versi GNOME Terminal modern menggunakan pemisah argumen `--`. Jangan gunakan flag usang `-e`. |
| **`konsole` (KDE)** | `konsole -e bash -c 'ssh -i "{{.KeyPath}}" -p {{.Port}} {{.User}}@{{.Host}}; exec bash'` | Opsi bawaan `konsole --noclose -e ...` hanya menahan output tanpa prompt interaktif; pembungkusan `bash -c ...; exec bash` memberikan interaktivitas penuh. |
| **`alacritty`** | `alacritty -e bash -c 'ssh -i "{{.KeyPath}}" -p {{.Port}} {{.User}}@{{.Host}}; exec bash'` | Parameter `-e` pada Alacritty harus selalu berada di posisi argumen paling akhir. |
| **`kitty`** | `kitty sh -c 'ssh -i "{{.KeyPath}}" -p {{.Port}} {{.User}}@{{.Host}}; exec $SHELL'` | Kitty dapat mengeksekusi perintah langsung tanpa `-e`. Flag `--hold` juga tersedia (`kitty --hold sh -c '...'`). |
| **`xfce4-terminal`** | `xfce4-terminal -e 'bash -c "ssh -i \"{{.KeyPath}}\" -p {{.Port}} {{.User}}@{{.Host}}; exec bash"'` | Mendukung flag `-e` atau `--command`. Pembungkusan shell mencegah jendela tertutup jika SSH exit code != 0. |

---

## 4. Pola Peluncuran Proses di Go (*Process Detachment & Lifecycle*)

Ketika backend Pantau meluncurkan aplikasi terminal eksternal dari thread server web, proses anak (**child process**) harus terlepas sepenuhnya (*detached*) dari proses induk Pantau.

### Mengapa Detachment Krusial?
1. **Mencegah Blokade (*Non-Blocking*)**: Pantau tidak boleh menunggu (`cmd.Wait()`) proses terminal selesai karena sesi terminal pengguna bisa berlangsung berjam-jam.
2. **Kekebalan Sinyal (*Signal Immunity*)**: Jika layanan daemon Pantau di-restart atau menerima sinyal `SIGINT` / `SIGTERM`, terminal pengguna yang sedang aktif **tidak boleh ikut mati**.
3. **Pencegahan Zombie Process**: Proses terminal GUI sistem harus dialihkan ke init/systemd (Linux) atau berjalan pada session group independen (Windows/macOS).

### Implementasi Go Cross-Platform
```go
package terminalbridge

import (
    "os/exec"
    "runtime"
    "syscall"
)

// SpawnTerminalProcess meluncurkan terminal baru secara terlepas (detached)
func SpawnTerminalProcess(name string, args ...string) error {
    cmd := exec.Command(name, args...)

    // Konfigurasi atribut proses spesifik platform
    if runtime.GOOS == "windows" {
        // Pada Windows: CREATE_NEW_PROCESS_GROUP | DETACHED_PROCESS
        // Nilai konstanta: 0x00000200 | 0x00000008 = 0x00000208
        cmd.SysProcAttr = &syscall.SysProcAttr{
            CreationFlags: 0x00000208,
        }
    } else {
        // Pada Unix (Linux & macOS): Buat Process Group baru (Setpgid)
        cmd.SysProcAttr = &syscall.SysProcAttr{
            Setpgid: true,
        }
    }

    // Jalankan tanpa menunggu (Start, bukan Run)
    return cmd.Start()
}
```

---

## 5. Rekomendasi Format Template Variabel Pantau

Untuk mendukung kustomisasi terminal oleh pengguna (Tiket 05: *Launcher Settings & Customization*), template perintah dapat direpresentasikan menggunakan token substitusi standar:

| Token | Deskripsi | Contoh Nilai |
| :--- | :--- | :--- |
| `{{.Terminal}}` | Nama biner terminal | `wt.exe`, `gnome-terminal`, `open` |
| `{{.KeyPath}}` | Path absolut berkas private key sementara | `/tmp/pantau-temp-key`, `C:\Users\admin\.ssh\temp_id` |
| `{{.User}}` | Username SSH target | `root`, `ubuntu` |
| `{{.Host}}` | Hostname / IP address target | `192.168.1.100`, `db.internal.lan` |
| `{{.Port}}` | Port SSH target | `22`, `2222` |
| `{{.SSHCommand}}` | Rangkuman perintah SSH lengkap | `ssh -i "/tmp/key" -p 22 root@192.168.1.100` |

### Default Profile Preset Pantau
```json
{
  "windows": {
    "default": "windows-terminal",
    "templates": {
      "windows-terminal": ["wt.exe", "new-tab", "cmd.exe", "/k", "ssh -i \"{{.KeyPath}}\" -p {{.Port}} {{.User}}@{{.Host}}"],
      "powershell": ["powershell.exe", "-NoExit", "-Command", "ssh -i '{{.KeyPath}}' -p {{.Port}} {{.User}}@{{.Host}}"],
      "cmd": ["cmd.exe", "/c", "start", "Pantau - {{.Host}}", "cmd.exe", "/k", "ssh -i \"{{.KeyPath}}\" -p {{.Port}} {{.User}}@{{.Host}}"]
    }
  },
  "darwin": {
    "default": "apple-terminal",
    "templates": {
      "apple-terminal": ["osascript", "-e", "tell application \"Terminal\" to do script \"ssh -i \\\"{{.KeyPath}}\\\" -p {{.Port}} {{.User}}@{{.Host}}; exec $SHELL\"", "-e", "tell application \"Terminal\" to activate"],
      "iterm2": ["osascript", "-e", "tell application \"iTerm2\" to create window with default profile", "-e", "tell current session of current window of application \"iTerm2\" to write text \"ssh -i \\\"{{.KeyPath}}\\\" -p {{.Port}} {{.User}}@{{.Host}}; exec $SHELL\"", "-e", "tell application \"iTerm2\" to activate"]
    }
  },
  "linux": {
    "default": "auto",
    "templates": {
      "gnome-terminal": ["gnome-terminal", "--", "bash", "-c", "ssh -i \"{{.KeyPath}}\" -p {{.Port}} {{.User}}@{{.Host}}; exec bash"],
      "konsole": ["konsole", "-e", "bash", "-c", "ssh -i \"{{.KeyPath}}\" -p {{.Port}} {{.User}}@{{.Host}}; exec bash"],
      "xfce4-terminal": ["xfce4-terminal", "-e", "bash -c 'ssh -i \"{{.KeyPath}}\" -p {{.Port}} {{.User}}@{{.Host}}; exec bash'"],
      "alacritty": ["alacritty", "-e", "bash", "-c", "ssh -i \"{{.KeyPath}}\" -p {{.Port}} {{.User}}@{{.Host}}; exec bash"],
      "kitty": ["kitty", "sh", "-c", "ssh -i \"{{.KeyPath}}\" -p {{.Port}} {{.User}}@{{.Host}}; exec $SHELL"],
      "x-terminal-emulator": ["x-terminal-emulator", "-e", "bash", "-c", "ssh -i \"{{.KeyPath}}\" -p {{.Port}} {{.User}}@{{.Host}}; exec bash"]
    }
  }
}
```

---

## 6. Referensi & Sumber Primer

1. **Microsoft Learn: Windows Terminal Command Line Arguments**:
   - `wt.exe command line arguments`: [https://learn.microsoft.com/en-us/windows/terminal/command-line-arguments](https://learn.microsoft.com/en-us/windows/terminal/command-line-arguments)
   - Dokumen resmi parameter sub-command `new-tab`, `-p <profile>`, dan eksekusi command string.
2. **Microsoft Learn: CMD.exe & PowerShell Execution Options**:
   - `CMD /K`: Run Command and Keep Prompt Active.
   - `PowerShell -NoExit`: [https://learn.microsoft.com/en-us/powershell/module/microsoft.powershell.core/about/about_powershell_exe](https://learn.microsoft.com/en-us/powershell/module/microsoft.powershell.core/about/about_powershell_exe)
3. **AppleScript & macOS Terminal Automation Guide**:
   - Apple Developer Documentation: *Terminal Scripting Dictionary* (`Terminal.sdef`) & `do script` command.
   - iTerm2 Scripting API: [https://iterm2.com/documentation-scripting.html](https://iterm2.com/documentation-scripting.html)
4. **GNOME Developer Documentation**:
   - `gnome-terminal` man page: migrasi dari opsi usang `-e` ke separator `--` untuk eksekusi argumen biner arbitrary.
5. **KDE Konsole Manual**:
   - KDE Documentation: opsi `-e` dan `--noclose` / `--hold`.
6. **Alacritty & Kitty Documentation**:
   - Alacritty CLI flags: `-e` / `--command` specification ([https://alacritty.org](https://alacritty.org)).
   - Kitty invocation reference: `kitty [options] [program]` ([https://sw.kovidgoyal.net/kitty/invocation/](https://sw.kovidgoyal.net/kitty/invocation/)).
