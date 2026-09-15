# Transparent Lifecycle Assessment, Productive Lifespan, dan Structured Audit Breakdown

Penerapan audit multi-faktor transparan pada proses *Lifecycle Assessment* dengan formula 6-faktor terkalibrasi (termasuk deteksi usia pakai hardware / MTBF), deteksi rasio kapasitas vCPU over SSH agentless, dan persistensi rincian audit terstruktur (`lifecycle_breakdown`).

## Konteks dan Masalah

Sebelumnya, nilai *Lifecycle Score* dihitung menggunakan 4 aturan statis dengan beban CPU mutlak (`load > 8.0`) tanpa mempertimbangkan kapasitas core Host, belum mengukur saturasi kapasitas disk (`df`), belum memperhitungkan masa pakai produktif hardware (*Productive Lifespan / MTBF*), serta hanya menyajikan angka agregat tunggal (`XX/100`) dan gabungan teks catatan (*opaque/black-box*). 

Dalam tata kelola IT korporat, server yang beroperasi melampaui masa depresiasi standar (3–5 tahun) memasuki kurva peningkatan risiko kegagalan fisik komponen (*bathtub curve / MTBF degradation*). Jika sistem tetap menampilkan skor 100/100 hanya karena sistem belum mati mendadak, manajemen memiliki rasa aman semu (*false sense of security*), tidak menyiapkan disaster recovery/backup, dan tim IT kehilangan dasar objektif untuk mengajukan anggaran peremajaan (*hardware refresh*).

## Keputusan Arsitektur

1. **Pemisahan Semantik Glosarium (`CONTEXT.md`)**:
   - **Lifecycle Assessment**: Proses evaluasi multi-faktor transparan terhadap Host untuk mengukur kelayakan siklus hidup sistem melalui analisis status OS EOL, saturasi memori/CPU/disk, integritas kernel I/O, dan batas masa pakai produktif hardware.
   - **Lifecycle Score**: Nilai numerik akhir (0-100) hasil dari *Lifecycle Assessment*.
   - **Productive Lifespan**: Ambang batas masa pakai produktif hardware server (standar industri: 3–5 tahun) sebelum memasuki kurva penurunan MTBF (*Mean Time Between Failures*).
   - **Hardware Refresh**: Rekomendasi formal penggantian atau peremajaan server yang telah melampaui masa pakai produktif atau terdepresiasi penuh.

2. **Formula Audit 6-Faktor (Basis 100 Poin)**:
   - **OS Support (-30)**: Pengurangan jika distribusi Linux terdeteksi End-Of-Life (Ubuntu 14/16/18.04, Debian 7/8/9, CentOS 6/7/8, RHEL 6/7).
   - **Productive Lifespan / Hardware Age (-15 s.d. -25)**:
     - Usia $\le 5$ Tahun: **0 Poin** (dalam masa garansi/produktif standar).
     - Usia $> 5$ Tahun: **-15 Poin** (*Exceeds 5-yr productive lifecycle / MTBF risk zone*).
     - Usia $> 8$ Tahun: **-25 Poin** (*Exceeds 8-yr critical lifespan — urgent hardware refresh recommended*).
   - **Memory Pressure (-20)**: Pengurangan jika utilisasi RAM $>92\%$.
   - **CPU Saturation (-20)**: Pengurangan jika rasio beban per-core $\frac{\text{load 1m}}{\text{vCPU}} > 2.0$, mencegah false-alarm pada server berkapasitas multi-core besar.
   - **Disk Capacity (-20)**: Pengurangan jika utilisasi ruang partisi root $>90\%$.
   - **Hardware I/O (-40)**: Pengurangan jika ditemukan error I/O atau filesystem fisik pada ring buffer kernel (`dmesg`).
   - Skor minimum dipatok pada angka 0 ($\max(0, \text{score})$).

3. **Deteksi Usia Hybrid (Fisik vs Virtual Cloud)**:
   - **Server Fisik (*Bare-Metal*)**: Mengukur tanggal rilis BIOS motherboard (`/sys/class/dmi/id/bios_date`).
   - **Cloud VPS / Virtual Machine**: Vendor hypervisor disaring (QEMU, KVM, VMware, AWS, dsb.) dan usia diukur dari **OS Deployment Age** (`/etc/machine-id` timestamp), mencegah false-alarm dari tanggal BIOS default milik hypervisor cloud provider.

4. **Deteksi vCPU Portabel (Agentless Zero-Dependency)**:
   - Menyisipkan perintah `(nproc 2>/dev/null || grep -c ^processor /proc/cpuinfo 2>/dev/null || echo 1)` pada seksi 12 `SystemMetricsBatchCmd`. Berjalan instan pada distribusi modern maupun legacy tanpa dependensi paket pihak ketiga.

5. **Persistensi Terstruktur (`lifecycle_breakdown`)**:
   - Menambahkan kolom `lifecycle_breakdown TEXT DEFAULT '[]'` pada tabel `hosts` SQLite, menyimpan serialisasi JSON array `LifecycleFactor` (`name`, `status`, `score_deduction`, `detail`).
   - Menyediakan data audit transparan untuk dirender sebagai checklist audit interaktif dan kartu justifikasi peremajaan (*Hardware Refresh*) resmi di antarmuka web.

6. **Freeze Score Saat Kegagalan Koneksi**:
   - Jika inspeksi SSH mengalami kegagalan koneksi (*host down*), skor dan rincian breakdown terakhir yang valid dipertahankan (*frozen*) agar fluktuasi jaringan sementara tidak merusak profil historis kesehatan fisik server.
