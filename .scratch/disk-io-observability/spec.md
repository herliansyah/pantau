# Spesifikasi: Disk I/O & Speedometer Observability

Status: ready-for-agent

## Deskripsi
Fitur pengamatan laju baca/tulis disk secara real-time (Speedometer delta/detik & akumulasi bytes) berbasis arsitektur agentless SSH Pantau, melengkapi pemantauan Live Network Traffic yang sudah ada.

## Keputusan Arsitektur
Sesuai hasil wayfinding di `.scratch/disk-io-observability/map.md`:
1. **One-liner Posix Awk**: Menggunakan `/proc/diskstats` dengan filter regex whitelist perangkat fisik utama (`sd*`, `vd*`, `nvme*n*`, `xvd*`, `mmcblk*`) dikali 512 bytes sektor.
2. **Database SQLite**: Menambahkan 4 kolom (`disk_read_bytes`, `disk_write_bytes`, `disk_read_speed_bps`, `disk_write_speed_bps`) dengan migrasi aman `alterAddColumn`.
3. **Reboot Protection**: Reset speed ke 0 bps jika pembacaan counter lebih rendah dari sebelumnya.
4. **UI Dashboard**: Grid 4 kolom berdampingan pada Host Card dan dedicated card pada Host Detail Modal.
