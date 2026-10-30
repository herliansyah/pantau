# Map: Metric Samples & Application Telemetry Observability

Status: complete

## Destination

Menghadirkan sistem observabilitas time-series (telemetri) berbasis SQLite internal dengan retensi terkonfigurasi (default 7 hari + auto-prune berkala) dan visualisasi grafik tren temporal interaktif di dashboard (mencakup sumber daya Hardware dan status performa Aplikasi via preset Nginx, MySQL, PHP-FPM maupun kustom probe), yang diinspeksi secara efisien tanpa agen eksternal.

## Notes

- Domain: Agentless Linux Telemetry, Embedded SQLite, Responsive Web UI, Application Probing.
- Skills to consult: `/grilling`, `/domain-modeling`, `/codebase-design`.
- Standing preferences: Prinsip Ponytail (Lazy senior dev) — SQLite flat table dengan composite index, single-binary bundling tanpa dependensi CDN/jaringan eksternal, batch SSH inspection execution tanpa daemon agen tambahan di target. Fitur observabilitas metrik harus mendukung kontrol keaktifan (toggle on/off) secara global dan override per-host.

## Decisions so far

<!-- the index — one line per closed ticket -->

- [01 — Desain Skema Database Metric Samples & Konfigurasi Auto-Prune](issues/01-database-schema-metric-samples-and-prune.md) — Tabel flat `metric_samples` (host_id, metric, value, recorded_at) dengan composite index, kontrol toggle tri-state (Inherit/Force ON/Force OFF) global dan per-host, serta hourly auto-prune worker.
- [02 — Arsitektur Application Metric Probe & Kontrak Ekstraksi Preset/Kustom](issues/02-application-metric-probe-architecture-and-preset-contracts.md) — Ekstraksi via plain-text key-value (`key: value`), tabel konfigurasi `host_metric_probes`, eksekusi satu batch script SSH dengan batas timeout isolasi per-probe, dan graceful silent fallback saat service target mati.
- [03 — Pemilihan Library Visualisasi Grafik Ringan & Vendor Lokal](issues/03-frontend-chart-library-selection-and-ui-integration.md) — Dual-tier visualisasi: uPlot (~48 KB, HTML5 Canvas 2D, zero-deps, columnar data, native sync & zoom) untuk modal observabilitas detail, dan handcrafted pure SVG helper (~15 baris) untuk mini-sparkline di Host Card.
- [04 — Spesifikasi API Query Time-Series & Strategi Downsampling](issues/04-metric-query-api-and-downsampling-strategy.md) — Payload JSON columnar langsung untuk uPlot (`timestamps` + `series` key-value arrays), dynamic bucketing di level SQL (`step=0` untuk 1h/6h, `300s` untuk 24h, `1800s` untuk 7d) menjaga transfer < 15 KB dan respon < 25 ms.
- [05 — Desain Tab Observabilitas Metrik & Interaktivitas UI](issues/05-host-detail-metric-tab-ui-ux.md) — Tab `Metrics & Telemetry` di Host Detail Modal dengan stack chart uPlot ter-sync, pill selector 1h/6h/24h/7d, filter kategori, tombol manage probes, mini-sparkline SVG di Host Card, serta banner ramah untuk disabled state.

## Not yet specified

- Integrasi Desired Rule Kind `metric_threshold` untuk evaluasi Drift berbasis ambang batas numerik metrik aplikasi.
- Mekanisme export reverse endpoint `/metrics` untuk penarikan data oleh sistem Prometheus/Grafana eksternal.
- Algoritma deteksi anomali / spike baseline otomatis pada tren metrik 7 hari.

## Out of scope

- Penggunaan database TSDB terdistribusi terpisah (VictoriaMetrics, InfluxDB, Thanos).
- Sub-second high-frequency telemetry streaming (inspeksi tetap mengikuti siklus berkala normal 30-60 detik).
- Distributed APM tracing / call graph spans (Jaeger, Zipkin).
