// transit-arrival/main.js
// Fetches real-time Seoul subway arrival info from 서울 열린데이터광장,
// formats it as a readable summary, and sends it to Telegram.

const ctx = JSON.parse(__context__);
const config = ctx.config || {};

const telegramToken = config.telegram_token;
const chatId = config.chat_id;
const apiKey = config.api_key;
const stationName = config.station_name || "강남";

// --- Fetch real-time arrival data ---
const url =
  "http://swopenapi.seoul.go.kr/api/subway/" +
  encodeURIComponent(apiKey) +
  "/json/realtimeStationArrival/0/10/" +
  encodeURIComponent(stationName);

let arrivals;
try {
  const raw = await Http.get(url);
  arrivals = JSON.parse(raw);
} catch (e) {
  return "Error fetching arrival data: " + e;
}

// Handle API errors
if (arrivals.errorMessage) {
  const code = arrivals.errorMessage.code;
  if (code === "INFO-200") {
    return "No arrival data for station: " + stationName;
  }
  return "API error: " + (arrivals.errorMessage.message || code);
}

const items = arrivals.realtimeArrivalList;
if (!items || items.length === 0) {
  return "No arrival data available for " + stationName;
}

// --- Group arrivals by line ---
const byLine = {};
for (let i = 0; i < items.length; i++) {
  const item = items[i];
  const line = item.subwayId;
  if (!byLine[line]) {
    byLine[line] = [];
  }
  byLine[line].push(item);
}

// Line name mapping (subwayId -> display name)
const lineNames = {
  "1001": "1호선", "1002": "2호선", "1003": "3호선",
  "1004": "4호선", "1005": "5호선", "1006": "6호선",
  "1007": "7호선", "1008": "8호선", "1009": "9호선",
  "1063": "경의중앙", "1065": "공항철도", "1067": "경춘선",
  "1075": "수인분당", "1077": "신분당", "1092": "우이신설",
  "1032": "GTX-A",
};

// Format arrival time text
function formatArrival(item) {
  // arvlMsg2: human-readable message like "3분 후 도착", "전역 출발" etc.
  const msg = item.arvlMsg2 || "";
  const dest = item.trainLineNm || "";  // e.g. "성수행 - 강남방면"
  // Extract direction from trainLineNm
  const dir = dest.split(" - ")[0] || dest;
  return dir + " : " + msg;
}

// --- Build message ---
const lines = [];
lines.push("*" + stationName + "역 실시간 도착정보*");
lines.push("");

const lineIds = Object.keys(byLine);
for (let i = 0; i < lineIds.length; i++) {
  const lineId = lineIds[i];
  const lineName = lineNames[lineId] || lineId;
  const trains = byLine[lineId];

  lines.push("*" + lineName + "*");

  // Show up/down direction trains (up to 2 each direction)
  const upDir = [];
  const downDir = [];
  for (let j = 0; j < trains.length; j++) {
    const t = trains[j];
    // updnLine: "상행"/"하행" or "외선"/"내선" (for line 2)
    if (t.updnLine === "상행" || t.updnLine === "외선") {
      upDir.push(t);
    } else {
      downDir.push(t);
    }
  }

  if (upDir.length > 0) {
    for (let k = 0; k < Math.min(upDir.length, 2); k++) {
      lines.push("  " + formatArrival(upDir[k]));
    }
  }
  if (downDir.length > 0) {
    for (let k = 0; k < Math.min(downDir.length, 2); k++) {
      lines.push("  " + formatArrival(downDir[k]));
    }
  }
  lines.push("");
}

lines.push("_data.seoul.go.kr · Powered by KittyPaw_");

const message = lines.join("\n");

await Telegram.sendMessage(telegramToken, chatId, message, { parse_mode: "Markdown" });

return "Arrival info sent for " + stationName + " (" + items.length + " trains).";
