# 0005. Legacy Server SSH & Platform Compatibility

Date: 2026-09-09

## Status

Accepted

## Context

Target Linux environments frequently include legacy production servers (such as CentOS 6 / RHEL 6, Debian 7/8, or Ubuntu 12.04/14.04). These environments exhibit significant divergence from modern Linux distributions:
1. **Cryptographic Limitations**: CentOS 6 runs OpenSSH 5.3p1 (OpenSSL 1.0.1e). It lacks support for Ed25519 public keys (introduced in OpenSSH 6.5) and relies on legacy Key Exchanges (`diffie-hellman-group14-sha1`, `diffie-hellman-group1-sha1`), older ciphers (`aes128-cbc`, `3des-cbc`), and SHA-1 RSA host keys (`ssh-rsa`), all of which are disabled or deprioritized by modern Go `golang.org/x/crypto/ssh` defaults.
2. **Init Systems & Diagnostics**: CentOS 6 utilizes SysVinit and Upstart rather than systemd. Utilities such as `systemctl` and `journalctl` do not exist; service supervision is handled by `/sbin/service` and logs are written directly to `/var/log/*`.
3. **Core Utility & Flag Drift**: Modern flags like `dmesg --level` (added in util-linux 2.23) and `ss -H` fail on older distributions. Furthermore, older `free -b` commands (procps 3.2.x) report used memory inclusive of filesystem buffer and cache, risking false-positive memory exhaustion alerts without parsing the `-/+ buffers/cache` line.

## Decision

1. **Universal RSA 4096-bit Global Key**:
   - For fresh installations, generate an RSA 4096-bit keypair by default instead of Ed25519. RSA 4096 provides strong modern cryptographic security while maintaining universal backward compatibility with OpenSSH 5.x through latest OpenSSH releases.
   - For existing installations with Ed25519 keys, preserve stored keys non-destructively and offer an optional key regeneration action in settings.
2. **Permissive SSH Cryptographic Profile in ClientConfig**:
   - Explicitly configure `ssh.ClientConfig` to allow legacy Key Exchange algorithms (`diffie-hellman-group14-sha1`, `diffie-hellman-group1-sha1`, `diffie-hellman-group-exchange-sha1`, `diffie-hellman-group-exchange-sha256`) and legacy ciphers (`aes128-cbc`, `aes256-cbc`, `3des-cbc`), while ordering modern algorithms first.
   - Include `ssh-rsa` alongside modern RSA and Ed25519 algorithms in `HostKeyAlgorithms`.
3. **Universal Polyglot Batch Shell Inspection**:
   - Update `SystemMetricsBatchCmd` to read OS identity from `/etc/os-release`, `/usr/lib/os-release`, `/etc/redhat-release`, and `/etc/centos-release`.
   - Remove `--level` from `dmesg` disk error checks in favor of portable pattern filtering.
   - Fall back from `ss` to standard `ss` (without `-H`) and `netstat` for connection and port tracking.
   - Enhance `parseFree` to subtract buffers and cache when reading pre-3.14 kernel / procps memory metrics.
4. **Polyglot Service Status & Root Cause Excerpts**:
   - Service state check: test `systemctl is-active <svc>` if `systemctl` exists, otherwise fallback to `service <svc> status` (inspecting for running status).
   - Root Cause Excerpt: fall back to tailing `/var/log/<svc>*.log` or `/var/log/messages` when `journalctl` is unavailable.
   - Service Discovery: expand baseline discovery to recognize RedHat/CentOS service naming conventions (`mysqld`, `httpd`).

## Consequences

- **Broad Target Reach**: Pantau seamlessly manages ancient enterprise installations without requiring manual agent installation or custom per-server SSH configs.
- **Security Trade-Off**: Enabling legacy key exchanges and ciphers exposes SSH connections to older cryptographic primitives if the remote server does not negotiate modern algorithms. Mitigated by maintaining modern algorithms at highest priority during cipher negotiation.
- **Zero Agent Overhead**: Maintained pure single-binary, agentless pull architecture across both modern and legacy targets.
