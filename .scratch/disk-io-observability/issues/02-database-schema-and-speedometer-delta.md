# 02 — Desain Skema Database & Kalkulasi Delta Rate Speedometer

Type: grilling
Status: resolved
Blocked by: 01

## Question

Kolom apa saja yang perlu ditambahkan ke tabel `hosts` SQLite (misal `disk_read_bytes`, `disk_write_bytes`, `disk_read_speed_bps`, `disk_write_speed_bps`), bagaimana kalkulasi delta rate per detik saat jeda inspeksi bervariasi, dan bagaimana menangani kondisi reboot (counter reset)?

## Answer

### 1. Perubahan Skema Database SQLite
Tambahkan 4 kolom baru ke tabel `hosts` dengan migrasi idempotenter (`alterAddColumn`):
- `disk_read_bytes INTEGER DEFAULT 0` — akumulasi sektor baca dikonversi ke bytes.
- `disk_write_bytes INTEGER DEFAULT 0` — akumulasi sektor tulis dikonversi ke bytes.
- `disk_read_speed_bps INTEGER DEFAULT 0` — laju baca (bytes per second).
- `disk_write_speed_bps INTEGER DEFAULT 0` — laju tulis (bytes per second).

### 2. Algoritma Kalkulasi Delta Rate
Formula kalkulasi mengikuti pola `NetRxSpeedBps` yang sudah teruji di `inspector.go`:
```go
if h.LastInspected != nil && h.DiskReadBytes > 0 {
    elapsed := time.Since(*h.LastInspected).Seconds()
    if elapsed > 0 {
        deltaRead := newReadBytes - h.DiskReadBytes
        deltaWrite := newWriteBytes - h.DiskWriteBytes

        if deltaRead >= 0 {
            h.DiskReadSpeedBps = int64(float64(deltaRead) / elapsed)
        } else {
            // Host rebooted / counter wrapped
            h.DiskReadSpeedBps = 0
        }

        if deltaWrite >= 0 {
            h.DiskWriteSpeedBps = int64(float64(deltaWrite) / elapsed)
        } else {
            // Host rebooted / counter wrapped
            h.DiskWriteSpeedBps = 0
        }
    }
}
h.DiskReadBytes = newReadBytes
h.DiskWriteBytes = newWriteBytes
```

### 3. Penanganan Anomali & Reboot
- **Host Reboot / Counter Rollover**: Saat server reboot, counter `/proc/diskstats` di-reset ke nol oleh kernel Linux (`deltaRead < 0` atau `deltaWrite < 0`). Sistem mereset speed ke `0 bps` pada siklus tersebut untuk mencegah spike kalkulasi nilai negatif, lalu menyimpan nilai counter baru sebagai baseline siklus inspeksi selanjutnya.
- **Inspeksi Pertama**: Saat `LastInspected == nil` atau nilai akumulasi awal adalah 0, speed diinisialisasi sebagai `0 bps`.

