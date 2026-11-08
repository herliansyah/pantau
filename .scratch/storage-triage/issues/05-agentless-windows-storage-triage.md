# 05 - Agentless Windows Storage Triage and Safe Reclaim

Type: task
Status: resolved

Ekstensi kapabilitas Storage Triage dan Safe Reclaim untuk Host target berbasis Windows Server (2019/2022/2025) dan Windows 10/11 secara agentless melalui eksekusi PowerShell berkode Base64 UTF-16LE via SSH:
- Pembacaan kapasitas partisi drive sistem (`C:`) via CIM `Win32_LogicalDisk`.
- Pemetaan Space Hogs direktori tingkat pertama berkecepatan tinggi via `robocopy /L` (list-only zero-copy) dengan fallback rekursif `Get-ChildItem` dan penemuan folder non-sistem dinamis.
- Pemindaian berkas besar (>15MB) dan runaway files termodifikasi dalam 60 menit terakhir dalam satu putaran (*single-pass*), dengan proteksi eksklusi mutlak berkas DBMS (`.mdf`, `.ldf`, `.ndf`, `.edb`, `.dit`, `.sqlite*`, `.vhdx`, `.avhdx`, `.bak`) dan arsip (`.7z`, `.rar`, `.cab`).
- Katalog Safe Reclaim terkurasi untuk Windows: Windows Update download cache (`SoftwareDistribution\Download`), Temp sistem dan user (`C:\Windows\Temp`, `%TEMP%`), Error Reports & Crash Dumps (`WER`, `Minidump`, `CrashDumps`, `MEMORY.DMP`), Keranjang Sampah (`Clear-RecycleBin`), arsip log IIS usang (>7 hari), serta Docker on Windows.
- Prosedur pengosongan log in-place aman (*Log Truncation*) via `Clear-Content -LiteralPath '...' -Force` dengan batasan keamanan ketat terhadap direktori sistem Windows (`System32`, `SysWOW64`, `WinSxS`, `Boot`, `Recovery`), swap/pagefiles (`pagefile.sys`, `swapfile.sys`, `hiberfil.sys`, `dumpstack.log`), dan database.
- Graceful Windows Service Reload (`Restart-Service -Name '...' -Force`).

## Resolution
- Diimplementasikan di `internal/storagetriage/storagetriage.go` (`ProbeWindowsStorageTriage`, `ReclaimWindowsStorageItem`, `TruncateWindowsLogFile`, `WindowsTriageBatchScript`).
- Unit test di `internal/storagetriage/storagetriage_test.go` dan `internal/web/storage_triage_test.go`.
- Dokumentasi di [ADR-0039 Bagian 7](../../docs/adr/0039-agentless-storage-triage-runaway-detection-and-safe-reclaim.md) dan [ADR-0040](../../docs/adr/0040-agentless-windows-target-observability.md).
