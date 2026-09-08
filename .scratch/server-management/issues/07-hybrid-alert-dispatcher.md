# 07 — Hybrid Alert Dispatcher (In-App & Telegram / Webhook)

**What to build:** A notification engine that publishes real-time alert badges to the web dashboard and delivers push alert messages (including Host name, Drift summary, and Root Cause Excerpt) to configured Telegram Bots and HTTP Webhooks when issues arise or resolve.

**Blocked by:** 05 — Root Cause Excerpt Diagnostic Capture & Incident Timeline, 06 — Backup Freshness & DB / Cron Monitoring.

**Status:** resolved

- [x] Settings page allows configuring Telegram Bot Token, Target Chat ID, and custom Webhook URLs with a "Test Notification" button.
- [x] Drift events generate instant In-App alert items with badge counters on the Host and global navigation.
- [x] Notification worker dispatches formatted Telegram messages containing the host name, alert details, and formatted Root Cause Excerpt snippet.
- [x] Alert resolution messages are dispatched when an issue is resolved.
- [x] Automated tests verify payload formatting and HTTP delivery to mock Telegram and webhook endpoints.
