# 04 - Load Throttling, Cooldown, and Security Hardening

Type: task
Status: resolved

Implementasi mekanisme load-aware throttling, dynamic cooldowns, emergency force scan, serta security and robustness hardening pada modul Storage Triage:
- Throttling berdasarkan beban I/O Host target (cooldown 2m normal, 5m jika write 15–40 MB/s, pemblokiran jika >40 MB/s atau CPU load > 2x cores).
- Dukungan parameter `?force=true` untuk penyelidikan darurat.
- Reset cooldown instan otomatis setelah aksi reclaim atau log truncation berhasil.
- Proteksi path traversal dan pencegahan pemangkasan direktori sistem pada `TruncateLog`.
- Inspeksi ghost file yang robust menggunakan `stat -L -c %s "$fd"` pada `/proc/*/fd`.
- Perbaikan pemisahan spasi nama direktori Space Hogs pada parser triage.

## Resolution
- Ditambahkan evaluasi beban host di `internal/storagetriage/storagetriage.go` (`EvaluateHostLoad`, `CanRunTriage`, cooldown state).
- Diperketat validasi `TruncateLog` dengan `path.Clean`, penolakan path relatif traversal, dan whitelist/blacklist direktori sistem.
- Diperbaiki skrip probe `TriageProbeScript` untuk `lsof` dan fallback `/proc/*/fd` dengan `stat -L` byte calculation.
- Diperbaiki parser `SpaceHogs` untuk mempertahankan nama direktori yang memiliki spasi.
- Unit test komprehensif di `internal/storagetriage/storagetriage_test.go` dan `internal/web/storage_triage_test.go`.
