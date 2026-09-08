# 05 — Root Cause Excerpt Diagnostic Capture & Incident Timeline

**What to build:** An automated diagnostic capture mechanism that triggers immediately when a service or container crashes or experiences Drift. Pantau executes targeted diagnostic inspection commands (`docker inspect`, `docker logs --tail 50`, `journalctl -xeu <service> -n 50`) and stores the resulting Root Cause Excerpt in an incident timeline log, showing administrators exactly why a state change occurred.

**Blocked by:** 03 — Desired State Baseline & Drift Engine, 04 — Docker Container Monitoring, Actions, & Live Logs.

**Status:** resolved

- [x] On drift trigger (e.g. container exited or service stopped), an automated diagnostic command is immediately run via SSH.
- [x] Extracts exit codes, OOMKilled flags, and the last 50 error log lines as a Root Cause Excerpt.
- [x] Incident timeline in the Host detail view displays chronologically sorted state transition cards with formatted log excerpts.
- [x] Automated tests simulate a container failure and assert that the correct exit code and error logs are stored in the incident record.
