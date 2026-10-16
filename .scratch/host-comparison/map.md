# Map: Host Comparison (Side-by-Side Multi-Host Evaluation & Scoring)

Status: complete

## Destination

Menghadirkan fitur Host Comparison interaktif di Pantau berbasis Workspace Modal (75vw × 80vh) untuk membandingkan 2–4 Host secara side-by-side dengan sistem scoring head-to-head transparan di 4 Comparison Category (Compute, Storage & I/O, Network Health, System Hygiene & Lifecycle), badge keunggulan visual, ringkasan narasi keputusan otomatis, dan tombol ekspor Markdown ke clipboard tanpa menambah backend endpoint baru (pure client-side).

## Notes

- Domain: Host Comparison, Workspace Modal, Agentless Linux Telemetry, Embedded SPA.
- Skills to consult: `/grilling`, `/domain-modeling`, `/codebase-design`.
- Standing preferences: Prinsip Ponytail (Lazy senior dev) — olah data inspeksi yang sudah tersedia di `state.hosts`, zero dependency eksternal, zero backend endpoint baru, CSS grid layout native yang adaptif.

## Decisions so far

<!-- the index — one line per closed ticket -->

- [01 — Matriks Metrik & Logika Kalkulasi Skor Head-to-Head](issues/01-metric-matrix-and-head-to-head-scoring-logic.md) — Definisi 4 kategori evaluasi berbasis telemetri lokal, toleransi deadband 5% anti-noise, sistem poin Best-in-Class, shared tie, dan soft penalty untuk host down/stale.
- [02 — Desain Tata Letak UI Workspace Modal Side-by-Side](issues/02-workspace-modal-side-by-side-ui-layout.md) — Pemilihan arsitektur Variant A (Unified Comparative Matrix) di Workspace Modal (75vw × 80vh) dengan CSS grid host summary, visual highlight pemenang metrik ⭐, dan status degradasi transparan.
- [03 — Generator Narasi Kesimpulan Keputusan & Ekspor Markdown](issues/03-decision-narrative-generator-and-markdown-export.md) — Alur 4 tahap sintesis narasi rekomendasi keputusan (standing, strengths, risk flags, takeaway), format tabel ekspor markdown, dan integrasi clipboard non-blocking.

## Not yet specified

- Preset komparasi berbobot (Weighted Profile): apakah perlu profil khusus seperti "Database Server" vs "Web/API Gateway" dengan bobot poin kustom.
- Perbandingan granular daftar service & container aktif antar host (mencocokkan kesamaan dan perbedaan service spesifik yang running).
- Historis tren komparasi time-series (apakah agregasi rata-rata 24 jam terakhir relevan atau cukup snapshot inspeksi terakhir).

## Out of scope

- Pengujian benchmark beban sintetis aktif melalui SSH (seperti menjalankan sysbench, fio, iperf secara live saat komparasi karena risiko membebani server target dan melanggar prinsip agentless telemetry pasif).
- Otomasi migrasi workload atau perpindahan service otomatis langsung dari hasil perbandingan.
