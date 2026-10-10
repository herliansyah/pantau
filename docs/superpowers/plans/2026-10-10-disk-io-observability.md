# Disk I/O & Speedometer Observability Implementation Plan

**Goal:** Implement real-time Disk Read/Write rate observability (Speedometer delta/sec & cumulative bytes) in Pantau's agentless SSH architecture, persisted in SQLite, and presented in the Web UI dashboard and host detail modal.

**Architecture Decisions:**
- Telemetry extracted via standard `/proc/diskstats` filtered with POSIX `awk` regex whitelist (`sd*`, `vd*`, `nvme*n*`, `xvd*`, `mmcblk*`) multiplied by 512-byte sector size.
- Database: 4 new columns in `hosts` table (`disk_read_bytes`, `disk_write_bytes`, `disk_read_speed_bps`, `disk_write_speed_bps`).
- Reboot protection: if delta bytes < 0, reset speed to 0 bps and take new counter as baseline.
- Web UI: 4-column compact grid row in Host Card alongside Network Traffic, dedicated observability card in Host Detail modal.

---

### Proposed Changes

#### Database Layer
- [internal/store/db.go](file:///home/ian/pantau-tripodfish/internal/store/db.go):
  - Add fields to `Host` struct: `DiskReadBytes`, `DiskWriteBytes`, `DiskReadSpeedBps`, `DiskWriteSpeedBps`.
  - Add idempotent `alterAddColumn` statements in `initSchema()`.
  - Update `GetHost`, `ListHosts`, and `UpdateHostInspection` SQL queries and scans to include the 4 new columns.
- [internal/store/db_test.go](file:///home/ian/pantau-tripodfish/internal/store/db_test.go):
  - Unit test verifying persistence and retrieval of disk I/O metrics.

#### Collector / Inspector Layer
- [internal/inspector/inspector.go](file:///home/ian/pantau-tripodfish/internal/inspector/inspector.go):
  - Update `SystemMetricsBatchCmd` to append a new section:
    `cat /proc/diskstats 2>/dev/null | awk '{if ($3 ~ /^([hsv]d[a-z]|nvme[0-9]+n[0-9]+|xvd[a-z]|mmcblk[0-9]+)$/) {r+=$6; w+=$10}} END {print (r?r:0)*512, (w?w:0)*512}'`
  - Add `parseDiskIOMetrics(h *store.Host, diskStatsSec string)` with delta rate calculation and reboot rollover handling.
  - Wire into `scrapeSystemMetrics`.
- [internal/inspector/inspector_test.go](file:///home/ian/pantau-tripodfish/internal/inspector/inspector_test.go):
  - Unit tests for `parseDiskIOMetrics`:
    - Normal inspection with positive delta.
    - Subsequent inspection with reboot (counter drop to smaller number).
    - Initial inspection (first run).

#### Web UI Layer
- [internal/web/static/index.html](file:///home/ian/pantau-tripodfish/internal/web/static/index.html):
  - Update Host Card HTML rendering to display both Network (⬇️/⬆️) and Disk I/O (📖/✍️) in compact layout.
  - Update Host Detail modal overview grid to display dedicated Disk I/O card with live rates and cumulative counters.
  - Add i18n keys for English and Indonesian (`traffic_disk`, `disk_io_rate`, `read_rate`, `write_rate`).

---

### Verification Plan

1. **Unit Tests**:
   - `go test -run TestDiskIOMetrics ./internal/inspector/...`
   - `go test -run TestHostDiskIOStorage ./internal/store/...`
2. **Integration Tests**:
   - `go test -run TestTicket ./...`
3. **Build & Sanity**:
   - `go build -o /dev/null main.go`
