# 04 — Docker Container Monitoring, Actions, & Live Logs

**What to build:** An integrated Docker management pane for each Host showing running, stopped, and restarting containers. The administrator can click quick actions to Start, Stop, or Restart any container directly from the web interface, and open a live log streaming drawer to observe real-time container output.

**Blocked by:** 02 — Periodic Inspection & System Metrics Dashboard.

**Status:** resolved

- [x] Inspection pipeline collects Docker container status (`docker ps -a --format json` or template format).
- [x] UI displays container list with status, uptime, ports, and image details per Host.
- [x] Administrator can trigger Start, Stop, and Restart actions directly on target containers via SSH.
- [x] WebSocket endpoint streams live container logs (`docker logs -f --tail 100`) directly to the browser viewer.
- [x] Automated tests verify container listing parsing, action command execution, and live log stream handling.
