# 08 — Interactive Web Terminal (xterm.js via SSH WebSocket)

**What to build:** An in-browser interactive terminal connected to target Linux hosts. Administrators can click "Open Terminal" on any Host to launch an interactive pseudo-terminal (PTY) session powered by xterm.js communicating with the backend over a secure WebSocket SSH proxy.

**Blocked by:** 01 — Host Management & SSH Connection Verification.

**Status:** resolved

- [x] WebSocket endpoint authenticates the admin session and opens an SSH PTY session (`xterm-256color`) to the target host.
- [x] Bidirectional terminal streaming handles raw keystrokes, window resizing (SIGWINCH), and ANSI escape sequences.
- [x] UI provides a clean modal terminal window powered by embedded xterm.js and fit addon.
- [x] Clean teardown on tab close, browser disconnect, or SSH exit.
- [x] Automated integration tests verify WebSocket connection establishment and command transmission against a mock SSH terminal server.
