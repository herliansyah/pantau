# Global Multi-Tab Terminal Dock dan Session Lifecycle

Pengembangan antarmuka terminal Pantau dari modal terminal per-host menjadi Global Multi-Tab Terminal Dock yang mendukung banyak tab dinamis, penamaan kustom instan, persistensi sesi saat diminimalkan, dan navigasi keyboard terintegrasi.

Keputusan arsitektur ini diambil berdasarkan trade-off teknis dan operasional berikut:

1. **Multi-Tab Dock vs Floating Window**: Memilih paradigma bilah tab horizontal (*tabbed dock*) dengan kontrol minimize (melipat ke *floating pill* di pojok kanan bawah) dan maximize (*full-viewport*) alih-alih sistem *floating window* gaya desktop. Paradigma tab menjaga kesederhanaan layout, responsif terhadap berbagai resolusi layar, dan mencegah tumpang-tindih jendela yang sulit dikelola pada aplikasi web pemantauan.
2. **Global Persistent Lifespan**: Terminal Dock berada pada level global aplikasi Pantau, bukan terisolasi di dalam modal host tertentu. Sesi WebSocket dan PTY remote tetap aktif di latar belakang saat dock di-minimize, memungkinkan administrator meninjau metrik dashboard tanpa memutus proses yang sedang berjalan.
3. **Pemberian Nama dan Seleksi Host**: Tab baru dibuat melalui dropdown instan pada tombol `+` dengan default nama Host. Judul tab dapat diganti secara langsung di tempat (*inline rename*) melalui klik ganda (*double-click*) pada label tab untuk membedakan konteks tugas (misal: log tailing, migrasi basis data). Klik aksi terminal pada kartu Host akan mengarahkan fokus ke tab yang sudah aktif untuk host tersebut guna mencegah duplikasi sesi yang tidak disengaja.
4. **Integrasi Split-Pane dan Pintasan Keyboard**: Fitur *Split-Pane* (`Alt+\`) dipertahankan di dalam tab yang sedang aktif untuk perbandingan berdampingan. Navigasi antar-tab mendukung pintasan keyboard `Alt + 1` hingga `Alt + 9` dan `Alt + W` untuk menutup tab tanpa mengganggu penanganan tombol shell pada xterm.js.
5. **Pemulihan Metadata Sesi**: Metadata tab (daftar host dan judul kustom) disimpan secara lokal pada `localStorage` sehingga struktur sesi dapat dipulihkan dengan opsi penyambungan ulang (*reconnect*) jika peramban tidak sengaja disegarkan (*reload*).
