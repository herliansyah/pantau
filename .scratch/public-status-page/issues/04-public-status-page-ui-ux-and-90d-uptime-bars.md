# 04 — Desain Antarmuka Publik `/status` & Visual Bar Uptime 90 Hari

Type: prototype
Status: resolved
Blocked by: 03

## Question

Bagaimana rancangan antarmuka visual mandiri (`/status`) yang zero-CDN, responsive, ramah lingkungan airgapped, dan mendukung auto dark/light mode? Bagaimana komponen visual bar 90 hari (90 bilah SVG/CSS interaktif dengan tooltip tanggal dan rasio uptime harian) dirender secara efisien tanpa library charting eksternal yang berat?

## Answer

Prototipe antarmuka interaktif yang sepenuhnya mandiri (*self-contained*) telah dibuat dan dapat diuji langsung di peramban pada berkas [`.scratch/public-status-page/prototype.html`](../prototype.html).

### Keputusan Desain & Spesifikasi Komponen:

1. **Arsitektur Zero-CDN & Kepatuhan Airgapped (ADR-0013)**:
   - Tidak menggunakan font daring (Google Fonts) atau framework CSS eksternal (Bootstrap/Tailwind CDN). Menggunakan *system font stack* native (`-apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif`).
   - SVG disematkan secara inline (zero network asset request).
   - Ukuran total payload HTML+CSS+JS < 25 KB uncompressed (< 6 KB dengan kompresi gzip), memuat instan di jaringan lambat atau intranet.

2. **Tema Dark & Light Adaptif**:
   - Menggunakan CSS Custom Properties (`--bg`, `--card-bg`, `--text-main`, `--success`, dll.) dengan atribut `data-theme="dark"` (default) dan `data-theme="light"`.
   - Preferensi tema disimpan di `localStorage` peramban pengunjung.

3. **Komponen Visual Bar Uptime 90 Hari (Pure CSS/Flexbox)**:
   - **Zero Chart Library Overhead**: Menolak pustaka charting berat (Chart.js / Highcharts). Menggunakan 90 elemen `div.bar-day` fleksibel dalam kontainer flexbox (`gap: 2.5px; height: 32px;`).
   - **Skema Warna Standar Industri**:
     - Hijau (`#10b981`): Uptime 100% tanpa gangguan.
     - Oranye (`#f59e0b`): Uptime terdegradasi / partial drift.
     - Merah (`#ef4444`): Downtime / outage tercatat.
     - Abu-abu (`#334155`): Hari kosong / belum ada data historis.
   - **Tooltip Mengambang Real-Time**: Satu elemen `#bar-tooltip` tunggal yang diposisikan secara presisi via `getBoundingClientRect()` saat kursor melayang (`mouseenter`), menampilkan tanggal ISO, persentase uptime numerik, dan status kualitatif.

4. **Hierarki Tata Letak Antarmuka**:
   - **Header**: Logo, Nama Organisasi/Layanan, dan Tombol Switcher Tema.
   - **Global Status Banner**: Banner besar dengan status pulse dot animasi (`All Systems Operational`, `Active Incident: Degraded Performance`, `Major Outage Detected`).
   - **Active Incident Area**: Muncul otomatis di bawah banner jika ada insiden yang belum resolved, menampilkan timeline kronologis penanganan (*Investigating* $\rightarrow$ *Monitoring* $\rightarrow$ *Resolved*).
   - **Target Grouping**: Daftar layanan dikelompokkan berdasarkan `group_name` (misal *Core Services & APIs*, *Customer Web Applications*).
   - **Past Incidents Archive**: Arsip riwayat insiden harian yang telah ditutup.
   - **Footer**: Label identitas *Powered by Pantau* dan tautan kontak bantuan.
