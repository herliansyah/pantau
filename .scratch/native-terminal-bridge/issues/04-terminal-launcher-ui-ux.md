# 04 — Native Terminal Launcher UI & UX Integration

Type: prototype
Status: resolved
Blocked by: 01, 03

## Question

Bagaimana bentuk antarmuka pengguna (UI) dan alur interaksi tombol 'Buka di Native Terminal' pada toolbar Terminal Tab (ikon popout `↗`) dan menu aksi Host Card, serta bagaimana antarmuka membedakan penanganan secara mulus antara mode akses lokal (langsung memunculkan jendela terminal di layar) dan mode peramban jarak jauh (menampilkan modal download script / copy command)?

## Answer

1. **Titik Akses Antarmuka (Placement)**:
   - **Terminal Dock Toolbar**: Menambahkan tombol aksi `↗` (`btnTerminalDockNative`) di bilah header jendela Terminal Dock di samping tombol maximize (`⛶`). Mengklik tombol ini meluncurkan native terminal untuk Host pada tab terminal aktif.
   - **Host Card Dropdown**: Menambahkan entri menu `↗ Buka di Native Terminal` pada dropdown `⋮` (*More Actions*) di setiap kartu Host pada dashboard utama.

2. **Deteksi Mode Adaptif (Lokal vs Remote Web)**:
   - Frontend memverifikasi hostname browser (`window.location.hostname`).
   - **Mode Lokal (`localhost` / `127.0.0.1` / `::1`)**: Mengirim `POST /api/terminal/native-open`, backend Go langsung mengeksekusi proses terminal (`cmd.Start()`), dan UI memunculkan notifikasi Toast: *"Native terminal diluncurkan di desktop"*.
   - **Mode Remote Web**: Mengirim `POST /api/terminal/native-ticket` dan menampilkan modal dialog *Native Terminal Launcher*:
     - Tombol unduh otomatis berkas skrip sekali pakai sesuai deteksi OS klien (`.cmd` / `.command` / `.sh`) dengan panduan pembersihan otomatis.
     - Tab alternatif *Salin Perintah Langsung* (`curl ... | bash` atau `irm ... | iex`) dengan tombol *One-Click Copy Command*.
