# Taksonomi dan Evaluator Aturan Desired State (Drift Engine)

Standarisasi taksonomi 7 jenis aturan Desired State (`container`, `service`, `port`, `disk`, `cron`, `backup`, `process`), aturan sintaks nilai target/ekspektasi, serta penanganan evaluasi deviasi dan Root Cause Excerpt.

## Konteks dan Masalah

Sebelum keputusan ini, Pantau memiliki kesenjangan (*mismatch*) antara tiga komponen inti Drift Engine:
1. **Inkonsistensi Form UI vs Evaluator**: Form dialog "Add Desired State Rule" menampilkan opsi `port` dan `process` yang tidak memiliki implementasi evaluator di backend (`evaluateRule`), sehingga evaluasi selalu lolos secara semu (`unknown/ok`).
2. **Ketiadaan Opsi `cron` dan `backup` di UI Manual**: Generator *1-Click Baseline* dan backend mendukung aturan `cron` dan `backup`, tetapi kedua opsi ini tidak ada di elemen dropdown form penambahan manual.
3. **Klaim Dokumentasi yang Keliru**: Dokumentasi pengguna menyatakan *1-Click Baseline* memindai *listening ports*, padahal generator baseline hanya berfokus pada disk, container, service, dan cron. Tidak ada tabel referensi baku yang mendefinisikan sintaks nilai `Expected` yang valid untuk setiap jenis aturan.

## Keputusan Arsitektur

1. **Standarisasi Taksonomi 7 Jenis Aturan (`Kind`)**:
   Pantau membakukan 7 jenis aturan Desired State kanonikal yang didukung penuh di antarmuka web dan evaluator backend:
   - `container`: Memantau status kontainer Docker melalui `docker inspect`.
   - `service`: Memantau keaktifan daemon Linux via polyglot `systemctl is-active` dan fallback `service status`.
   - `port`: Memantau status keterbukaan listening TCP port pada sistem host melalui `ss -tlpn` / `netstat -tlpn`.
   - `disk`: Memantau batas persentase utilisasi mount point partisi melalui `df -Pk` dengan batas timeout 5 detik (proteksi hung NFS).
   - `cron`: Memantau keberadaan entri perintah terjadwal pada crontab user melalui `crontab -l`.
   - `backup`: Memantau keberadaan fisik, ukuran byte (> 0 byte), dan batas toleransi usia berkas cadangan melalui `stat -c "%s %Y"`.
   - `process`: Memantau keaktifan proses sistem kustom (non-daemon / non-service) melalui `pgrep -fl` dan fallback `ps -ef`.

2. **Sintaks Baku `Target` dan `Expected`**:
   | Jenis (`Kind`) | Sintaks `Target` | Sintaks `Expected` | Kondisi Normal (`ok`) | Kondisi Deviasi (`drift`) |
   | :--- | :--- | :--- | :--- | :--- |
   | `container` | Nama kontainer (`nginx`, `app`) | `running` | State kontainer adalah running | Kontainer tidak ada, exited, crash loop |
   | `service` | Nama service (`mariadb`, `redis`) | `active` | Service aktif | Service inactive, failed |
   | `port` | Nomor port atau `ip:port` (`80`, `3306`) | `listening` (atau `closed`) | Port berstatus listening | Port tertutup / proses mati |
   | `disk` | Mount point (`/`, `/var`, `/data`) | `<N%` (contoh: `<85%`) | Utilisasi < N% dan IO merespons | Utilisasi >= N% atau IO timeout (hung NFS) |
   | `cron` | Kata kunci / substring perintah | `configured` | Ditemukan dalam `crontab -l` | Baris tidak ditemukan di crontab |
   | `backup` | Path absolut file (`/backups/db.sql.gz`) | `fresh_<N>h` (contoh: `fresh_24h`) | Berkas ada, > 0 B, usia <= N jam | Berkas hilang, 0 B, atau kedaluwarsa |
   | `process` | Nama atau pola proses (`celery`, `worker`) | `running` (atau `stopped`) | PID proses ditemukan aktif | Proses mati / tidak ditemukan |

3. **Cakupan Otonom 1-Click Baseline**:
   Generator baseline otomatis (`GenerateBaseline`) secara sengaja membatasi pemindaian pada 4 elemen fondasi utama: partisi root `/`, kontainer Docker yang sedang aktif, service database/web terpasang, dan entri crontab. Pemindaian port listening publik tidak dimasukkan ke dalam baseline otomatis untuk menghindari *alert fatigue* akibat port ephemeral atau listening lokal temporer.

4. **UX Interaktif pada Dialog Form Penambahan Rule**:
   Antarmuka dialog penambahan aturan dilengkapi petunjuk placeholder dan nilai *default expected* dinamis sesuai jenis aturan yang dipilih untuk mengeliminasi kesalahan input sintaks di batas interaksi pengguna.
