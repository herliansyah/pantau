# 02 — Periodic Inspection & System Metrics Dashboard

**What to build:** An automated background worker that executes periodic agentless SSH inspections across all registered Hosts to gather CPU load average, RAM utilization, and disk space usage. Calculates the Host Lifecycle Score (0-100) based on OS EOL status and resource saturation, and displays metrics on a live dashboard with clear warning indicators.

**Blocked by:** 01 — Host Management & SSH Connection Verification.

**Status:** resolved

- [x] Background goroutine executes periodic inspections on configurable intervals (default: 5 minutes).
- [x] Agentless SSH execution gathers load averages, memory usage (`free`), and disk filesystem usage (`df`).
- [x] Lifecycle Score algorithm evaluates OS EOL proximity, sustained CPU/RAM load, and dmesg I/O errors into a 0-100 score.
- [x] Dashboard displays Host cards with real-time health badges, resource usage bars, and Lifecycle recommendations.
- [x] Includes tests verifying metrics collection parsing and Lifecycle Score calculations against simulated SSH responses.
