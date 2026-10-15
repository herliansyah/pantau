# Process Telemetry & Package Inventory Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Menghadirkan visibilitas agentless lengkap terhadap proses pemakan sumber daya aktif (Top CPU, RAM, Swap) dan inventarisasi paket perangkat lunak terpasang (Package Inventory) beserta klasifikasi manual vs bawaan dan deteksi layanan terbengkalai (Dormant Package) pada Pantau.

**Architecture:** Memisahkan *Process Telemetry* (ringan, diekstrak di setiap siklus inspeksi normal dan disimpan di tabel `hosts`) dari *Package Inventory* (ekstraksi on-demand/lazy polyglot multi-distro yang disimpan di tabel `host_packages` dengan atomic replace). Antarmuka pengguna menyajikan widget *Top Resource Consumers* di tab Overview dan tab baru *Packages* di Workspace Modal lengkap dengan pintasan Terminal Dock dan integrasi Desired State.

**Tech Stack:** Go 1.22+, SQLite (`modernc.org/sqlite`), Vanilla JS/CSS (embedded single-binary assets), POSIX Shell scripting agentless over SSH.

**Spec:** [docs/adr/0032-process-telemetry-and-package-inventory.md](file:///home/ian/pantau-chimaera/docs/adr/0032-process-telemetry-and-package-inventory.md) dan [CONTEXT.md](file:///home/ian/pantau-chimaera/CONTEXT.md).

## Global Constraints

- 100% Agentless: Tidak boleh menginstal daemon, agen, python, atau library tambahan pada host remote.
- Polyglot & Kompatibel Linux Lawas: Perintah shell harus valid dari POSIX / CentOS 5 hingga Ubuntu modern. Gunakan `shellQuote` untuk argumen remote.
- Zero External Web CDN: Aset CSS/JS harus tetap self-contained dalam embedded binary tanpa memanggil CDN publik.
- Database Atomic Replace: Pembaruan paket untuk suatu host harus menggantikan entri lama secara atomik (`DELETE FROM host_packages WHERE host_id = ?` + batch insert).
- Non-destructive UX: Tidak menyediakan tombol hapus/uninstall paket instan via web; aksi dialihkan ke pintasan Terminal Dock Pantau.

## Review Focus

1. Server tanpa Swap (`SwapTotalBytes == 0` atau `SwapUsedBytes == 0`): Ekstraksi swap harus otomatis di-skip dan widget tidak menampilkan error atau nilai sampah.
2. Distro Linux dengan format tanggal paket bervariasi: Parsing tanggal instalasi harus memiliki fallback aman ke 0 jika informasi waktu tidak tersedia (misal file log dpkg dirotasi).
3. Paket dengan nama karakter aneh atau whitespace: Parsing baris output package manager (`\t` delimiter) harus tahan terhadap string tak terduga tanpa panic.
4. Host yang sedang offline / down saat Rescan Packages dipicu: API harus mengembalikan error HTTP yang deskriptif tanpa mengunci in-flight state host.
5. Host dengan ribuan paket: Rendering tabel frontend harus efisien dengan pencarian instan sisi klien (*client-side filtering*) tanpa lag pada DOM.

---

### Task 1: Store & Schema Model (Process Telemetry & HostPackage)

**Files:**
- Modify: `internal/store/db.go`
- Test: `internal/store/db_test.go`

**Interfaces:**
- Consumes: `store.DB`, `store.Host`
- Produces:
  - `ProcessInfo` struct (`PID int`, `User string`, `CPU float64`, `MEM float64`, `RSSBytes int64`, `SwapBytes int64`, `Command string`)
  - `HostPackage` struct (`ID int64`, `HostID int64`, `Name string`, `Version string`, `SizeBytes int64`, `InstallEpoch int64`, `IsManual bool`, `ServiceName string`, `ServiceStatus string`, `LastScannedAt time.Time`)
  - `db.ReplaceHostPackages(hostID int64, pkgs []*HostPackage) error`
  - `db.GetHostPackages(hostID int64) ([]*HostPackage, error)`
  - New fields on `Host`: `TopCPUProcesses string`, `TopRAMProcesses string`, `TopSwapProcesses string`

- [ ] **Step 1: Write the failing test**

Tambahkan pengujian model `HostPackage` dan serialization proses di `internal/store/db_test.go`:
```go
func TestHostPackagesAndProcessTelemetry(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	host, err := db.CreateHost(&Host{Name: "srv1", Host: "10.0.0.1", Port: 22, User: "root"})
	if err != nil {
		t.Fatalf("create host: %v", err)
	}

	pkgs := []*HostPackage{
		{
			HostID:        host.ID,
			Name:          "nginx",
			Version:       "1.18.0",
			SizeBytes:     1024000,
			InstallEpoch:  1600000000,
			IsManual:      true,
			ServiceName:   "nginx.service",
			ServiceStatus: "active",
		},
		{
			HostID:        host.ID,
			Name:          "curl",
			Version:       "7.68.0",
			SizeBytes:     204800,
			InstallEpoch:  1590000000,
			IsManual:      false,
			ServiceName:   "",
			ServiceStatus: "",
		},
	}

	if err := db.ReplaceHostPackages(host.ID, pkgs); err != nil {
		t.Fatalf("replace host packages: %v", err)
	}

	got, err := db.GetHostPackages(host.ID)
	if err != nil {
		t.Fatalf("get host packages: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 packages, got %d", len(got))
	}
	if got[0].Name != "nginx" || !got[0].IsManual {
		t.Errorf("unexpected package 0: %+v", got[0])
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -v ./internal/store -run TestHostPackagesAndProcessTelemetry`
Expected: FAIL (types / methods not defined)

- [ ] **Step 3: Implement minimal code in `internal/store/db.go`**

1. Tambahkan kolom migration di `initSchema()`:
```sql
CREATE TABLE IF NOT EXISTS host_packages (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    host_id INTEGER NOT NULL,
    name TEXT NOT NULL,
    version TEXT NOT NULL,
    size_bytes INTEGER NOT NULL DEFAULT 0,
    install_epoch INTEGER NOT NULL DEFAULT 0,
    is_manual INTEGER NOT NULL DEFAULT 0,
    service_name TEXT NOT NULL DEFAULT '',
    service_status TEXT NOT NULL DEFAULT '',
    last_scanned_at DATETIME NOT NULL,
    FOREIGN KEY(host_id) REFERENCES hosts(id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_host_packages_host_manual ON host_packages(host_id, is_manual);
```
2. Tambahkan kolom `top_cpu_processes`, `top_ram_processes`, `top_swap_processes` ke tabel `hosts` jika belum ada via `PRAGMA table_info`.
3. Definisikan struct `ProcessInfo` dan `HostPackage`.
4. Implementasikan `ReplaceHostPackages` dalam transaksi SQLite dan `GetHostPackages`.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test -v ./internal/store -run TestHostPackagesAndProcessTelemetry`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/store/db.go internal/store/db_test.go
git commit -m "feat(store): add host_packages schema and process telemetry fields"
```

---

### Task 2: Inspector - Process Telemetry Batching (Top CPU, RAM, Swap)

**Files:**
- Modify: `internal/inspector/inspector.go`
- Test: `internal/inspector/inspector_test.go`

**Interfaces:**
- Consumes: `sshrunner.Runner`, `store.Host`, `store.ProcessInfo`
- Produces:
  - `parseTopProcesses(output string) ([]store.ProcessInfo, error)`
  - `parseTopSwap(output string) ([]store.ProcessInfo, error)`
  - Update `SystemMetricsBatchCmd` to append top CPU and RAM process snapshots, plus conditional swap extraction.

- [ ] **Step 1: Write the failing test**

Tambahkan pengujian parser di `internal/inspector/inspector_test.go`:
```go
func TestParseTopProcesses(t *testing.T) {
	sampleOutput := `PID USER %CPU %MEM RSS COMMAND
1001 www-data 12.5 4.2 45000 /usr/sbin/nginx
1002 mysql 8.1 15.0 160000 /usr/sbin/mysqld
`
	procs := parseTopProcesses(sampleOutput)
	if len(procs) != 2 {
		t.Fatalf("expected 2 processes, got %d", len(procs))
	}
	if procs[0].PID != 1001 || procs[0].User != "www-data" || procs[0].CPU != 12.5 {
		t.Errorf("unexpected proc 0: %+v", procs[0])
	}
	if procs[0].RSSBytes != 45000*1024 {
		t.Errorf("unexpected RSS bytes: %d", procs[0].RSSBytes)
	}
}

func TestParseTopSwap(t *testing.T) {
	sampleSwap := `1002 mysql 153600 /usr/sbin/mysqld
1005 redis 20480 /usr/bin/redis-server
`
	swaps := parseTopSwap(sampleSwap)
	if len(swaps) != 2 {
		t.Fatalf("expected 2 swaps, got %d", len(swaps))
	}
	if swaps[0].PID != 1002 || swaps[0].SwapBytes != 153600*1024 {
		t.Errorf("unexpected swap 0: %+v", swaps[0])
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -v ./internal/inspector -run "TestParseTopProcesses|TestParseTopSwap"`
Expected: FAIL

- [ ] **Step 3: Implement minimal code in `internal/inspector/inspector.go`**

1. Tambahkan seksi ekstraksi proses di `SystemMetricsBatchCmd`:
   - Top 5 CPU: `ps -eo pid,user,%cpu,%mem,rss,comm --sort=-%cpu 2>/dev/null | head -n 6`
   - Top 5 RAM: `ps -eo pid,user,%cpu,%mem,rss,comm --sort=-rss 2>/dev/null | head -n 6`
   - Top 5 Swap: Awk membaca `/proc/*/status` kondisional jika Swap total & used > 0.
2. Implementasikan `parseTopProcesses` dan `parseTopSwap`.
3. Serialisasikan hasilnya ke dalam `h.TopCPUProcesses`, `h.TopRAMProcesses`, dan `h.TopSwapProcesses` di `scrapeSystemMetrics`.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test -v ./internal/inspector -run "TestParseTopProcesses|TestParseTopSwap"`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/inspector/inspector.go internal/inspector/inspector_test.go
git commit -m "feat(inspector): batch process telemetry for top CPU, RAM, and Swap"
```

---

### Task 3: Inspector - Package Inventory & Dormant Scanner

**Files:**
- Modify: `internal/inspector/inspector.go`
- Test: `internal/inspector/inspector_test.go`

**Interfaces:**
- Consumes: `sshrunner.Runner`, `store.Host`, `store.HostPackage`
- Produces:
  - `(ins *Inspector) ScanPackages(hostID int64) error`
  - Polyglot shell command for package managers (Debian/Ubuntu `dpkg`, RHEL/CentOS `rpm`, Alpine `apk`, Arch `pacman`).
  - Systemd service state correlator for `Dormant Package` detection.

- [ ] **Step 1: Write the failing test**

Tambahkan pengujian parser output package manager di `internal/inspector/inspector_test.go`:
```go
func TestParseDpkgPackages(t *testing.T) {
	dpkgOutput := `nginx	1.18.0-0ubuntu1	1024	installed
curl	7.68.0-1ubuntu2	512	installed
`
	manualList := map[string]bool{"nginx": true}
	services := map[string]string{"nginx": "active"}

	pkgs := parseDpkgOutput(dpkgOutput, manualList, services, 1)
	if len(pkgs) != 2 {
		t.Fatalf("expected 2 packages, got %d", len(pkgs))
	}
	if pkgs[0].Name != "nginx" || !pkgs[0].IsManual || pkgs[0].ServiceStatus != "active" {
		t.Errorf("unexpected package: %+v", pkgs[0])
	}
	if pkgs[1].Name != "curl" || pkgs[1].IsManual {
		t.Errorf("unexpected curl: %+v", pkgs[1])
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -v ./internal/inspector -run TestParseDpkgPackages`
Expected: FAIL

- [ ] **Step 3: Implement minimal code in `internal/inspector/inspector.go`**

1. Buat polyglot script untuk mendeteksi package manager dan mengekstrak daftar paket beserta status instalasi manual (`apt-mark showmanual`, `dnf history`, dll.) dan unit `systemd` aktif (`systemctl list-units --type=service --all`).
2. Implementasikan `parseDpkgOutput`, `parseRpmOutput`, `parseApkOutput`, `parsePacmanOutput`.
3. Tandai `Dormant Package` jika paket berstatus `IsManual`, memiliki file service terkait, namun status servicenya `inactive`, `failed`, atau `disabled`.
4. Implementasikan method `(ins *Inspector) ScanPackages(hostID int64) error` yang menjalankan scan, mem-parse hasil, dan memanggil `ins.db.ReplaceHostPackages(hostID, pkgs)`.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test -v ./internal/inspector -run TestParseDpkgPackages`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/inspector/inspector.go internal/inspector/inspector_test.go
git commit -m "feat(inspector): implement polyglot package inventory and dormant detection"
```

---

### Task 4: Web API Handlers (`/api/hosts/{id}/packages`)

**Files:**
- Modify: `internal/web/server.go`
- Test: `internal/web/server_test.go`

**Interfaces:**
- Consumes: `ins.ScanPackages`, `store.GetHostPackages`, `store.GetHost`
- Produces:
  - `GET /api/hosts/{id}/packages`: Mengembalikan list `HostPackage` dalam format JSON.
  - `POST /api/hosts/{id}/packages/scan`: Memulai pemindaian paket secara asinkron/sinkron dan mengembalikan daftar yang diperbarui.
  - Update `GET /api/hosts/{id}` response untuk menyertakan string serialized / objek `TopCPUProcesses`, `TopRAMProcesses`, `TopSwapProcesses`.

- [ ] **Step 1: Write the failing test**

Tambahkan pengujian rute API di `internal/web/server_test.go`:
```go
func TestHostPackagesAPI(t *testing.T) {
	ts, db := setupTestServer(t)
	defer ts.Close()

	host, _ := db.CreateHost(&store.Host{Name: "web1", Host: "10.0.0.1", Port: 22, User: "root"})
	_ = db.ReplaceHostPackages(host.ID, []*store.HostPackage{
		{HostID: host.ID, Name: "nginx", Version: "1.18", SizeBytes: 500000, IsManual: true},
	})

	// Test GET /api/hosts/{id}/packages
	res, err := http.Get(fmt.Sprintf("%s/api/hosts/%d/packages", ts.URL, host.ID))
	if err != nil {
		t.Fatalf("get packages: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", res.StatusCode)
	}

	var pkgs []*store.HostPackage
	_ = json.NewDecoder(res.Body).Decode(&pkgs)
	if len(pkgs) != 1 || pkgs[0].Name != "nginx" {
		t.Errorf("unexpected response: %+v", pkgs)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -v ./internal/web -run TestHostPackagesAPI`
Expected: FAIL (404 Not Found)

- [ ] **Step 3: Implement minimal code in `internal/web/server.go`**

Tambahkan rute dan handler:
- `s.mux.HandleFunc("/api/hosts/{id}/packages", s.handleHostPackages)`
- `s.mux.HandleFunc("/api/hosts/{id}/packages/scan", s.handleHostPackagesScan)`
Pastikan handler dilindungi oleh `authMiddleware`.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test -v ./internal/web -run TestHostPackagesAPI`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/web/server.go internal/web/server_test.go
git commit -m "feat(web): add REST API endpoints for package inventory and scan"
```

---

### Task 5: Web UI Frontend (Overview Widget & Packages Tab)

**Files:**
- Modify: `internal/web/static/index.html`
- Test: `internal/web/server_test.go` (or `pantau_test.go`)

**Interfaces:**
- Consumes: `/api/hosts/{id}` (Process Telemetry), `/api/hosts/{id}/packages` (Package Inventory)
- Produces:
  - Tab "Packages" di navigasi Workspace Modal Host Detail.
  - Section "Top Resource Consumers" di tab Overview dengan sub-tab atau kolom (Top CPU, Top RAM, Top Swap) + tombol terminal cepat.
  - Tabel Packages dengan filter: `[All]`, `[Manual Install]`, `[Dormant Packages]`, kotak pencarian instan, dan tombol *"Rescan Packages"*.

- [ ] **Step 1: Write the failing UI integration check**

Verifikasi bahwa HTML menyertakan elemen tab packages dan process telemetry di `pantau_test.go`:
```go
func TestUIPackageTabAndProcessTelemetryPresence(t *testing.T) {
	htmlContent, err := os.ReadFile("internal/web/static/index.html")
	if err != nil {
		t.Fatalf("read index.html: %v", err)
	}
	s := string(htmlContent)
	if !strings.Contains(s, `data-tab="packages"`) {
		t.Errorf("expected index.html to contain packages tab")
	}
	if !strings.Contains(s, `top-resource-consumers`) {
		t.Errorf("expected index.html to contain top-resource-consumers section")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -v -run TestUIPackageTabAndProcessTelemetryPresence`
Expected: FAIL

- [ ] **Step 3: Implement UI in `internal/web/static/index.html`**

1. Tambahkan tombol tab `<div class="tab" data-tab="packages" onclick="switchTab('packages')">Packages</div>` pada modal navigasi.
2. Tambahkan panel konten `<div id="tab-packages" class="tab-content">...</div>`:
   - Filter bar: tombol All, Manual, Dormant, Search input, tombol *"Rescan Packages"*.
   - Tabel responsif: Nama Paket, Versi, Ukuran Disk (KB/MB), Tipe (Manual / Base), Status Service (Active/Inactive), Aksi (Open Terminal).
3. Tambahkan widget *"Top Resource Consumers"* di tab Overview di bawah grafik resource:
   - Menampilkan mini table 5 proses teratas CPU, RAM, dan Swap dengan badge warna dan tombol pintasan terminal.
4. Tambahkan fungsi JavaScript `renderTopProcesses(host)` dan `loadHostPackages(hostId)` serta handler pencarian/filter.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test -v -run TestUIPackageTabAndProcessTelemetryPresence`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/web/static/index.html pantau_test.go
git commit -m "feat(ui): add Top Resource Consumers widget and Packages inventory tab"
```

---

### Task 6: Full Integration Verification & Changelog

**Files:**
- Modify: `CHANGELOG.md`
- Test: Full test suite `go test ./...`

- [ ] **Step 1: Run complete test suite**

Run: `go test ./...`
Expected: All tests pass across all packages (`pantau`, `inspector`, `store`, `web`, etc.).

- [ ] **Step 2: Update CHANGELOG.md**

Dokumentasikan penambahan fitur di seksi `[Unreleased]` berbasis standar Keep a Changelog:
```markdown
### Added
- Observabilitas Process Telemetry waktu-nyata untuk Top 5 proses pemakan CPU, RAM (RSS), dan Swap secara berkala.
- Widget Top Resource Consumers pada tab Overview dengan pintasan instan ke Terminal Dock.
- Modul Package Inventory agentless untuk mengaudit paket terpasang (Debian, RHEL, Alpine, Arch) dengan klasifikasi Manual Install vs Base System.
- Deteksi Dormant Package untuk mengidentifikasi aplikasi manual dengan layanan yang tidak aktif.
- Tab Packages di Workspace Modal dengan pencarian instan, filter kategori, dan tombol rescan on-demand.
```

- [ ] **Step 3: Commit**

```bash
git add CHANGELOG.md
git commit -m "docs(changelog): document Process Telemetry and Package Inventory features"
```
