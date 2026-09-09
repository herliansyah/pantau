# 14 — Terminal Presets & Split-Pane Observability

**What to build:** An interactive terminal enhancement allowing administrators to save, manage, and one-click launch predefined diagnostic commands (Terminal Presets) on target Hosts, with support for split-screen multi-terminal monitoring (side-by-side split pane) across the same or different Hosts.

**Blocked by:** 08 — Interactive Web Terminal (xterm.js via SSH WebSocket).

**Status:** resolved

- [x] Database schema and CRUD API for `terminal_presets` (Hybrid: Global when `host_id IS NULL`, Host-specific when `host_id` set).
- [x] Essential seed presets automatically populated on fresh setup (`htop`/`top`, `docker stats`, `journalctl -n 100 -f`).
- [x] Non-blocking shell pre-check wrapper detecting binary presence before command launch.
- [x] One-click preset trigger from Host Cards / Host Detail, and in-place preset toolbar in Web Terminal.
- [x] Side-by-side Split Pane in Workspace Modal with independent WebSocket connections and xterm.js instances.
- [x] Cross-Host support in secondary pane with Host selector dropdown and keyboard shortcut toggle (`Alt+\`).
- [x] In-place "Save as Preset" dialog directly accessible from terminal toolbar.
