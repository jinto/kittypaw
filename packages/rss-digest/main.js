// rss-digest/main.js
// Fetches an RSS feed, filters already-seen items via Storage,
// summarizes new items with LLM, and sends a digest to Telegram.

const ctx = JSON.parse(__context__);
const config = ctx.config || {};

const telegramToken = config.telegram_token;
const chatId = config.chat_id;
const feedUrl = config.feed_url || "https://news.ycombinator.com/rss";
const maxItems = parseInt(config.max_items || "5", 10);

// Storage key that holds a JSON array of already-seen item links
const SEEN_KEY = "rss_seen_links";

// --- Simple XML field extractor (no DOM, works in QuickJS) ---
// Returns the first text content between <tag>...</tag>
function extractTag(xml, tag) {
  const open = `<${tag}>`;
  const close = `</${tag}>`;
  const start = xml.indexOf(open);
  if (start === -1) return "";
  const end = xml.indexOf(close, start);
  if (end === -1) return "";
  return xml.slice(start + open.length, end).replace(/<!\[CDATA\[|\]\]>/g, "").trim();
}

// Returns all occurrences of content between <item>...</item>
function extractItems(xml) {
  const items = [];
  let remaining = xml;
  while (true) {
    const start = remaining.indexOf("<item>");
    if (start === -1) break;
    const end = remaining.indexOf("</item>", start);
    if (end === -1) break;
    items.push(remaining.slice(start + 6, end));
    remaining = remaining.slice(end + 7);
  }
  return items;
}

// --- Load seen links from Storage ---
let seenLinks = [];
try {
  const raw = await Storage.get(SEEN_KEY);
  if (raw) {
    const parsed = JSON.parse(raw);
    // Storage.get returns {"value":"..."}, value itself is the JSON array string
    const value = parsed.value || parsed;
    seenLinks = typeof value === "string" ? JSON.parse(value) : value;
  }
} catch (e) {
  seenLinks = [];
}

// --- Fetch the RSS feed ---
let feedXml = "";
try {
  feedXml = await Http.get(feedUrl);
} catch (e) {
  return `Error fetching RSS feed: ${e}`;
}

// --- Parse items ---
const rawItems = extractItems(feedXml);
const parsedItems = rawItems.map(itemXml => ({
  title: extractTag(itemXml, "title"),
  link: extractTag(itemXml, "link") || extractTag(itemXml, "guid"),
  description: extractTag(itemXml, "description").slice(0, 300),
})).filter(item => item.title && item.link);

// --- Filter new (unseen) items ---
const newItems = parsedItems.filter(item => !seenLinks.includes(item.link));

if (newItems.length === 0) {
  return "RSS digest: no new items since last run.";
}

// Process up to maxItems
const toProcess = newItems.slice(0, maxItems);

// --- Summarize with LLM ---
const itemList = toProcess
  .map((item, i) => `${i + 1}. ${item.title}\n   ${item.description}`)
  .join("\n\n");

const prompt =
  `Here are ${toProcess.length} new items from an RSS feed (${feedUrl}):\n\n` +
  `${itemList}\n\n` +
  `Write a brief 1-sentence summary for each item. ` +
  `Format your response as a numbered list matching the order above.`;

let summary = "";
try {
  const llmRaw = await Llm.generate(prompt);
  const llmData = JSON.parse(llmRaw);
  summary = llmData.text || "";
} catch (e) {
  // Fallback: just use titles
  summary = toProcess.map((item, i) => `${i + 1}. ${item.title}`).join("\n");
}

// --- Build Telegram message ---
const today = new Date().toISOString().slice(0, 10);
const lines = [
  `📰 *RSS Digest — ${today}*`,
  `_Source: ${feedUrl}_`,
  `_${toProcess.length} new item(s)_`,
  ``,
  summary,
  ``,
  `*Links*`,
  ...toProcess.map((item, i) => `${i + 1}. [${item.title.slice(0, 60)}](${item.link})`),
  ``,
  `_Powered by KittyPaw_`,
];
const message = lines.join("\n");

await Telegram.sendMessage(telegramToken, chatId, message, { parse_mode: "Markdown" });

// --- Persist seen links (keep last 200 to avoid unbounded growth) ---
const updatedSeen = [...seenLinks, ...toProcess.map(i => i.link)].slice(-200);
await Storage.set(SEEN_KEY, JSON.stringify(updatedSeen));

return `RSS digest sent: ${toProcess.length} new item(s) from ${feedUrl}.`;
