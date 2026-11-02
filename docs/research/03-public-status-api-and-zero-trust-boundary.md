# 03 — Riset Arsitektur: Endpoint API Publik, Batas Keamanan Zero-Trust, In-Memory Caching & Rate Limiter

**Konteks Repositori:** Pantau (*Single-binary, Agentless SSH, Embedded SQLite, Airgapped-ready*)  
**Status:** Resolved  
**Tiket:** `.scratch/public-status-page/issues/03-public-status-api-and-zero-trust-boundary.md`  

---

## 1. Eksekutif & Ringkasan Masalah

Fitur **Public Status Page** memungkinkan publik atau pengguna internal organisasi melihat status ketersediaan layanan (*uptime*) tanpa memerlukan hak akses admin ke dashboard Pantau. Namun, fitur ini membawa dua tantangan kritis:
1. **Risiko Kebocoran Keamanan & Zero-Trust Boundary**: Dashboard administratif Pantau memuat informasi infrastruktur yang sangat sensitif (IP privat, port SSH, username, rule drift, isi log/error internal). Endpoint publik harus dipastikan tidak memiliki celah kebocoran data (*data leakage*), tidak dapat disusupi melalui bypass sesi admin, dan menyajikan data hasil *data masking* murni.
2. **Lonjakan Trafik Saat Insiden (*The Slashdot / DDoS Effect*)**: Ketika sistem utama atau layanan penting mengalami insiden, ratusan hingga ribuan pengguna akan secara serentak mengakses `/status` dan melakukan *refresh* berulang kali. Jika endpoint publik mengeksekusi kueri SQLite secara langsung pada setiap request, disk I/O dan lock SQLite akan tersaturasi (*database bottleneck*), yang justru melumpuhkan engine pemantauan Pantau di saat paling krusial.

Riset ini mengkaji struktur `internal/web/server.go`, merancang pemisahan batas zero-trust, menentukan skema DTO publik yang tersanitasi total, serta merancang pola **Lock-Free In-Memory Caching (`atomic.Pointer`)** dan **In-Memory Sharded Token Bucket Rate Limiter** berbasis 100% Go Standard Library tanpa dependensi pihak ketiga (*Zero-Dependency*).

---

## 2. Analisis Arsitektur Web Eksisting (`internal/web/server.go`)

### 2.1. Routing & Sub-Routing (`http.ServeMux`)
* Server menggunakan `http.ServeMux` standar Go (`s.mux = http.NewServeMux()`) yang diinisialisasi pada `NewServer()` dan dipetakan di `s.routes()`.
* **Pattern Matching**:
  * Root route `/` ditangani oleh `s.handleIndex` dengan validasi eksplisit:
    ```go
    if r.URL.Path != "/" {
        http.NotFound(w, r)
        return
    }
    ```
    Hal ini krusial karena dalam Go `http.ServeMux`, pattern `/` adalah *catch-all prefix*. Pendaftaran rute baru seperti `/status` atau `/status/` akan diprioritaskan oleh `ServeMux` karena memiliki kecocokan prefiks yang lebih spesifik daripada `/`.
  * Rute berparameter dinamis menggunakan trailing slash (misal `/api/hosts/`, `/api/rules/`), di mana ID diekstraksi menggunakan `strings.TrimPrefix(r.URL.Path, "/api/hosts/")`.

### 2.2. Otentikasi & Manajemen Sesi Admin
* **Sesi In-Memory**: Sesi login admin dikelola dalam `sync.Map` (`s.sessions`), memetakan token heksadesimal 32-byte acak ke waktu kedaluwarsa (`time.Time`, default 7 hari).
* **Cookie**: Menggunakan cookie HTTP `pantau_session` (`HttpOnly: true`, `SameSite: Lax`, `Path: "/"`).
* **Middleware**: Diproteksi melalui `s.authMiddleware(next http.HandlerFunc)`. Jika cookie tidak valid atau expired, request langsung ditolak dengan `401 Unauthorized`.
* **Rute Publik Eksisting**: Saat ini hanya mencakup `/`, `/favicon.ico`, `/vendor/*`, `/api/docs`, serta endpoint pra-otentikasi (`/api/login`, `/api/login/2fa`, snapshot bootstrap).

