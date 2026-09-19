# 0022. Inspection Guard, Anti-Flood Cooldown, dan Proteksi Hung NFS Storage

Pengamanan eksekusi inspeksi Host melalui pembatasan laju manual (cooldown), penolakan eksplisit konkurensi (in-flight conflict), pembatasan konkurensi inspeksi massal, isolasi filesystem lokal (`df -l`), dan batas waktu absolut (hard timeout budget).

## Konteks dan Masalah

Pantau mengandalkan inspeksi berkala berbasis SSH agentless untuk memvalidasi Desired State dan mengumpulkan metrik Host. Terdapat beberapa skenario operasional yang dapat membebani atau membekukan sistem jika tidak dijaga:
1. **Spam & Flooding Tombol Manual**: Administrator atau script dapat memicu tombol "⚡ Inspect" berulang kali dalam hitungan detik, menciptakan tumpukan koneksi SSH dan lonjakan beban CPU di server Host.
2. **Tabrakan Inspeksi Konkuren (In-Flight)**: Ketika sebuah inspeksi sedang berjalan, request inspeksi baru sebelumnya di-skip secara diam-diam (*silent skip*) dengan HTTP 200 palsu, tanpa indikasi jelas ke pengguna bahwa request tidak dijalankan.
3. **Penyakit Klasik Linux: Hung / Stale NFS Mount**: Perintah `df` standar pada Linux dapat membeku (*hang*) tanpa batas waktu dalam status kernel `D` (*uninterruptible sleep*) jika terdapat mount NFS/CIFS/network storage yang bermasalah. Hal ini menyebabkan sesi SSH gantung dan proses menumpuk di Host.
4. **Banjir Koneksi Inspeksi Massal ("Inspect All")**: Menekan tombol "Inspect All" sebelumnya memicu koneksi SSH serentak ke seluruh Host tanpa batas worker, berisiko menguras socket dan memicu throttle firewall.

## Keputusan Arsitektur

1. **Anti-Flood Cooldown Manual (15 Detik)**:
   - Mempertahankan interval background worker otomatis sesuai konfigurasi `poll_interval_sec` (default 5 menit).
   - Memanfaatkan kolom `h.LastInspected` di database: jika endpoint manual `POST /api/hosts/{id}/inspect` dipanggil dengan selisih waktu `< 15 detik`, sistem menolak request dengan **HTTP 429 Too Many Requests** dan menyertakan sisa detik tunggu.

2. **Eksplisit In-Flight Concurrency Guard (HTTP 409 Conflict)**:
   - Jika sebuah Host sedang menjalani proses inspeksi (`s.inspector.IsInspecting(hostID)`), request baru langsung ditolak dengan **HTTP 409 Conflict**: *"Inspeksi untuk host ini sedang berjalan, harap tunggu hingga selesai."*
   - Di frontend UI, tombol inspect langsung berubah menjadi status berputar (*spinner*) dan *disabled*, serta menampilkan *toast warning* kuning informatif (bukan crash error).

3. **Proteksi Hung NFS / Stale Network Mounts**:
   - Pada `SystemMetricsBatchCmd`: menggunakan `(timeout -k 2s 5s df -lPk / 2>/dev/null || df -lPk / 2>/dev/null)`. Flag `-l` (`--local`) secara ketat melompati seluruh remote filesystem (NFS, CIFS, SMB) sehingga pembacaan metrik root tidak akan tersandera oleh network storage yang rusak.
   - Pada evaluasi aturan disk (`rule.Kind == "disk"`): perintah dibungkus `timeout -k 2s 5s df -Pk <target>`. Jika exit code bernilai 124 (timeout), Pantau mengklasifikasikannya sebagai *drift* dengan pesan transparan: *"Storage/mount point timed out (possible hung NFS or storage deadlock)"*.

4. **Total Timeout Budget (45 Detik)**:
   - Setiap pemanggilan `InspectHost` dibatasi oleh `context.WithTimeout(..., 45*time.Second)`.
   - Jika batas waktu 45 detik terlampaui, koneksi SSH runner langsung diputus paksa di sisi Pantau (`runner.Close()`), membatalkan seluruh operasi gantung, dan mencatat status Host sebagai *"down"* dengan rincian *"Inspection timed out after 45s"*.

5. **Worker Pool Konkurensi Terbatas untuk "Inspect All"**:
   - Pada endpoint `POST /api/hosts/inspect-all`, inspeksi dieksekusi melalui worker pool berbasis Go buffered channel berkapasitas 5 (`sem := make(chan struct{}, 5)`).
   - Menghindari lonjakan koneksi serentak ke switch/firewall dan menghemat konsumsi memori/goroutine Pantau.
