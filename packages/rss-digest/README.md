# RSS Digest

Monitors an RSS feed, deduplicates items using persistent Storage, summarizes new entries with an LLM, and delivers a daily digest to Telegram. Works with any valid RSS 2.0 feed.

## What it does

1. Fetches the configured RSS feed via `Http.get`.
2. Parses `<item>` blocks to extract title, link, and description (no external XML library needed).
3. Compares links against the previously-seen list stored in `Storage`.
4. Summarizes up to `max_items` new entries using `Llm.generate`.
5. Sends the digest (summaries + links) to Telegram, then saves the new seen-links list.

The seen-links list is capped at 200 entries to prevent unbounded storage growth.

## Configuration

| Key | Type | Required | Default | Description |
|---|---|---|---|---|
| `telegram_token` | secret | yes | — | Your Telegram bot token |
| `chat_id` | string | yes | — | Telegram chat or channel ID |
| `feed_url` | string | no | `https://news.ycombinator.com/rss` | Full URL of any RSS 2.0 feed |
| `max_items` | number | no | `5` | Maximum number of new items to process per run |
| `schedule` | cron | no | `0 9 * * *` | When to run (09:00 daily by default) |

## Example Output

```
📰 RSS Digest — 2025-03-28
Source: https://news.ycombinator.com/rss
3 new item(s)

1. A new paper introduces a compact transformer architecture that...
2. Cloudflare announced free DDoS protection tiers for small projects...
3. A retrospective on 10 years of Rust in production at Mozilla shows...

Links
─────
1. Show HN: Compact Transformer for Edge Devices
2. Cloudflare Expands Free Tier with DDoS Protection
3. 10 Years of Rust at Mozilla

Powered by KittyPaw
```

## Data Source

- Any RSS 2.0 feed URL you configure.
- Default: **Hacker News** — `https://news.ycombinator.com/rss`
- Other examples: `https://feeds.bbci.co.uk/news/rss.xml`, `https://rss.nytimes.com/services/xml/rss/nyt/HomePage.xml`
