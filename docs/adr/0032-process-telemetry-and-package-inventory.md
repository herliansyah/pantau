# 0032. Observabilitas Process Telemetry dan Package Inventory Agentless

Memisahkan pemantauan konsumsi sumber daya waktu-nyata (Process Telemetry / Resource Hog) dan audit inventaris paket perangkat lunak terpasang (Package Inventory & Dormant Package) secara agentless, aman, dan kompatibel lintas distribusi Linux.

## Konteks dan Masalah

Administrator server membutuhkan visibilitas mendalam atas kondisi sistem terkini untuk menjawab dua pertanyaan utama:
1. **Siapa biang kerok pemakan sumber daya saat ini?** (Proses aktif mana yang menghabiskan CPU, RAM fisik/RSS, atau memicu swap thrashing).
2. **Aplikasi apa saja yang terpasang di sistem dan apakah masih dibutuhkan?** (Berapa konsumsi ruang penyimpanan disk, kapan dipasang, apakah paket tersebut bawaan distro atau dipasang manual oleh admin, serta mendeteksi aplikasi/layanan yang terbengkalai atau tidak pernah aktif).

Tantangan teknis di lingkungan server Linux *agentless*:
- Perintah audit paket (`dpkg`, `rpm`) memproses ribuan data yang memakan waktu ~1–2 detik dan I/O disk. Menjalankannya pada setiap interval inspeksi normal (30–60 detik) akan membebani server target.
- Sebagian besar server Linux mematikan pembaruan waktu akses file (`noatime`/`relatime`), sehingga tanggal akses binary `/usr/bin/*` tidak dapat diandalkan untuk mendeteksi kapan aplikasi terakhir dipakai.
- Perintah standar `ps` tidak menyediakan kolom penggunaan memori Swap per proses.
- Tindakan destruktif (seperti `apt remove` otomatis via tombol web) berisiko memicu kerusakan sistem akibat penghapusan dependensi berantai (*cascading broken dependencies*).

## Keputusan Arsitektur

1. **Pemisahan Konseptual: Process Telemetry vs Package Inventory**:
   - **Process Telemetry (Runtime / Resource Hog)**: Mengambil cuplikan 5 proses teratas pemakan CPU, RAM fisik (RSS), dan Swap secara berkala pada setiap siklus inspeksi normal. Digabungkan langsung ke dalam `SystemMetricsBatchCmd` berlatensi rendah (<20 ms).
   - **Package Inventory (Static / Storage)**: Mengambil daftar paket terpasang secara *lazy/on-demand* (saat tab Packages dibuka atau saat tombol *Rescan* diklik, dengan auto-refresh terjadwal 24 jam).

2. **Deteksi Polyglot Multi-Distro Tanpa Dependensi**:
   - Menggunakan deteksi CLI bawaan sistem remote tanpa memasang agen atau pustaka eksternal:
     - Debian/Ubuntu: `dpkg-query` + `apt-mark showmanual`.
     - RHEL/CentOS/AlmaLinux/Rocky: `rpm -qa --qf` + `dnf/yumdb`.
     - Alpine: `apk info -v -s` + `/etc/apk/world`.
     - Arch: `pacman -Qqe` + `pacman -Qi`.

3. **Ekstraksi Swap per Proses Kondisional**:
   - Ekstraksi Swap per proses hanya dipicu saat `SwapUsedBytes > 0` menggunakan *one-liner awk* cepat membaca baris `VmSwap:` dari `/proc/*/status` (hanya belasan milidetik), mengeliminasi overhead saat sistem tidak mengalami penukaran memori virtual.

4. **Klasifikasi Paket dan Heuristik Dormant Package**:
   - Paket diklasifikasikan menjadi **Manual Install** (dipasang eksplisit oleh admin) dan **Base System** (bawaan instalasi distro/dependensi inti).
   - Paket Manual Install berstatus daemon/service ditandai sebagai **Dormant Package** jika unit `systemd` terkait berada dalam status `inactive`, `failed`, atau `disabled` selama kurun waktu observasi.

5. **Penyimpanan SQLite dan Strategi Sinkronisasi Bersih**:
   - Data Resource Hog disimpan sebagai string JSON pada tabel `hosts` (`top_cpu_processes`, `top_ram_processes`, `top_swap_processes`).
   - Data Package Inventory disimpan dalam tabel terpisah `host_packages` dengan skema `(id, host_id, name, version, size_bytes, install_epoch, is_manual, service_name, service_status, last_scanned_at)`.
   - Sinkronisasi paket menggunakan pola *Atomic Host-Scoped Replace* (`DELETE FROM host_packages WHERE host_id = ?` disusul batch insert) untuk memastikan paket yang telah dihapus di server langsung bersih dari basis data.

6. **Desain Antarmuka Pengguna (UI/UX)**:
   - **Tab Overview**: Ditambahkan widget *"Top Resource Consumers"* (Top 5 CPU, Top 5 RAM, Top 5 Swap).
   - **Tab Packages Baru**: Tab khusus pada Workspace Modal dengan bilah pencarian instan (*instant search*), penyaringan kategori (`[Semua]`, `[Manual Install]`, `[Dormant / Inactive]`), pengurutan ukuran disk, dan tombol *"Rescan Packages"*.
   - **Prinsip Keselamatan Operasional**: Tindakan perbaikan tidak menyediakan tombol hapus destruktif di web UI, melainkan menyediakan tombol navigasi cepat ke *Terminal Dock* Pantau dan pembuatan aturan verifikasi otomatis (*Desired State Rules*).

## Konsekuensi

- **Efisiensi Beban Remote**: Nol overhead tambahan pada inspeksi rutin untuk inventarisasi paket; CPU server target tetap dingin.
- **Transparansi Sumber Daya**: Administrator langsung mengetahui proses mana yang menyebabkan beban tinggi atau kebocoran memori (RAM & Swap) secara real-time.
- **Kebersihan Server (Server Hygiene)**: Memudahkan identifikasi software lama yang menghabiskan ruang disk dan tidak lagi dijalankan.
- **Keamanan Terjamin**: Menghindari risiko crash sistem akibat eksekusi penghapusan paket otomatis tanpa pengawasan CLI terminal.
