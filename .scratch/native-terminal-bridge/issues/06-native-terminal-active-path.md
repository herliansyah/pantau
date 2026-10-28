# 06 — Native Terminal Active Path Navigation

Type: prototype
Status: resolved
Blocked by: 04

## Question

Bagaimana arsitektur dan antarmuka untuk mendukung pembukaan Native Terminal langsung pada direktori aktif (active working directory), baik dari tab terminal aktif (xterm via OSC 7) maupun dari antarmuka SFTP File Manager?

## Answer

1. **Sinkronisasi Path Aktif Frontend**:
   - `openNativeTerminalForTab(tabId)` memanfaatkan `getTerminalActivePath(tabId)` (hasil pelacakan OSC 7 sequence & shell title) untuk membaca direktori kerja sesi shell aktif saat ini.
   - Antarmuka SFTP File Manager menambahkan fungsi `openNativeTerminalForPath(targetPath)` dan tombol aksi *Native Terminal* di toolbar penjelajah berkas di samping tombol *Open Terminal Here*.
   - Antarmuka Global Bookmarks (`renderGlobalPinsModal`) menambahkan fungsi `openNativeTerminalFromGlobalPin(hostId, targetPath, isDir)` dan tombol aksi *Native Terminal* pada setiap pin item direktori/berkas yang otomatis bernavigasi ke path target (atau folder induk jika berupa file).
   - Nilai path dikirim sebagai payload `path` opsional ke endpoint `POST /api/terminal/native-open` (mode lokal) dan `POST /api/terminal/native-ticket` (mode remote web).

2. **Sanitasi dan Eksekusi Perintah Remote SSH**:
   - Fungsi `cleanInitialDir` membersihkan karakter kontrol, newline (`\n`, `\r`), dan null bytes untuk mencegah potensi command injection.
   - Jika path valid dan bukan `~`, perintah SSH diperluas dengan alokasi pseudo-terminal (`-t`) dan perpindahan direktori aman:
     `-t "cd '<escaped_dir>' 2>/dev/null; exec bash -l || exec sh -l"`
   - Pola tanpa evaluasi variabel lokal (`exec bash -l || exec sh -l`) menjamin kompatibilitas lintas shell dan OS klien tanpa konflik ekspansi variabel lingkungan pada cmd.exe, PowerShell, bash, maupun AppleScript.
   - *Graceful fallback*: Jika direktori target tidak ditemukan atau tidak memiliki izin akses, sesi shell tetap aktif dan berada di default home directory pengguna.