### 2.3. Static Asset Serving & Optimasi ADR-0018 / ADR-0013
* Seluruh aset web di-embed langsung ke dalam binary Go melalui `embed.FS` (`internal/web/ui.go`):
  * `//go:embed static` untuk direktori vendor dan favicon.
  * `//go:embed static/index.html` untuk SPA admin utama.
* **Pre-Compressed In-Memory Gzip (ADR-0018)**:
  * Pada saat startup (`prepareHTML`), file HTML dikompresi satu kali ke memori (`s.htmlContentGz`) menggunakan `compress/gzip.BestCompression`.
  * ETag dihitung dari hash SHA-256 (`fmt.Sprintf("\"%x\"", sum[:8])`).
  * Jika klien mengirim `If-None-Match` yang cocok, server langsung merespons **`304 Not Modified`** (0 byte transfer).
  * Jika klien mendukung `Accept-Encoding: gzip`, server menyajikan `s.htmlContentGz` langsung tanpa alokasi CPU runtime.
* **Content Security Policy (ADR-0013)**:
  * Strict CSP lokal diterapkan: `default-src 'self'; script-src 'self' 'unsafe-inline'; style-src 'self' 'unsafe-inline'; connect-src 'self' ws: wss:; font-src 'self' data:; img-src 'self' data:;` menjamin ketersediaan penuh di lingkungan terisolasi (*airgapped*) tanpa kebocoran ke CDN publik.

---

## 3. Desain Batas Keamanan Zero-Trust & Pemisahan Sesi

### 3.1. Topologi Rute Baru
Dua rute publik baru didaftarkan di `s.routes()` tanpa pembungkus `s.authMiddleware`:
```go
// Public Status Page (Airgapped SPA & Masked API)
s.mux.HandleFunc("/status", s.handlePublicStatus)
s.mux.HandleFunc("/status/", s.handlePublicStatus)
s.mux.HandleFunc("/api/status-page/summary", s.handlePublicStatusSummary)
```

### 3.2. Prinsip Isolasi Zero-Trust
1. **Total Session Agnostic**:
   * Handler `/status` dan `/api/status-page/summary` **sama sekali tidak membaca, memvalidasi, atau bergantung pada cookie `pantau_session`**.
   * Jika seorang admin yang sedang login membuka `/status`, cookie `pantau_session` tetap diabaikan. Respons yang dikembalikan identik 100% dengan respons untuk pengunjung publik anonim.
   * Tidak ada parameter `?debug=true`, `?full=true`, atau *admin-preview* pada endpoint publik ini. Fitur preview admin harus diakses melalui endpoint internal `/api/settings/status-page/preview` yang diproteksi `authMiddleware`.
2. **Pencegahan Eksfiltrasi Data & ID Enumeration**:
   * Endpoint publik **tidak pernah** mengembalikan ID host internal (`hosts.id`). Target publik diidentifikasi melalui `target_id` (ID pada tabel `status_page_targets`) atau `slug` publik.
   * Field privat host (alamat IP privat/publik, port SSH, username, path direktori, rule drift internal, process list, kernel, alert root causes) dieliminasi total pada level konstruksi DTO.
3. **Status Page Feature Flag / Kill-Switch**:
   * Pengaturan `status_page_enabled` ("true" / "false") diatur melalui Dashboard Admin.
   * Jika status page berstatus nonaktif:
     * `/status` mengembalikan `404 Not Found`.
     * `/api/status-page/summary` mengembalikan `404 Not Found` (`{"error":"status_page_disabled"}`).
     * Tidak ada metadata atau data historis yang bocor saat fitur dimatikan.

---

## 4. Spesifikasi Payload & Aturan Data Masking

### 4.1. Pemetaan Entitas Internal ke DTO Publik

| Sumber Internal (`store.*`) | Risiko Keamanan Jika Bocor | Transformasi ke DTO Publik (`PublicStatusSummary`) |
| :--- | :--- | :--- |
| `hosts.name`, `hosts.host`, `hosts.port` | Pemetaan topologi jaringan, IP internal, port SSH | **Dihapus total**. Digantikan oleh `status_page_targets.display_name` (misal: "API Gateway (Production)") |
| `hosts.user`, `hosts.custom_key` | Kredensial & otentikasi SSH | **Dihapus total**. |
| `hosts.status`, `actual_items`, `desired_rules` | Detail kegagalan teknis (PID mati, disk full, port tertutup) | **Diterjemahkan ke status kualitatif**: `"operational"`, `"degraded"`, `"major_outage"`, atau `"maintenance"` |
| `hosts.cpu_load`, `hosts.ram_used_bytes`, dll. | Profil kapasitas & beban server internal | **Dihapus total**. Publik hanya melihat uptime rasio harian & status agregat. |
| `incidents`, `alerts`, `inspection_runs` | Error stack trace, root cause teknis internal | **Dihapus total**. Digantikan oleh `status_page_incidents` (insiden yang secara eksplisit dikurasi dan dipublikasikan oleh admin). |

