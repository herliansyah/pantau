# 06 — Backup Freshness & DB / Cron Monitoring

**What to build:** Targeted inspection rules to verify Backup Freshness (verifying that backup files exist at specified paths, have size > 0, and have an mtime within the expected window like 24h), crontab job configurations, and local database services (MySQL, PostgreSQL systemd or container status) against the Host's Desired State.

**Blocked by:** 03 — Desired State Baseline & Drift Engine.

**Status:** resolved

- [x] Inspection pipeline checks defined backup file paths via SSH for file existence, size in bytes, and last modification timestamp.
- [x] Stale or empty backups trigger a Backup Freshness drift warning.
- [x] Database service availability (systemd unit status or DB container health) is evaluated against desired rules.
- [x] Host crontab entries (`crontab -l`) are tracked and verified against expected cron schedules.
- [x] Automated tests verify backup freshness evaluation with valid, empty, and expired mock backup files.
