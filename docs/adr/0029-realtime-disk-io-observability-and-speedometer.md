# 0029. Observabilitas Laju Disk I/O Real-Time dan Indikator Beban Speedometer

Observabilitas laju throughput baca/tulis disk secara berkala (bytes delta per detik) pada arsitektur agentless SSH Pantau, dilengkapi deteksi rollover reboot, penataan compact grid 4-kolom berdampingan dengan network traffic, dan pill status beban I/O.

## Konteks dan Masalah

Sebelumnya, Pantau telah memiliki observabilitas komprehensif untuk *Network Traffic* (`net_rx_speed_bps` dan `net_tx_speed_bps`) serta kapasitas statis partisi disk (`df -lPk /`). Namun untuk aktivitas disk dinamis:

1. **Ketiadaan Metrik Laju I/O Disk Real-Time (*Disk I/O Throughput Rate*)**:
   Administrator tidak dapat melihat laju throughput baca/tulis disk secara seketika (*read/write speed per second*). Lonjakan I/O (seperti penulisan data runaway pada database, proses scanning disk berat, atau pencadangan I/O intensif) sulit dideteksi kecuali disk sudah benar-benar penuh atau sistem mengalami freeze.
2. **Ketiadaan Evaluasi Kualitatif Beban Throughput**:
   Mengetahui besaran angka transfer saja belum cukup intuitif bagi operator untuk menentukan apakah kondisi aktivitas disk tersebut wajar atau berisiko (*optimal*, *aktif*, atau *beban tinggi*).
3. **Keterbatasan Tanpa Agen (*Agentless SSH Constraints*)**:
   Pengumpulan telemetri tidak boleh menginstal agen daemon baru atau bergantung pada perkakas pihak ketiga seperti `sysstat` (`iostat`), `iotop`, atau eBPF/blktrace yang belum tentu tersedia di semua target Linux (terutama legacy CentOS 6 atau distro minimal).
4. **Pencegahan Double Counting dan Noise Virtual Device**:
   Linux `/proc/diskstats` memuat seluruh partisi (`sda1`), loop devices (`loop0`), RAM disk (`ram*`), dan mapper (`dm-*`). Pembacaan mentah tanpa filtering selektif akan menghasilkan angka palsu (*double-counting* atau salah hitung).

## Keputusan Arsitektur

1. **Ekstraksi Telemetri POSIX One-Liner via `/proc/diskstats`**:
   - Mengekstrak kolom 6 (*sectors read*) dan kolom 10 (*sectors written*) langsung dari `/proc/diskstats`.
   - Menggunakan filter regex whitelist perangkat fisik utama pada POSIX `awk`:
     ```sh
     cat /proc/diskstats 2>/dev/null | awk '{if ($3 ~ /^([hsv]d[a-z]|nvme[0-9]+n[0-9]+|xvd[a-z]|mmcblk[0-9]+)$/) {r+=$6; w+=$10}} END {print (r?r:0)*512, (w?w:0)*512}'
     ```
   - Whitelist mencakup drive utama (`sd*`, `vd*`, `nvme*n*`, `xvd*`, `mmcblk*`), mengabaikan partisi (`sda1`, `nvme0n1p1`), mapper, dan loop devices.
   - Sektor dikonversikan ke byte standar dengan pengali 512 (`* 512`).

2. **Perluasan Skema SQLite & Delta Speed Calculation**:
   - Menambahkan 4 kolom baru pada tabel `hosts` dengan migrasi idempotenter (`alterAddColumn`):
     - `disk_read_bytes INTEGER DEFAULT 0`
     - `disk_write_bytes INTEGER DEFAULT 0`
     - `disk_read_speed_bps INTEGER DEFAULT 0`
     - `disk_write_speed_bps INTEGER DEFAULT 0`
   - Laju per detik dihitung dengan membagi selisih bytes akumulatif (`deltaRead`, `deltaWrite`) dengan durasi detik yang terlewati sejak `LastInspected`.

3. **Reboot Protection (Counter Rollover Guard)**:
   - Jika server target melakukan reboot, counter `/proc/diskstats` akan ter-reset ke nol (`deltaRead < 0` atau `deltaWrite < 0`).
   - Sistem secara aman mendeteksi kondisi ini, menyetel kecepatan ke `0 bps` pada siklus tersebut untuk menghindari perhitungan nilai negatif palsu, dan mencatat nilai counter baru sebagai baseline berikutnya.

4. **Indikator Kualitatif Beban Throughput Disk (Status Pill)**:
   - Dihitung dari total throughput $\text{Read} + \text{Write}$:
     - 🟢 **Optimal** ($< 10\text{ MB/s}$): Aktivitas I/O disk normal dan santai.
     - 🟡 **Aktif** ($10\text{ MB/s} - 80\text{ MB/s}$): Aktivitas transfer/baca-tulis kerja wajar.
     - 🔴 **Beban I/O Tinggi / Busy** ($\ge 80\text{ MB/s}$): Disk berada pada utilisasi tinggi atau mendekati bottleneck I/O.

5. **Tata Letak UI Harmonis & Hemat Ruang**:
   - **Host Card**: Memadukan Network Traffic (⬇️/⬆️) dan Disk I/O (📖/✍️) dalam 1 baris grid 4-kolom berdampingan dengan pill status dinamis (`● Optimal`), menjaga konsistensi tinggi card.
   - **Table View**: Kolom kecepatan menggabungkan baris laju Network (biru `#38bdf8`) dan Disk I/O (ungu `#c084fc`).
   - **Host Detail Modal**: Menghadirkan dedicated card **Live Disk I/O Rate** lengkap dengan pill status beban, laju baca/tulis aktif, dan total akumulasi bytes.
