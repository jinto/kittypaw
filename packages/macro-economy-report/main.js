// macro-economy-report/main.js
// Fetches daily price data for major ETFs via Yahoo Finance v8 API,
// summarizes the market with LLM, and sends the report to Telegram.

const ctx = JSON.parse(__context__);
const config = ctx.config || {};

const tickers = (config.tickers || "SPY,QQQ,TLT,GLD").split(",").map(t => t.trim());
const telegramToken = config.telegram_token;
const chatId = config.chat_id;

// --- Fetch price data for a single ticker ---
async function fetchQuote(ticker) {
  const url = `https://query1.finance.yahoo.com/v8/finance/chart/${ticker}?interval=1d&range=2d`;
  try {
    const raw = await Http.get(url);
    const data = JSON.parse(raw);
    const meta = data.chart.result[0].meta;
    const close = meta.regularMarketPrice;
    const prevClose = meta.chartPreviousClose;
    const change = close - prevClose;
    const changePct = ((change / prevClose) * 100).toFixed(2);
    const sign = change >= 0 ? "+" : "";
    return {
      ticker,
      price: close.toFixed(2),
      change: `${sign}${change.toFixed(2)}`,
      changePct: `${sign}${changePct}%`,
      ok: true,
    };
  } catch (e) {
    return { ticker, ok: false, error: String(e) };
  }
}

// --- Build a plain-text market snapshot ---
function buildSnapshot(quotes) {
  return quotes
    .map(q => {
      if (!q.ok) return `${q.ticker}: N/A`;
      return `${q.ticker}: $${q.price} (${q.changePct})`;
    })
    .join("\n");
}

// --- Main ---
const today = new Date().toISOString().slice(0, 10);

// Fetch all tickers concurrently
const quotePromises = tickers.map(fetchQuote);
const quotes = await Promise.all(quotePromises);

const snapshot = buildSnapshot(quotes);

// Ask LLM to write a brief market commentary
const prompt =
  `Today is ${today}. Here is today's market snapshot:\n\n${snapshot}\n\n` +
  `Write a concise 3-5 sentence macro-economic briefing for a retail investor. ` +
  `Mention the direction of equities (SPY/QQQ), bonds (TLT), and gold (GLD) if present. ` +
  `Keep the tone neutral and factual.`;

let commentary = "";
try {
  const llmRaw = await Llm.generate(prompt);
  const llmData = JSON.parse(llmRaw);
  commentary = llmData.text || "";
} catch (e) {
  commentary = "(LLM summary unavailable)";
}

// Format the final Telegram message
const lines = [
  `📊 *Macro Economy Report — ${today}*`,
  ``,
  `*Market Snapshot*`,
  "```",
  snapshot,
  "```",
  ``,
  `*Commentary*`,
  commentary,
  ``,
  `_Data: Yahoo Finance · Powered by KittyPaw_`,
];
const message = lines.join("\n");

// Send to Telegram
await Telegram.sendMessage(telegramToken, chatId, message, { parse_mode: "Markdown" });

// Cache last run date in Storage so other skills can check freshness
await Storage.set("macro_economy_last_run", JSON.stringify({ date: today, tickers }));

return `Macro economy report sent for ${today}. Tickers: ${tickers.join(", ")}`;
