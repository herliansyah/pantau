# 03 — Desired State Baseline & Drift Engine

**What to build:** A 1-click baseline generator that detects current services, mounts, and containers to populate a Host's Desired State checklist. The engine periodically compares Actual State against Desired State to detect Drift (e.g. disk threshold exceeded, required container missing, unexpected state change) and logs state transition events.

**Blocked by:** 02 — Periodic Inspection & System Metrics Dashboard.

**Status:** resolved

- [x] "Generate Baseline from Current" button detects running containers, mounts, and active services to create a Desired State policy.
- [x] User can customize and toggle individual rules in the Desired State checklist per Host.
- [x] Comparison engine evaluates Actual State against Desired State during each periodic inspection.
- [x] Any discrepancy is flagged as Drift and recorded in the host state history.
- [x] Automated tests verify baseline generation, rule editing, and positive/negative drift detection logic.
