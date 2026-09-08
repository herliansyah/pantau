# 11 — Cross-Host Transfer (Piped Streaming & Progress Tracking)

**What to build:** An in-memory piped streaming transfer engine to copy files and large folders (up to 500GB) directly between two registered Hosts without staging files on the Pantau master disk. Supports Fast Mode and Verified Mode (SHA256 checksum), background transfer jobs with real-time throughput metrics (MB/s, Transferred/Total, ETA), cancellation control, and a user-friendly UI integrated in the File Manager with an Active Transfers drawer.

**Blocked by:** 09 — SFTP File Manager & Lightweight Code/Text Editor.

**Status:** resolved

- [x] Piped streaming pipeline connects Source and Destination SSH sessions with zero temporary disk usage on master.
- [x] Handles single files and full recursive directories using native POSIX tar streams.
- [x] Fast Mode executes direct stream; Verified Mode computes and validates SHA256 checksums.
- [x] Real-time progress tracker measures throughput (MB/s), transferred percentage, and ETA.
- [x] Background job manager allows concurrent transfers and safe cancellation.
- [x] File Manager UI provides "🚀 Copy to Host" action modal with destination host picker and path input.
- [x] Active Transfers drawer displays animated progress bars and live status.
- [x] Automated tests verify single file and folder streaming, progress accounting, and cancellation.