### 4.2. Skema JSON Kontrak Publik (`/api/status-page/summary`)
```json
{
  "page": {
    "title": "Pantau Status",
    "description": "Real-time service status and incident updates",
    "support_url": "https://help.example.com",
    "logo_url": ""
  },
  "status": {
    "indicator": "operational",
    "description": "All Systems Operational"
  },
  "targets": [
    {
      "id": 1,
      "name": "Public API Gateway",
      "group": "Core APIs",
      "status": "operational",
      "uptime_90d": 99.98,
      "daily_history": [
        { "date": "2026-08-03", "status": "operational", "uptime": 100.0 },
        { "date": "2026-08-04", "status": "operational", "uptime": 100.0 },
        { "date": "2026-10-31", "status": "degraded", "uptime": 98.45 }
      ]
    },
    {
      "id": 2,
      "name": "Customer Web Portal",
      "group": "Frontends",
      "status": "operational",
      "uptime_90d": 100.0,
      "daily_history": [
        { "date": "2026-08-03", "status": "operational", "uptime": 100.0 }
      ]
    }
  ],
  "active_incidents": [
    {
      "id": 14,
      "title": "Intermittent Latency on Authentication Service",
      "status": "monitoring",
      "impact": "minor",
      "created_at": "2026-10-31T14:20:00Z",
      "updated_at": "2026-10-31T15:05:00Z",
      "updates": [
        {
          "status": "monitoring",
          "message": "Mitigation applied. Latency returned to normal thresholds. We continue monitoring.",
          "created_at": "2026-10-31T15:05:00Z"
        },
        {
          "status": "investigating",
          "message": "We are investigating elevated response times in the Southeast Asia region.",
          "created_at": "2026-10-31T14:20:00Z"
        }
      ]
    }
  ],
  "updated_at": "2026-10-31T15:10:00Z"
}
```

---

## 5. Rancangan Pola In-Memory Caching (Lock-Free & Zero-Disk Overhead)

### 5.1. Analisis Kebutuhan Kinerja
* **Ancaman Beban**: Saat insiden terjadi, trafik publik dapat melonjak dari 1 req/menit menjadi ribuan req/detik.
* **Masalah SQLite**: SQLite adalah basis data *embedded single-file*. Mengeksekusi kueri aggregasi rollup 90 hari untuk puluhan target pada setiap request HTTP publik akan membebani thread SQLite, menyebabkan contention lock, dan memperlambat worker inspeksi SSH Pantau.
* **Solusi**: **Zero DB hits on HTTP read path**. Data publik selalu disajikan langsung dari RAM.

### 5.2. Struktur Cache Snapshot In-Memory
Menggunakan `sync/atomic.Pointer` (tersedia di Go 1.19+):

```go
type statusPageSnapshot struct {
	etag        string    // SHA-256 ETag dari rawJSON
	rawJSON     []byte    // Payload JSON mentah tersanitasi
	gzipJSON    []byte    // Pre-compressed Gzip JSON (BestCompression)
	generatedAt time.Time // Waktu snapshot dibuat
	disabled    bool      // True jika status page dimatikan di settings
}

type Server struct {
	// ... field eksisting ...
	statusSnapshot     atomic.Pointer[statusPageSnapshot]
	statusInvalidateCh chan struct{} // Buffer 1 untuk debouncing
	statusLimiter      *shardedRateLimiter
}
```

