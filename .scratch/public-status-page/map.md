# Map: Public Status Page (Uptime 90 Hari & Pengumuman Insiden)

Status: complete

## Destination

Menghadirkan Public Status Page mandiri tanpa autentikasi di path `/status` (ala Uptime Kuma) yang menyajikan status kesehatan operasional sistem secara real-time, visual bar riwayat ketersediaan (uptime) 90 hari, dan pengumuman insiden resmi (Status Incident) dengan batas isolasi keamanan zero-trust, opt-in target eksplisit, kustomisasi branding terpusat, dan performa tinggi tanpa dependensi CDN eksternal.

## Notes

- Domain: Public Status Page, Uptime Rollup, Incident Management, Agentless Health Aggregation, Zero-Trust Boundary, Embedded Go SPA.
- Skills to consult: `/grilling`, `/domain-modeling`, `/research`, `/prototype`, `/codebase-design`.
- Standing preferences: Prinsip Ponytail (Lazy senior dev) — single-binary, single-port, flat SQLite tables dengan composite index, in-memory caching untuk endpoint publik agar tidak membebani database saat trafik melonjak, visual bar 90 hari menggunakan pure SVG/CSS (~30 baris vanilla JS/CSS) tanpa dependensi charting eksternal, dan zero data leakage (nama internal, IP privat, credential tidak pernah dikirim ke publik).

## Decisions so far

<!-- the index — one line per closed ticket: enough to judge relevance, then zoom the link for the detail the ticket holds -->

- [01 — Model Data Status Target & Skema Agregasi Rollup](issues/01-target-opt-in-and-data-model.md) — 4 tabel SQLite mandiri (status_page_targets, status_page_daily_rollups, status_page_incidents, status_page_incident_updates) dengan pemisahan total data privat (zero data leakage), UPSERT harian ber-composite index, dan integrasi branding settings.
- [02 — Pekerja Agregasi Daily Uptime Rollup & Status Resolution Engine](issues/02-daily-uptime-rollup-worker-and-aggregation.md) — Arsitektur dual-trigger (UPSERT harian inkremental pasca-inspeksi + hourly 90d auto-prune), klasifikasi kualitatif target dengan mitigasi anti-flapping (2 consecutive failures), akumulasi uptime non-drift berbasis sum-of-checks (bobot degraded 0.5), dan kaskade status global sistem.
- [03 — Endpoint API Publik & Batas Keamanan Zero-Trust](issues/03-public-status-api-and-zero-trust-boundary.md) — Isolasi rute publik zero-trust (tanpa pembacaan sesi admin & data masking mutlak), lock-free in-memory snapshot cache (`atomic.Pointer` + pre-compressed gzip + ETag 304), dan sharded token bucket rate limiter in-memory 100% Go stdlib. ([docs/research/03-public-status-api-and-zero-trust-boundary.md](../../docs/research/03-public-status-api-and-zero-trust-boundary.md))
- [04 — Desain Antarmuka Publik `/status` & Visual Bar Uptime 90 Hari](issues/04-public-status-page-ui-ux-and-90d-uptime-bars.md) — Prototipe mandiri zero-CDN berukuran < 25 KB dengan dark/light mode otomatis, 90 baris flexbox mikro-interaktif ber-tooltip presisi, active incident timeline, dan pengelompokan grup layanan. ([prototype.html](prototype.html))
- [05 — Antarmuka Admin Incident Publisher & Manajemen Status Target](issues/05-admin-incident-publisher-and-status-settings-ui.md) — Tab baru Settings (Status Page) untuk kontrol global & branding, alur pendaftaran opt-in target (Host vs Rule) ber-data masking, serta alur penerbitan insiden aktif dan pembaruan kronologis penanganan.

## Not yet specified

<!-- see "Fog of war": in-scope fog you can't ticket yet; graduates as the frontier advances -->

- Format RSS / Atom feed atau JSON feed publik (`/status/feed.xml`) untuk langganan notifikasi insiden oleh pihak luar.
- Notifikasi webhook keluar publik (pengunjung mendaftarkan webhook Discord/Slack untuk menerima alert insiden status page).
- Dukungan custom domain alias (misal me-route `status.company.com` langsung ke `/status` via reverse proxy header `Host`).
- Integrasi widget status badge SVG embedding (`/status/badge.svg` untuk disematkan di README GitHub atau website publik).

## Out of scope

<!-- see "Out of scope": work ruled beyond the destination; closed, never graduates -->

- Sistem pengiriman email massal / SMS blast langganan status page (Pantau adalah tool self-hosted agentless, bukan SaaS marketing platform).
- Akses multi-tenant dengan status page terpisah per grup pengguna publik.
- Membuka akses terminal atau metrik teknis mentah (CPU/RAM/Disk I/O) ke publik.
