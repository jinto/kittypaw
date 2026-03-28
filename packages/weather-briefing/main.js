// weather-briefing/main.js
// Fetches a 7-day weather forecast from Open-Meteo (free, no API key),
// summarizes it with LLM, and sends the briefing to Telegram.

const ctx = JSON.parse(__context__);
const config = ctx.config || {};

const telegramToken = config.telegram_token;
const chatId = config.chat_id;
const city = config.city || "Seoul";
const latitude = config.latitude || "37.57";
const longitude = config.longitude || "126.98";

// --- Fetch forecast from Open-Meteo ---
const forecastUrl =
  `https://api.open-meteo.com/v1/forecast` +
  `?latitude=${latitude}&longitude=${longitude}` +
  `&daily=temperature_2m_max,temperature_2m_min,precipitation_sum,weathercode` +
  `&timezone=auto`;

let forecast;
try {
  const raw = await Http.get(forecastUrl);
  forecast = JSON.parse(raw);
} catch (e) {
  return `Error fetching weather data: ${e}`;
}

const daily = forecast.daily;
const dates = daily.time;
const maxTemps = daily.temperature_2m_max;
const minTemps = daily.temperature_2m_min;
const precip = daily.precipitation_sum;

// Build a compact text table for the next 7 days
const rows = dates.map((date, i) => {
  const rain = precip[i] != null ? `${precip[i].toFixed(1)}mm` : "—";
  return `${date}  ${maxTemps[i]}°C / ${minTemps[i]}°C  rain:${rain}`;
});
const forecastTable = rows.join("\n");

// Ask LLM to summarize in natural language
const today = dates[0];
const prompt =
  `Today is ${today}. The city is ${city}.\n\n` +
  `7-day weather forecast:\n${forecastTable}\n\n` +
  `Write a friendly 2-4 sentence weather briefing. ` +
  `Highlight today's conditions, any notable rain or temperature changes, ` +
  `and a practical tip (e.g. bring an umbrella, wear layers).`;

let summary = "";
try {
  const llmRaw = await Llm.generate(prompt);
  const llmData = JSON.parse(llmRaw);
  summary = llmData.text || "";
} catch (e) {
  summary = "(LLM summary unavailable)";
}

// Format Telegram message
const lines = [
  `🌤 *Weather Briefing — ${city} (${today})*`,
  ``,
  `*7-Day Forecast*`,
  "```",
  forecastTable,
  "```",
  ``,
  `*Summary*`,
  summary,
  ``,
  `_Data: Open-Meteo · Powered by KittyPaw_`,
];
const message = lines.join("\n");

await Telegram.sendMessage(telegramToken, chatId, message, { parse_mode: "Markdown" });

return `Weather briefing sent for ${city} on ${today}.`;
