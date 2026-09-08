# 10 — Key Provisioning (Auto-Inject SSH Public Key)

**What to build:** One-time password-based SSH Key Provisioning. When adding a new Host or clicking "Inject Key", an administrator can enter the target server's SSH password once. Pantau connects via SSH with password authentication, idempotently appends its public key to `~/.ssh/authorized_keys` with correct permissions (`0700` and `0600`), verifies key authentication, and immediately discards the password from memory without storing it in the database.

**Blocked by:** 01 — Host Management & SSH Connection Verification.

**Status:** resolved

- [x] Backend connects via SSH password authentication and idempotently injects the global ED25519 public key.
- [x] Correct permissions (`chmod 700 ~/.ssh` and `chmod 600 ~/.ssh/authorized_keys`) are ensured automatically.
- [x] Password is never persisted in SQLite; discarded from memory after one-time provisioning.
- [x] Add Host modal supports optional one-time password field to provision key on save.
- [x] "🔑 Inject Key" action on Host cards enables one-click key injection on demand.
- [x] Automated integration test verifies key provisioning and subsequent key-based command execution.
