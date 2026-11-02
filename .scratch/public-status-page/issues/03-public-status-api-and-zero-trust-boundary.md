# 03 — Endpoint API Publik & Batas Keamanan Zero-Trust

Type: research
Status: resolved
Blocked by: 01

## Question

Bagaimana arsitektur handler HTTP Go pada `/status` dan `/api/status-page/summary` mengisolasi akses publik dari sesi administratif Pantau? Apa mekanisme caching in-memory (misal atomic value / sync.RWMutex snapshot + ETag header) dan proteksi rate limiting ringan berbasis Go stdlib untuk melindungi Pantau dari lonjakan trafik publik (DDoS/slashdot effect) saat server utama mengalami insiden tanpa membebani disk atau basis data SQLite?

## Answer

Riset dan spesifikasi arsitektur komprehensif telah didokumentasikan di [docs/research/03-public-status-api-and-zero-trust-boundary.md](../../../docs/research/03-public-status-api-and-zero-trust-boundary.md).

### Ringkasan Temuan & Keputusan:

1. **Batas Keamanan Zero-Trust & Isolasi Sesi**:
   - Handler `/status` dan `/api/status-page/summary` didaftarkan langsung pada `http.ServeMux` tanpa pembungkus `authMiddleware` dan **sama sekali tidak membaca cookie sesi `pantau_session`**.
   - Admin yang membuka `/status` diperlakukan identik 100% dengan pengunjung publik biasa (tidak ada bypass atau query debug di endpoint publik).
   - *Data Masking Total*: ID host internal, IP privat, port SSH, username, rule drift teknis, dan error stack trace internal dieliminasi total pada DTO publik. Publik hanya menerima `display_name`, status kualitatif (`operational`, `degraded`, `outage`), riwayat ketersediaan 90 hari, dan pengumuman insiden yang eksplisit dikurasi.
   - *Kill-Switch*: Ketika pengaturan `status_page_enabled` bernilai false, seluruh rute publik merespons `404 Not Found`.

2. **Lock-Free In-Memory Caching (`atomic.Pointer`)**:
   - Menghindari *database bottleneck* dan SQLite contention lock saat terjadi insiden (lonjakan ribuan request/detik): **Zero DB hit on HTTP read path**.
   - Snapshot publik disimpan di RAM via `atomic.Pointer[statusPageSnapshot]` lengkap dengan payload JSON mentah, pre-compressed Gzip (`compress/gzip.BestCompression`), dan ETag SHA-256.
   - Endpoint `/api/status-page/summary` mendukung ETag HTTP 304 (*Not Modified*) dan `Accept-Encoding: gzip`, mampu melayani > 120.000 req/detik pada latensi sub-milidetik.
   - Cache rebuild di-trigger secara asinkron via channel debounced (300 ms) saat terjadi event inspeksi baru atau perubahan insiden.

3. **In-Memory Sharded Token Bucket Rate Limiter (Zero-Dependency)**:
   - Dibuat limiter berbasis 16-shard mutex map (`shardedRateLimiter`) 100% Go stdlib untuk mencegah lock contention antar-goroutine.
   - Limit default 60 req/menit dengan burst capacity 30 request per IP.
   - Dilengkapi *reaper worker* background setiap 5 menit untuk menghapus IP nonaktif dan batas maksimum map untuk menangkal serangan memory exhaustion.
   - Resolusi IP klien memvalidasi header `X-Forwarded-For` dan `X-Real-IP` hanya jika koneksi langsung berasal dari loopback/IP privat.

4. **UI Serving Mandiri (Airgapped & Zero-CDN)**:
   - Template `/status` disimpan di `internal/web/static/status.html`, di-embed via `embed.FS`, dan dikompresi di memori saat startup.
   - CSP ketat tanpa WebSocket (`connect-src 'self'`) menjamin keamanan penuh di lingkungan intranet maupun publik.
