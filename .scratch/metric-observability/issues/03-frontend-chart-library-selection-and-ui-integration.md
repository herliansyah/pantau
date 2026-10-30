# 03 — Pemilihan Library Visualisasi Grafik Ringan & Vendor Lokal

Type: research
Status: resolved
Blocked by:

## Question

Library charting JavaScript mana yang paling efisien untuk di-vendor secara lokal (zero CDN/eksternal network, footprint file < 40KB, e.g. uPlot vs minimal Chart.js vs SVG renderer mandiri) yang mampu merender interaksi multi-series time-series zoom/tooltip dengan performa tinggi pada puluhan ribu titik data di peramban?

## Answer

Gunakan **Strategi Dual-Tier Visualisasi (Lazy & High-Performance)**:

1. **Tab Observabilitas Detail Host (10.000 – 100.000 titik data)**: **uPlot** (`uPlot.iife.min.js` ~48 KB JS + ~2.4 KB CSS, zero dependencies).
   - **Rasional**: Dipakai oleh Grafana untuk panel time-series. Memanfaatkan HTML5 Canvas 2D dengan struktur data columnar `[ [timestamps...], [cpu...], [ram...] ]` tanpa overhead pembuatan jutaan objek JavaScript (sangat hemat memori & bebas garbage collection freeze). Mendukung pan, zoom, dan crosshair synchronization antar-panel grafik (`uPlot.sync()`) secara bawaan.
2. **Kartu Ringkasan Host / Mini Sparkline (15 – 50 titik)**: **Handcrafted Pure SVG Helper** (~15 baris vanilla JS, 0 KB dependensi).
   - **Rasional**: Tidak membutuhkan overhead canvas untuk sparkline kecil.

**Dampak pada Komponen Lain**:
- **Format API (Tiket 04)**: Payload `/api/hosts/:id/metrics` harus mengadopsi format array columnar `{ timestamps: [...], series: { cpu: [...], mem: [...] } }` yang menghemat ukuran transfer JSON 60-70% dan langsung dapat dikonsumsi uPlot tanpa remapping data di frontend.
- **Penyimpanan Lokal**: Ditempatkan di `internal/web/static/vendor/` di-embed ke binary via Go `embed`.
