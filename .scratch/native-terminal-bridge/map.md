# Map: Native Terminal Bridge

Status: resolved

## Destination

Menghadirkan fitur "Buka di Native Terminal" pada Pantau untuk meluncurkan terminal bawaan OS pengguna (Windows Terminal, GNOME Terminal, iTerm2, Alacritty, dll.) yang langsung terhubung ke target Host tanpa perlu login/memasukkan password ulang, baik saat Pantau berjalan lokal di mesin admin maupun saat diakses via remote peramban web.

## Notes

- Domain: Native Terminal Integration, Agentless SSH, Ephemeral Credentials, Cross-Platform Process Spawning, Web-to-Desktop Handoff.
- Skills to consult: `/grilling`, `/domain-modeling`, `/research`, `/prototype`, `/codebase-design`.
- Standing preferences: Prinsip Ponytail (Lazy senior dev) — zero client-side installation untuk mode remote web, gunakan native OS launcher & OpenSSH bawaan tanpa ketergantungan tool pihak ketiga (`sshpass`), dan manfaatkan koneksi SSH Go yang sudah ada untuk injeksi credential sementara.

## Decisions so far

<!-- the index — one line per closed ticket: enough to judge relevance, then zoom the link for the detail the ticket holds -->
- **01 OS Terminal Matrix & Execution Templates**: Gunakan kaskade deteksi `exec.LookPath` (Windows: `wt` > `powershell` > `cmd`; macOS: `iTerm.app` > `Terminal.app`; Linux: `$TERMINAL` > `x-terminal-emulator` > `$PATH`) dan pola *Shell Chaining* (`exec bash`, `cmd /k`, `powershell -NoExit`) dengan detached process (`Setpgid`/`CREATE_NEW_PROCESS_GROUP`) agar jendela terminal tetap interaktif dan pesan error terlihat saat SSH terputus. ([01-os-terminal-matrix.md](issues/01-os-terminal-matrix.md), [docs/research/01-os-terminal-matrix.md](../../docs/research/01-os-terminal-matrix.md))
- **02 Ephemeral SSH Key Injection Architecture**: Pembuatan ED25519 keypair in-memory tanpa menyentuh disk Pantau, injeksi atomik via SSH runner ke `~/.ssh/authorized_keys` dengan label metadata komentar (`pantau-ephemeral-<id>-<expiry>`) untuk kompatibilitas daemon legacy, pembersihan dua tingkat (*proactive Go timer* per-sesi dan *reactive sweep* via `awk` ber-grace period 10 menit anti clock drift), serta memanfaatkan sifat OpenSSH yang mempertahankan sesi shell aktif meski kunci telah dibersihkan. ([02-ephemeral-key-injector.md](issues/02-ephemeral-key-injector.md), [docs/research/02-ephemeral-key-injector.md](../../docs/research/02-ephemeral-key-injector.md))
- **03 Remote Web Launcher Script & Command Generator**: Token unduhan sekali pakai (*burn-after-read*) dengan TTL 60 detik di RAM, berkas launcher mandiri lintas OS (`.cmd` dengan `icacls` khusus user aktif di Windows, `.command`/`.sh` dengan `trap` di macOS/Linux) yang otomatis menghapus private key dan berkas script dirinya sendiri (*self-delete*) saat terminal selesai, serta opsi modal streaming *One-Click Copy Command*. ([03-remote-bridge-launcher.md](issues/03-remote-bridge-launcher.md))
- **05 Terminal Launcher Settings & Custom Command Template**: Konfigurasi hybrid (pengaturan bawaan di server dan override per-klien di `localStorage`), validasi pra-penyimpanan ketat (*Strict Pre-flight Check* via `LookPath` dan validasi placeholder `%cmd%`), serta mekanisme *graceful fallback* ke terminal bawaan OS dengan pemberitahuan toast jika executable kustom tidak ditemukan di runtime. ([05-launcher-settings-customization.md](issues/05-launcher-settings-customization.md))
- **04 Native Terminal Launcher UI & UX Integration**: Tombol aksi `↗` di toolbar header Terminal Dock dan entri menu pada dropdown `⋮` Host Card, dengan logika adaptif yang langsung memicu peluncuran proses di desktop lokal (*Local Mode*) atau memunculkan modal *Native Terminal Launcher* (*Remote Mode*) dengan opsi unduh script auto-clean atau salin perintah streaming. ([04-terminal-launcher-ui-ux.md](issues/04-terminal-launcher-ui-ux.md))
- **06 Native Terminal Active Path Navigation**: Penambahan parameter `path` ke payload `/api/terminal/native-open` dan `/api/terminal/native-ticket`, tombol *Native Terminal* di toolbar SFTP File Manager, integrasi path aktif `getTerminalActivePath(tabId)` pada toolbar Terminal Dock, serta bootstrap remote shell `cd '<escaped>' 2>/dev/null; exec bash -l || exec sh -l` ber-fallback aman ke `$HOME`. ([06-native-terminal-active-path.md](issues/06-native-terminal-active-path.md))

## Not yet specified

- Skenario penanganan Host target yang memberlakukan MFA/2FA berbasis SSH (misal Google Authenticator PAM / hardware FIDO2 key) pada saat peluncuran native terminal.
- Penanganan auto-reconnect atau session resumption jika koneksi native terminal terputus di tengah jalan.
- Dukungan custom jump host bertingkat (bastion proxy chaining) jika Host target hanya bisa diakses lewat gateway privat berlapis.

## Out of scope

- Membangun aplikasi native desktop wrapper (Electron/Tauri) khusus hanya untuk menangkap protocol handler.
- Mendistribusikan biner executable pihak ketiga (seperti `sshpass` atau `putty.exe`) ke dalam bundle Pantau.
- Mengirimkan kata sandi SSH akun Host secara mentah (plain text) ke clipboard atau baris perintah OS klien.
