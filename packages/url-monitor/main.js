// url-monitor/main.js
// Checks if a configured URL is reachable via Http.get.
// Stores the last known status in Storage.
// Sends a Telegram alert only when the status changes (up → down or down → up).

const ctx = JSON.parse(__context__);
const config = ctx.config || {};

const telegramToken = config.telegram_token;
const chatId = config.chat_id;
const targetUrl = config.target_url;

if (!targetUrl) {
  return "Error: target_url is not configured.";
}

const STORAGE_KEY = `url_monitor_status_${targetUrl.replace(/[^a-zA-Z0-9]/g, "_")}`;
const now = new Date().toISOString();

// --- Load last known status from Storage ---
// Stored as: { status: "up" | "down", checkedAt: ISO string }
async function loadLastStatus() {
  try {
    const raw = await Storage.get(STORAGE_KEY);
    if (!raw) return null;
    const parsed = JSON.parse(raw);
    const value = parsed.value !== undefined ? parsed.value : parsed;
    return typeof value === "string" ? JSON.parse(value) : value;
  } catch (e) {
    return null;
  }
}

// --- Check if the URL is reachable ---
// Http.get throws (or returns an error body) if the request fails.
// We treat any successful response as "up".
async function checkUrl(url) {
  try {
    const response = await Http.get(url);
    // If Http.get returns a non-empty string without throwing, the URL is up.
    // Some implementations may return an object with a status field.
    if (typeof response === "string") {
      return { up: true, detail: `Response received (${response.length} bytes)` };
    }
    return { up: true, detail: "OK" };
  } catch (e) {
    return { up: false, detail: String(e) };
  }
}

// --- Main ---
const lastStatus = await loadLastStatus();
const check = await checkUrl(targetUrl);
const currentStatus = check.up ? "up" : "down";

// Persist the new status
await Storage.set(STORAGE_KEY, JSON.stringify({ status: currentStatus, checkedAt: now }));

// Determine if we need to send an alert
const previousStatus = lastStatus ? lastStatus.status : null;
const statusChanged = previousStatus !== null && previousStatus !== currentStatus;
const isFirstCheck = previousStatus === null;

let alertSent = false;

if (statusChanged || isFirstCheck) {
  let emoji, headline, detail;

  if (isFirstCheck) {
    emoji = check.up ? "✅" : "🔴";
    headline = check.up
      ? `URL Monitor started — ${targetUrl} is UP`
      : `URL Monitor started — ${targetUrl} is DOWN`;
    detail = check.detail;
  } else if (currentStatus === "down") {
    emoji = "🔴";
    headline = `ALERT: ${targetUrl} is DOWN`;
    detail = `Was UP since last check.\nError: ${check.detail}`;
  } else {
    emoji = "✅";
    headline = `RECOVERY: ${targetUrl} is back UP`;
    detail = `Was DOWN since last check (${lastStatus.checkedAt}).`;
  }

  const message = [
    `${emoji} *${headline}*`,
    ``,
    `🕐 Checked at: ${now}`,
    `📋 Detail: ${detail}`,
    ``,
    `_Powered by KittyPaw URL Monitor_`,
  ].join("\n");

  await Telegram.sendMessage(telegramToken, chatId, message, { parse_mode: "Markdown" });
  alertSent = true;
}

// Return a summary string for logging
const summary = [
  `URL: ${targetUrl}`,
  `Status: ${currentStatus.toUpperCase()}`,
  `Previous: ${previousStatus || "unknown (first check)"}`,
  `Alert sent: ${alertSent}`,
  `Checked at: ${now}`,
].join(" | ");

return summary;
