# 01 — Host Management & SSH Connection Verification

**What to build:** An administrative interface to configure and test SSH connections to target Linux hosts. An administrator can log in, generate a shared Pantau SSH public key, register a new Host (hostname/IP, SSH port, user, optional private key override), verify the SSH connection with one click, and view basic detected OS information (OS name, version, kernel, uptime).

**Blocked by:** None — can start immediately.

**Status:** resolved

- [x] Admin authentication is functional using a secure password session.
- [x] Pantau generates a default SSH keypair if none exists and displays the public key for easy copy-pasting.
- [x] User can add, edit, and delete Host records in SQLite.
- [x] "Test Connection" button executes an SSH probe against the target host (or mock runner in tests) and verifies credentials.
- [x] Detected basic system properties (OS, kernel version, uptime) are stored and displayed on the Host detail page.
- [x] Includes an automated end-to-end test using an SSH test seam verifying host addition and connection verification.
