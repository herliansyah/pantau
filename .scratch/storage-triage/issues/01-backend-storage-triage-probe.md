# 01 - Backend Storage Triage Probe

Status: resolved

Implementasi modul `internal/storagetriage` yang bertugas menjalankan sesi diagnosa penyimpanan (*Storage Triage Probe*) via SSH secara *agentless*, memparsing output POSIX standar (top directories, large files, runaway files, ghost files, reclaimable storage), serta mengimplementasikan *Triage Concurrency Guard* dan timeout 20 detik.

## Resolution
- Dibuat package `internal/storagetriage` dengan `RunTriage` dan `ExecuteReclaim`.
- Skrip POSIX batch dengan seksi pembatas mengekstrak `df`, `du -kx --max-depth=1`, `find` files >30MB, `find` mmin -60, `lsof +L1` / `/proc/*/fd` ghost files, serta catalog reclaimables.
- Dilengkapi unit test di `internal/storagetriage/storagetriage_test.go` yang memverifikasi parsing output, human byte conversion, dan security boundary validation.
