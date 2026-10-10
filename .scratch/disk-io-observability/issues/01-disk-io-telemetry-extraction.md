# 01 — Format Pengambilan Telemetri Disk I/O Lintas Kernel & Distro

Type: grilling
Status: resolved
Blocked by:

## Question

Bagaimana format one-liner paling portabel untuk mengekstrak total bytes read & write dari Linux target tanpa ketergantungan pada tool eksternal seperti `iostat` atau `sysstat`, serta bagaimana filter disk drive utama (menghindari loop devices, dm/mapper, cdrom)?

## Answer

Gunakan parsing langsung terhadap `/proc/diskstats` dengan filter regex whitelist pada `awk` standar:
```sh
cat /proc/diskstats 2>/dev/null | awk '{if ($3 ~ /^([hsv]d[a-z]|nvme[0-9]+n[0-9]+|xvd[a-z]|mmcblk[0-9]+)$/) {r+=$6; w+=$10}} END {print (r?r:0)*512, (w?w:0)*512}'
```

**Rasional Keputusan**:
1. **Zero External Dependency**: Hanya bergantung pada `cat` dan `awk` (standar POSIX) dan file `/proc/diskstats` yang ada di Linux kernel 2.6 hingga 6.x+, tanpa membutuhkan paket `sysstat` (`iostat`).
2. **Whitelist Device Utama**: Pola regex `^([hsv]d[a-z]|nvme[0-9]+n[0-9]+|xvd[a-z]|mmcblk[0-9]+)$` menyaring drive fisik/virtual utama (SATA/SCSI/IDE `sd*`/`hd*`, VirtIO `vd*`, NVMe `nvme*n*`, Xen `xvd*`, MMC `mmcblk*`), secara otomatis mengabaikan:
   - Partisi anak (`sda1`, `nvme0n1p1`) sehingga tidak terjadi perhitungan ganda (*double-counting*).
   - Loop devices snap/squashfs (`loop*`), RAM disk (`ram*`, `zram*`), optical (`sr*`), dan mapper (`dm-*`).
3. **Konversi Sektor ke Byte**: Kernel Linux mendefinisikan kolom 6 (sectors read) dan kolom 10 (sectors written) dalam unit standar 512-byte sectors (`* 512`), menghasilkan nilai mutlak read/write bytes yang presisi.
4. **Fallback Aman**: Jika file kosong atau host tidak memiliki disk yang cocok (misal containerized tanpa diskstats), menghasilkan output `0 0` tanpa error.

