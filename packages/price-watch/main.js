// price-watch/main.js
// Fetches a product page, extracts the price via structured data or HTML patterns,
// and sends a Telegram alert when the price drops below a target or changes.
// Tracks price history over time in Storage.

const ctx = JSON.parse(__context__);
const config = ctx.config || {};

const telegramToken = config.telegram_token;
const chatId = config.chat_id;
const productUrl = config.product_url;
const targetPrice = Number(config.target_price) || 0;

if (!productUrl) {
  return "Error: product_url is not configured.";
}

// ---------------------------------------------------------------------------
// Price extraction — three strategies, tried in order of reliability
// ---------------------------------------------------------------------------

function extractFromJsonLd(html) {
  // Match all JSON-LD blocks; product data may live in any one of them.
  const blocks = [];
  const re = /<script[^>]*type\s*=\s*["']application\/ld\+json["'][^>]*>([\s\S]*?)<\/script>/gi;
  let m;
  while ((m = re.exec(html)) !== null) {
    blocks.push(m[1]);
  }

  for (const block of blocks) {
    try {
      const obj = JSON.parse(block);
      const price = digPrice(obj);
      if (price !== null) return price;
    } catch (_) {
      // malformed JSON-LD — skip
    }
  }
  return null;
}

// Recursively dig for a numeric "price" field inside JSON-LD objects/arrays.
function digPrice(node) {
  if (Array.isArray(node)) {
    for (const item of node) {
      const p = digPrice(item);
      if (p !== null) return p;
    }
    return null;
  }
  if (node && typeof node === "object") {
    // Direct "price" field (Product, Offer, etc.)
    if (node.price !== undefined) {
      const n = parseKoreanPrice(String(node.price));
      if (n > 0) return n;
    }
    // Offers may be nested
    if (node.offers) {
      const p = digPrice(node.offers);
      if (p !== null) return p;
    }
    // lowPrice is common in AggregateOffer
    if (node.lowPrice !== undefined) {
      const n = parseKoreanPrice(String(node.lowPrice));
      if (n > 0) return n;
    }
  }
  return null;
}

function extractFromMetaTags(html) {
  // og:price:amount, product:price:amount, og:price
  const patterns = [
    /property\s*=\s*["']og:price:amount["'][^>]*content\s*=\s*["']([^"']+)["']/i,
    /content\s*=\s*["']([^"']+)["'][^>]*property\s*=\s*["']og:price:amount["']/i,
    /property\s*=\s*["']product:price:amount["'][^>]*content\s*=\s*["']([^"']+)["']/i,
    /content\s*=\s*["']([^"']+)["'][^>]*property\s*=\s*["']product:price:amount["']/i,
  ];

  for (const pattern of patterns) {
    const m = html.match(pattern);
    if (m) {
      const n = parseKoreanPrice(m[1]);
      if (n > 0) return n;
    }
  }
  return null;
}

function extractFromHtml(html) {
  // Look for price-like numbers near currency markers (₩, 원, KRW, $, etc.)
  // Targets patterns like: ₩12,900 / 12,900원 / 12900원 / KRW 12,900
  const patterns = [
    /₩\s*([\d,]+)/,
    /([\d,]+)\s*원/,
    /KRW\s*([\d,]+)/i,
    /\$\s*([\d,.]+)/,
  ];

  for (const pattern of patterns) {
    const m = html.match(pattern);
    if (m) {
      const n = parseKoreanPrice(m[1]);
      if (n > 0) return n;
    }
  }
  return null;
}

// Parse a price string that may contain commas, whitespace, or decimal points.
function parseKoreanPrice(s) {
  if (!s) return 0;
  const cleaned = s.replace(/[,\s]/g, "");
  const n = Number(cleaned);
  return isFinite(n) ? n : 0;
}

// ---------------------------------------------------------------------------
// Storage helpers
// ---------------------------------------------------------------------------

async function loadStored(key) {
  try {
    const raw = await Storage.get(key);
    if (!raw) return null;
    const parsed = JSON.parse(raw);
    // Handle wrapper format { value: ... }
    return parsed.value !== undefined ? JSON.parse(parsed.value) : parsed;
  } catch (_) {
    return null;
  }
}

// ---------------------------------------------------------------------------
// Main
// ---------------------------------------------------------------------------

// 1. Fetch the product page
let html;
try {
  html = await Http.get(productUrl);
} catch (e) {
  return `Error: failed to fetch ${productUrl} — ${e}`;
}

if (!html || typeof html !== "string") {
  return `Error: empty or non-string response from ${productUrl}`;
}

// 2. Extract the current price (JSON-LD > meta tags > HTML patterns)
let currentPrice = extractFromJsonLd(html);
if (currentPrice === null) currentPrice = extractFromMetaTags(html);
if (currentPrice === null) currentPrice = extractFromHtml(html);

if (currentPrice === null || currentPrice <= 0) {
  return `Could not extract a price from ${productUrl}. The page may not contain recognisable price data.`;
}

// 3. Load previous price
const lastPrice = await loadStored("last_price");

// 4. Update price history (keep last 90 entries ~ 90 checks ~ ~11 days at 3h interval)
const MAX_HISTORY = 90;
const now = new Date().toISOString();
let history = (await loadStored("price_history")) || [];
history.push({ date: now, price: currentPrice });
if (history.length > MAX_HISTORY) {
  history = history.slice(history.length - MAX_HISTORY);
}
await Storage.set("price_history", JSON.stringify(history));

// 5. Persist current price as last_price
await Storage.set("last_price", JSON.stringify(currentPrice));

// 6. Determine whether to send an alert
const fmt = (n) => Number(n).toLocaleString("ko-KR");
let alertSent = false;

if (targetPrice > 0 && currentPrice <= targetPrice) {
  // --- Target-price mode: price dropped to or below goal ---
  const message = [
    `🏷 *가격 알림*`,
    ``,
    `상품: ${productUrl}`,
    `현재 가격: *${fmt(currentPrice)}원*`,
    `목표 가격: ${fmt(targetPrice)}원`,
    ``,
    lastPrice !== null
      ? `이전 가격: ${fmt(lastPrice)}원 (${currentPrice < lastPrice ? "↓" : currentPrice > lastPrice ? "↑" : "→"} ${fmt(Math.abs(currentPrice - lastPrice))}원)`
      : `(첫 번째 확인)`,
    ``,
    `_목표 가격 이하로 떨어졌습니다!_`,
  ].join("\n");

  await Telegram.sendMessage(telegramToken, chatId, message, { parse_mode: "Markdown" });
  alertSent = true;
} else if (targetPrice === 0 && lastPrice !== null && currentPrice !== lastPrice) {
  // --- Change-tracking mode: any price movement ---
  const delta = currentPrice - lastPrice;
  const arrow = delta < 0 ? "↓" : "↑";
  const pct = ((Math.abs(delta) / lastPrice) * 100).toFixed(1);

  const message = [
    `🔔 *가격 변동 감지*`,
    ``,
    `상품: ${productUrl}`,
    `이전 가격: ${fmt(lastPrice)}원`,
    `현재 가격: *${fmt(currentPrice)}원*`,
    `변동: ${arrow} ${fmt(Math.abs(delta))}원 (${pct}%)`,
  ].join("\n");

  await Telegram.sendMessage(telegramToken, chatId, message, { parse_mode: "Markdown" });
  alertSent = true;
}

// 7. Return summary for logging
return [
  `URL: ${productUrl}`,
  `Price: ${fmt(currentPrice)}원`,
  lastPrice !== null ? `Previous: ${fmt(lastPrice)}원` : "Previous: (first check)",
  `Target: ${targetPrice > 0 ? fmt(targetPrice) + "원" : "none (track changes)"}`,
  `Alert sent: ${alertSent}`,
  `Checked at: ${now}`,
].join(" | ");
