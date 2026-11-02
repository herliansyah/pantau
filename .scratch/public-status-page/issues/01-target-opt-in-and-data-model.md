# 01 — Model Data Status Target & Skema Agregasi Rollup

Type: grilling
Status: resolved
Blocked by:

## Question

Bagaimana struktur skema SQLite untuk `status_page_targets`, `status_page_daily_rollups`, `status_page_incidents`, dan `status_page_incident_updates`? Bagaimana integritas relasi foreign key dan indexing dirancang agar kueri status publik selalu instan (< 5 ms) serta menjamin *data masking* (anonymisasi) total agar informasi sensitif host (alamat IP privat, port SSH internal, path file sistem) tidak pernah tersimpan di tabel publik atau bocor ke publik?

## Answer

### 1. Desain Skema Basis Data SQLite

Empat tabel flat baru dirancang dengan pemisahan tegas antara entitas konfigurasi/pemetaan publik dan rekaman telemetri privat:

```sql
-- 1. Status Targets (Entitas Publik Hasil Opt-In)
CREATE TABLE IF NOT EXISTS status_page_targets (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    target_type TEXT NOT NULL CHECK(target_type IN ('host', 'desired_rule')),
    host_id INTEGER NOT NULL REFERENCES hosts(id) ON DELETE CASCADE,
    desired_rule_id INTEGER NULL REFERENCES desired_rules(id) ON DELETE CASCADE,
    public_name TEXT NOT NULL,
    group_name TEXT NOT NULL DEFAULT 'Services',
    sort_order INTEGER NOT NULL DEFAULT 0,
    enabled INTEGER NOT NULL DEFAULT 1,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(target_type, host_id, desired_rule_id)
);
CREATE INDEX IF NOT EXISTS idx_status_targets_display 
    ON status_page_targets(enabled, group_name, sort_order ASC);

-- 2. Daily Uptime Rollups (Agregasi 90 Hari Kompak)
CREATE TABLE IF NOT EXISTS status_page_daily_rollups (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    target_id INTEGER NOT NULL REFERENCES status_page_targets(id) ON DELETE CASCADE,
    date TEXT NOT NULL, -- Format ISO 'YYYY-MM-DD'
    total_checks INTEGER NOT NULL DEFAULT 0,
    successful_checks INTEGER NOT NULL DEFAULT 0,
    degraded_checks INTEGER NOT NULL DEFAULT 0,
    outage_checks INTEGER NOT NULL DEFAULT 0,
    uptime_percentage REAL NOT NULL DEFAULT 100.0,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(target_id, date)
);
CREATE INDEX IF NOT EXISTS idx_status_rollups_lookup 
    ON status_page_daily_rollups(target_id, date DESC);
CREATE INDEX IF NOT EXISTS idx_status_rollups_date 
    ON status_page_daily_rollups(date);

-- 3. Status Incidents (Pengumuman Resmi Insiden / Pemeliharaan)
CREATE TABLE IF NOT EXISTS status_page_incidents (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    title TEXT NOT NULL,
    status TEXT NOT NULL CHECK(status IN ('investigating', 'identified', 'monitoring', 'resolved')),
    impact TEXT NOT NULL DEFAULT 'minor' CHECK(impact IN ('none', 'minor', 'major', 'critical')),
    message TEXT NOT NULL,
    affected_targets TEXT DEFAULT '[]', -- JSON array of target_ids
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    resolved_at DATETIME NULL
);
CREATE INDEX IF NOT EXISTS idx_status_incidents_active 
    ON status_page_incidents(status, created_at DESC);

-- 4. Status Incident Updates (Riwayat Kronologis Penanganan)
CREATE TABLE IF NOT EXISTS status_page_incident_updates (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    incident_id INTEGER NOT NULL REFERENCES status_page_incidents(id) ON DELETE CASCADE,
    status TEXT NOT NULL CHECK(status IN ('investigating', 'identified', 'monitoring', 'resolved')),
    message TEXT NOT NULL,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_status_updates_incident 
    ON status_page_incident_updates(incident_id, created_at ASC);
```

### 2. Jaminan Data Masking & Isolasi Privasi (Zero Data Leakage)
1. **Tidak Ada Redundansi Data Sensitif**: Kolom internal seperti alamat IP host (`hosts.host`), port SSH (`hosts.port`), kredensial akun (`hosts.user`, `hosts.custom_key`), path file sistem internal, atau excerpt log kesalahan (`root_cause_excerpt`) sama sekali tidak disimpan pada tabel-tabel publik.
2. **Abstraksi Nama Publik (*Public Display Name*)**: Entitas publik hanya dikenal melalui `public_name` (misalnya: "Public Web Portal", "Production API Gateway") yang diinput secara eksplisit oleh administrator saat melakukan opt-in.
3. **Payload Ringan Tanpa Relasi Bocor**: Handler publik hanya membaca proyeksi field `id`, `public_name`, `group_name`, status kualitatif saat ini (`operational`, `degraded`, `outage`), dan deretan persentase ketersediaan 90 hari.

### 3. Kinerja Kueri & Strategi UPSERT
- **Kueri Instan (< 5 ms)**: Index komposit `(target_id, date DESC)` menjamin pengambilan riwayat 90 hari terakhir per target dieksekusi dengan *Index Scan* langsung tanpa table scan. Dengan limit 90 baris per target, payload kueri SQLite sangat kecil (< 10 KB total untuk puluhan target).
- **Operasi UPSERT Atomik**: Pembaruan status harian menggunakan sintaks SQLite `ON CONFLICT(target_id, date) DO UPDATE SET total_checks = total_checks + 1, ...`, mencegah race condition antar-goroutine inspeksi.
- **Konfigurasi Branding Terpusat**: Pengaturan `status_page_enabled`, `status_page_title`, `status_page_organization`, `status_page_header_text`, `status_page_logo_url`, dan `status_page_support_url` disatukan pada tabel `settings` yang sudah ada.
