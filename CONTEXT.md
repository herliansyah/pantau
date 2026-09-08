# Pantau

Sistem pemantauan dan manajemen infrastruktur server berbasis agentless SSH dengan verifikasi kondisi seharusnya (desired state), diagnosis deviasi otomatis, dan manajemen file/terminal.

## Language

**Host**:
Mesin server fisik atau virtual (VPS) yang dikelola dan dipantau melalui koneksi SSH.
_Avoid_: Node, instance, machine

**Desired State**:
Kumpulan aturan dan ekspektasi yang didefinisikan untuk sebuah Host (misal: service harus aktif, disk di bawah ambang batas, container tertentu harus running).
_Avoid_: Baseline, blueprint, config template

**Actual State**:
Kondisi riil sebuah Host yang didapatkan dari hasil pembacaan inspeksi berkala.
_Avoid_: Current status, telemetry snapshot

**Inspection**:
Proses pengambilan Actual State dari Host secara periodik melalui koneksi SSH tanpa menginstal agen di target.
_Avoid_: Polling, health check run, telemetry scrape

**Drift**:
Ketidaksesuaian atau deviasi antara Actual State dan Desired State pada Host.
_Avoid_: Mismatch, divergence, error state

**Root Cause Excerpt**:
Potongan log diagnostik terakhir (misal: exit code, potongan pesan error log atau journalctl) yang diambil otomatis saat Drift terdeteksi.
_Avoid_: Crash dump, debug output

**Lifecycle Score**:
Nilai kelayakan sebuah Host (0-100) berdasarkan status dukungan OS (EOL), beban sumber daya jangka panjang, dan indikasi kegagalan perangkat keras.
_Avoid_: Server grade, health rating

**Backup Freshness**:
Status kevalidan backup berdasarkan keberadaan file di path tujuan, timestamp perubahan terbaru (recency), dan ukuran file yang wajar (> 0 byte).
_Avoid_: Backup validation, dump check

**Notification Channel**:
Saluran pengiriman peringatan saat terdeteksi Drift (seperti Telegram bot, webhook, atau in-app dashboard).
_Avoid_: Alert sink, message publisher
