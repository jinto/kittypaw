# Weather Briefing

Sends a daily weather forecast to Telegram for any city. Fetches a 7-day forecast from the Open-Meteo API (free, no API key needed) and uses an LLM to write a friendly natural-language summary with practical tips.

## What it does

1. Calls the Open-Meteo forecast API using the configured latitude and longitude.
2. Retrieves daily max/min temperature and precipitation for the next 7 days.
3. Asks the LLM to write a 2–4 sentence briefing highlighting today's conditions and notable changes.
4. Sends the formatted message to Telegram every morning at 07:00.

## Configuration

| Key | Type | Required | Default | Description |
|---|---|---|---|---|
| `telegram_token` | secret | yes | — | Your Telegram bot token |
| `chat_id` | string | yes | — | Telegram chat or channel ID |
| `city` | string | no | `Seoul` | City name (display only — not used for geocoding) |
| `latitude` | string | no | `37.57` | Latitude of the city |
| `longitude` | string | no | `126.98` | Longitude of the city |
| `schedule` | cron | no | `0 7 * * *` | When to run (07:00 daily by default) |

To find coordinates for your city, use [latlong.net](https://www.latlong.net/).

## Example Output

```
🌤 Weather Briefing — Seoul (2025-03-28)

7-Day Forecast
──────────────
2025-03-28  12.3°C / 4.1°C  rain:0.0mm
2025-03-29  14.7°C / 5.8°C  rain:2.3mm
2025-03-30  10.2°C / 3.0°C  rain:8.1mm
...

Summary
───────
Seoul will enjoy mild and dry conditions today with a high of 12°C —
a good day to be outside. Rain is expected Sunday and Monday, so keep
an umbrella handy this weekend. Temperatures dip back to single digits
by Tuesday, so pack a layer if you're heading out in the evening.
```

## Data Source

- **Open-Meteo** — `https://api.open-meteo.com/v1/forecast`
- Completely free, no account or API key required.
- Attribution: [open-meteo.com](https://open-meteo.com/)