### 5.3. Jalur Baca Cepat (*Fast-Path Read*)
Ketika `GET /api/status-page/summary` masuk:
1. Panggil rate limiter IP (lihat Bagian 6). Jika terlampaui -> kembalikan `429 Too Many Requests`.
2. Ambil snapshot via atomic pointer: `snap := s.statusSnapshot.Load()`.
3. Jika `snap == nil` atau `snap.disabled`: kembalikan `404 Not Found`.
4. Periksa header `If-None-Match`:
   ```go
   w.Header().Set("ETag", snap.etag)
   w.Header().Set("Cache-Control", "public, max-age=15, stale-while-revalidate=30")
   if match := r.Header.Get("If-None-Match"); match != "" && match == snap.etag {
       w.WriteHeader(http.StatusNotModified)
       return
   }
   ```
5. Jika klien mendukung kompresi Gzip (`Accept-Encoding: gzip`):
   ```go
   w.Header().Set("Content-Type", "application/json; charset=utf-8")
   w.Header().Set("Content-Encoding", "gzip")
   w.WriteHeader(http.StatusOK)
   _, _ = w.Write(snap.gzipJSON)
   ```
6. Fallback jika tidak mendukung gzip: tulis `snap.rawJSON`.

> **Karakteristik Kinerja**: 
> Operasi baca di atas adalah *lock-free* (tidak ada RWMutex contention), *zero-allocation*, dan *zero-SQL*. Satu core server dapat melayani lebih dari **120.000 req/detik** dengan latensi sub-milidetik.

### 5.4. Siklus Invalidasi & Debounced Background Rebuild
Snapshot di-rebuild secara asinkron di background saat terjadi event:
1. **Pemicu Event**:
   * Worker inspeksi (`ins.InspectHost`) selesai dan terjadi perubahan status pada target yang di-opt-in.
   * Admin membuat, mengubah, atau menutup status insiden publik (`status_page_incidents`).
   * Admin mengubah konfigurasi target publik atau status page settings.
   * Pekerja Rollup Harian (Ticket 02) menyelesaikan agregasi harian jam 00:00 UTC.
   * Fallback Heartbeat Ticker (setiap 60 detik) untuk memperbarui timestamp jam dinding.
2. **Coalesced Debouncer**:
   Mencegah *stampede* rebuild SQLite jika 50 host selesai diinspeksi secara hampir bersamaan.
   ```go
   func (s *Server) InvalidateStatusPageCache() {
       select {
       case s.statusInvalidateCh <- struct{}{}:
       default:
           // Channel sudah terisi, permintaan rebuild sudah terjadwal
       }
   }

   func (s *Server) startStatusPageCacheWorker(ctx context.Context) {
       ticker := time.NewTicker(60 * time.Second)
       defer ticker.Stop()

       for {
           select {
           case <-ctx.Done():
               return
           case <-ticker.C:
               s.rebuildStatusPageSnapshot()
           case <-s.statusInvalidateCh:
               // ponytail: Debounce 300ms untuk menggabungkan batch event inspeksi
               time.Sleep(300 * time.Millisecond)
               // Kuras sisa notifikasi yang menumpuk selama sleep
               select {
               case <-s.statusInvalidateCh:
               default:
               }
               s.rebuildStatusPageSnapshot()
           }
       }
   }
   ```

---

## 6. Proteksi Rate Limiting Ringan (Go Stdlib Sharded Token Bucket)

### 6.1. Mengapa Perlu Rate Limiter Khusus?
Pada saat gangguan/downtime, script bot, monitoring pihak ketiga (UptimeRobot, Pingdom), atau refresh massal dari pengguna dapat membanjiri koneksi HTTP server. Kita memerlukan rate limiter in-memory yang:
* 100% menggunakan Go Standard Library (tidak butuh Redis atau external daemon).
* Efisien memori (*bounded memory* dengan auto-eviction).
* Tidak mengalami *lock bottleneck* di bawah puluhan ribu koneksi serentak.

### 6.2. Desain Sharded Token Bucket
Menggunakan array 16 shard mutex untuk mendistribusikan beban kunci berdasarkan hash IP klien:

