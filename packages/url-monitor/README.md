# URL Monitor

Periodically checks whether a configured URL is reachable. Stores the last known status in Storage and sends a Telegram alert only when the status changes — so you get notified on outages and recoveries without being spammed on every check.

## What it does

1. Attempts to fetch the configured `target_url` via `Http.get`.
2. Loads the previous status from Storage.
3. If the status changed (up → down or down → up), or this is the first check, sends a Telegram alert.
4. Saves the new status and timestamp to Storage for the next run.

By default the check runs every 5 minutes. Alerts are sent only on state transitions.

## Configuration

| Key | Type | Required | Default | Description |
|---|---|---|---|---|
| `telegram_token` | secret | yes | — | Your Telegram bot token |
| `chat_id` | string | yes | — | Telegram chat or channel ID |
| `target_url` | string | yes | — | The URL to monitor (e.g. `https://myapp.com/health`) |
| `schedule` | cron | no | `*/5 * * * *` | Check interval (every 5 minutes by default) |

## Example Alerts

**Outage detected:**
```
🔴 ALERT: https://myapp.com is DOWN

🕐 Checked at: 2025-03-28T09:15:00.000Z
📋 Detail: Error: connection refused

Powered by KittyPaw URL Monitor
```

**Recovery:**
```
✅ RECOVERY: https://myapp.com is back UP

🕐 Checked at: 2025-03-28T09:20:00.000Z
📋 Detail: Response received (1842 bytes)

Powered by KittyPaw URL Monitor
```

## Notes

- The monitor only alerts on **status changes**. If the site has been down for 3 consecutive checks, you receive only 1 alert.
- The Storage key is namespaced by URL, so you can run multiple instances monitoring different URLs without conflict.
- Suitable for monitoring health-check endpoints, APIs, or any publicly reachable URL.
