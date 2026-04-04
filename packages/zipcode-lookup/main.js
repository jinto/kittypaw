// zipcode-lookup/main.js
// Keyword-triggered Korean address/zipcode lookup via the Juso API.
// Usage: "우편번호 강남구 역삼동" → searches and sends results to Telegram.

const ctx = JSON.parse(__context__);
const config = ctx.config || {};

const telegramToken = config.telegram_token;
const chatId = config.chat_id;
const apiKey = config.juso_api_key;

// Extract the user's message from the keyword trigger
const raw = ((ctx.message && ctx.message.text) || ctx.input || "").trim();

// Strip the trigger keyword to isolate the address query
const query = raw
  .replace(/^(우편번호|zipcode|주소검색)\s*/i, "")
  .trim();

if (!query) {
  return "검색할 주소를 입력해주세요. 예: 우편번호 강남구 역삼동";
}

// --- Call the Juso (도로명주소) API ---
const url =
  "https://business.juso.go.kr/addrlink/addrLinkApi.do" +
  `?confmKey=${encodeURIComponent(apiKey)}` +
  "&currentPage=1" +
  "&countPerPage=5" +
  `&keyword=${encodeURIComponent(query)}` +
  "&resultType=json";

let data;
try {
  const body = await Http.get(url);
  data = JSON.parse(body);
} catch (e) {
  return `주소 API 호출 실패: ${e}`;
}

const result = data && data.results;
const common = result && result.common;

if (!common || common.errorCode !== "0") {
  const errorMsg = common ? common.errorMessage : "알 수 없는 오류";
  return `주소 검색 오류: ${errorMsg}`;
}

const jusoList = result.juso;
if (!jusoList || jusoList.length === 0) {
  const noResultMsg = `"${query}"에 대한 검색 결과가 없습니다.`;
  await Telegram.sendMessage(telegramToken, chatId, noResultMsg);
  return noResultMsg;
}

// --- Format results ---
const totalCount = common.totalCount || jusoList.length;
const lines = [`*우편번호 검색 결과* — "${query}"`, ""];

jusoList.forEach((juso, i) => {
  const zipNo = juso.zipNo || "-";
  const roadAddr = juso.roadAddr || "";
  const jibunAddr = juso.jibunAddr || "";

  lines.push(`${i + 1}. [${zipNo}]`);
  lines.push(`   ${roadAddr}`);
  if (jibunAddr && jibunAddr !== roadAddr) {
    lines.push(`   (지번) ${jibunAddr}`);
  }
  lines.push("");
});

if (parseInt(totalCount, 10) > 5) {
  lines.push(`_총 ${totalCount}건 중 상위 5건 표시_`);
}
lines.push("_출처: 도로명주소 API · Powered by KittyPaw_");

const message = lines.join("\n");
await Telegram.sendMessage(telegramToken, chatId, message, { parse_mode: "Markdown" });

return `우편번호 검색 완료: "${query}" — ${jusoList.length}건 전송`;