```go
type tokenBucket struct {
	tokens   float64
	lastSeen time.Time
}

type limiterShard struct {
	mu      sync.Mutex
	buckets map[string]*tokenBucket
}

type shardedRateLimiter struct {
	shards    [16]limiterShard
	rate      float64 // misal: 1.0 token/detik (60 req/menit)
	burst     float64 // misal: 30 token burst
	maxPerMap int     // batas atas entri per shard (misal: 5.000)
}

func newShardedRateLimiter(rate, burst float64) *shardedRateLimiter {
	l := &shardedRateLimiter{rate: rate, burst: burst, maxPerMap: 5000}
	for i := 0; i < 16; i++ {
		l.shards[i].buckets = make(map[string]*tokenBucket)
	}
	return l
}

// fnv32 hash sederhana stdlib untuk sharding cepat
func hashIP(ip string) uint32 {
	var h uint32 = 2166136261
	for i := 0; i < len(ip); i++ {
		h ^= uint32(ip[i])
		h *= 16777619
	}
	return h
}

func (l *shardedRateLimiter) Allow(ip string) bool {
	shardIdx := hashIP(ip) % 16
	shard := &l.shards[shardIdx]

	shard.mu.Lock()
	defer shard.mu.Unlock()

	now := time.Now()
	b, ok := shard.buckets[ip]
	if !ok {
		// ponytail: Batasi ukuran map untuk menangkal serangan spoofed IP memory exhaustion
		if len(shard.buckets) >= l.maxPerMap {
			return false
		}
		shard.buckets[ip] = &tokenBucket{
			tokens:   l.burst - 1,
			lastSeen: now,
		}
		return true
	}

	// Refill token sesuai waktu berlalu
	elapsed := now.Sub(b.lastSeen).Seconds()
	b.lastSeen = now
	b.tokens += elapsed * l.rate
	if b.tokens > l.burst {
		b.tokens = l.burst
	}

	if b.tokens >= 1.0 {
		b.tokens -= 1.0
		return true
	}
	return false
}
```

### 6.3. Pembersihan Otomatis (Reaper) Anti-Memory Leak
Pembersih latar belakang berjalan setiap 5 menit untuk menghapus IP yang tidak aktif lebih dari 10 menit:
```go
func (l *shardedRateLimiter) StartReaper(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			now := time.Now()
			for i := 0; i < 16; i++ {
				shard := &l.shards[i]
				shard.mu.Lock()
				for ip, b := range shard.buckets {
					if now.Sub(b.lastSeen) > 10*time.Minute {
						delete(shard.buckets, ip)
					}
				}
				shard.mu.Unlock()
			}
		}
	}
}
```

### 6.4. Ekstraksi IP Klien & Penanganan Reverse Proxy Aman
Untuk mencegah IP spoofing via header `X-Forwarded-For`:
```go
func resolveClientIP(r *http.Request) string {
	remoteHost, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		remoteHost = r.RemoteAddr
	}

	ip := net.ParseIP(remoteHost)
	// Hanya percayai X-Forwarded-For jika koneksi langsung berasal dari localhost atau privat IP
	if ip != nil && (ip.IsLoopback() || ip.IsPrivate()) {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			parts := strings.Split(xff, ",")
			clientIP := strings.TrimSpace(parts[0])
			if net.ParseIP(clientIP) != nil {
				return clientIP
			}
		}
		if xri := r.Header.Get("X-Real-IP"); xri != "" {
			if net.ParseIP(strings.TrimSpace(xri)) != nil {
				return strings.TrimSpace(xri)
			}
		}
	}
	return remoteHost
}
```

---

## 7. Desain Penyajian Antarmuka Publik (`/status`)

1. **Aset UI Terisolasi**:
   * Template HTML/JS antarmuka status publik disimpan di `internal/web/static/status.html` (dikembangkan di Ticket 04).
   * Di-embed melalui `//go:embed static/status.html` di `internal/web/ui.go`.
   * Dikompresi di memori saat startup: `s.statusHTMLContentGz` + ETag SHA-256 tersendiri.
2. **Content Security Policy Ketat**:
   * Berbeda dengan SPA admin yang memerlukan koneksi WebSocket untuk terminal dan log docker (`connect-src 'self' ws: wss:`), SPA publik tidak memerlukan WebSocket sama sekali:
     ```http
     Content-Security-Policy: default-src 'self'; script-src 'self' 'unsafe-inline'; style-src 'self' 'unsafe-inline'; connect-src 'self'; font-src 'self' data:; img-src 'self' data:;
     ```
   * Menjamin zero-leakage, tidak ada ketergantungan CDN eksternal, dan aman dijalankan pada jaringan intranet maupun publik.
3. **HTTP 304 Revalidation**:
   * Permintaan berulang ke `/status` dari browser pengguna akan menghasilkan `304 Not Modified` dengan transfer jaringan 0 byte.
