# Terminal Presets and Split-Pane Observability

Pantau mengadopsi fitur Terminal Preset dan Split-Pane Web Terminal untuk mempercepat observabilitas interaktif multi-host langsung dari antarmuka web tanpa mengharuskan administrator mengetik ulang perintah diagnostik berulang secara manual.

### Model Data Hybrid
Terminal Preset disimpan dalam basis data SQLite melalui tabel `terminal_presets` dengan relasi opsional ke `hosts` (`host_id NULL` menandakan preset global yang berlaku di seluruh mesin, sedangkan `host_id` terisi mengkhususkan preset untuk Host tertentu). Sistem menyertakan bibit preset esensial bawaan (`htop`/`top`, `docker stats`, dan `journalctl -n 100 -f`). Konfigurasi dapat dikelola secara terpusat melalui Settings maupun disimpan langsung (*in-place*) dari bilah alat terminal saat sesi sedang aktif.

### Eksekusi Shell Injection dan Pre-Flight Non-Blocking
Perintah dijalankan melalui injeksi shell interaktif pada sesi WebSocket PTY yang sudah terbentuk, dengan opsi pembungkusan (*wrapper*) deteksi ketersediaan binary remote secara non-blocking. Pendekatan ini mempertahankan sesi shell tetap aktif saat perintah monitoring selesai atau dihentikan pengguna (menghindari pemutusan PTY mendadak), serta memberikan petunjuk perintah instalasi ketika utilitas diagnostik belum terpasang pada Host target.

### Split-Pane Multi-Terminal
Antarmuka Workspace Modal mendukung pembagian layar terminal menjadi dua panel berdampingan (*side-by-side split pane*) dengan kendali via tombol toolbar maupun pintasan keyboard (`Alt+\`). Masing-masing panel mempertahankan sesi WebSocket dan instance xterm.js independen, dengan fleksibilitas memilih Host target yang sama atau berbeda guna memfasilitasi perbandingan performa lintas-server secara simultan (misalnya master vs replica).
