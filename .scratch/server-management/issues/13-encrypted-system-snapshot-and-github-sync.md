# 13 — Encrypted System Snapshot and GitHub Sync

**What to build:** System Snapshot export/import with AES-256-GCM encryption derived from a user Snapshot Passphrase, automated and manual GitHub Private Repository synchronization via GitHub REST API (HTTPS + Personal Access Token), periodic background backup scheduler, and a First-Boot Web UI Wizard allowing new installations to restore from a local `.enc` file or pull directly from a private GitHub repo.

**Blocked by:** 01 — Host Management, 07 — Alert & Settings.

**Status:** resolved

- [x] Create `internal/snapshot` package for encrypting and decrypting database configuration (Hosts, Desired Rules, Settings) with AES-256-GCM and PBKDF2/SHA-256 key derivation.
- [x] Implement GitHub REST API client using standard library `net/http` to read and commit `pantau-state.enc` to private GitHub repositories.
- [x] Add background periodic backup worker and manual trigger API endpoints (`/api/snapshot/export`, `/api/snapshot/import`, `/api/snapshot/github/sync`, `/api/snapshot/github/restore`).
- [x] Add First-Boot Wizard in Web UI (`index.html`) when database is unconfigured/fresh, offering Restore from File, Restore from GitHub, or Start Fresh.
- [x] Add Snapshot & GitHub Backup management section in the Settings modal with interval configuration, manual download, manual sync, and status display.
- [x] Add end-to-end automated tests verifying snapshot encryption/decryption, export/import roundtrip, GitHub API mock sync/restore, and first-boot detection.
