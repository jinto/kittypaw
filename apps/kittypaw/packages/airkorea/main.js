// AirKorea 대기질 — kittypaw-api 연동 패키지
(function () {
  var ctx = JSON.parse(__context__);
  var apiURL = (ctx.config && ctx.config.api_url) || "http://localhost:8080";
  var token = (ctx.config && ctx.config.access_token) || "";
  var station = (ctx.config && ctx.config.station_name) || "종로구";
  var locale = (ctx.user && ctx.user.locale) || "ko";

  if (!token) {
    Agent.respond(
      "로그인이 필요합니다. 터미널에서 다음 명령어를 실행하세요:\n\n" +
      "  kittypaw login --api-url " + apiURL + "\n\n" +
      "로그인 후 다시 시도해주세요."
    );
    return;
  }

  var url = apiURL + "/api/v1/airkorea?station=" + encodeURIComponent(station);
  var resp = Http.get(url, { headers: { "Authorization": "Bearer " + token } });

  if (resp.error) {
    if (resp.error.indexOf("401") !== -1 || resp.error.indexOf("403") !== -1) {
      Agent.respond(
        "인증이 만료되었습니다. 다시 로그인하세요:\n\n" +
        "  kittypaw login --api-url " + apiURL
      );
      return;
    }
    Agent.respond("대기질 조회 실패: " + resp.error);
    return;
  }

  var data;
  try {
    data = JSON.parse(resp.body || resp);
  } catch (e) {
    Agent.respond("응답 파싱 실패: " + e.message);
    return;
  }

  if (data.error) {
    Agent.respond("API 오류: " + data.error);
    return;
  }

  // Format air quality data.
  var items = data.items || data.data || [data];
  if (!items || items.length === 0) {
    Agent.respond(station + " 측정소의 데이터가 없습니다.");
    return;
  }

  var item = items[0];
  var grade = item.khaiGrade || item.khai_grade || "";
  var gradeLabel = { "1": "좋음", "2": "보통", "3": "나쁨", "4": "매우나쁨" }[grade] || grade || "-";

  var lines = [];
  lines.push("📍 " + (item.stationName || station) + " 대기질");
  lines.push("");
  lines.push("종합지수(CAI): " + (item.khaiValue || item.khai_value || "-") + " (" + gradeLabel + ")");
  lines.push("PM10: " + (item.pm10Value || item.pm10 || "-") + " ㎍/㎥");
  lines.push("PM2.5: " + (item.pm25Value || item.pm25 || "-") + " ㎍/㎥");

  if (item.o3Value || item.o3) {
    lines.push("O₃: " + (item.o3Value || item.o3) + " ppm");
  }
  if (item.coValue || item.co) {
    lines.push("CO: " + (item.coValue || item.co) + " ppm");
  }
  if (item.no2Value || item.no2) {
    lines.push("NO₂: " + (item.no2Value || item.no2) + " ppm");
  }
  if (item.so2Value || item.so2) {
    lines.push("SO₂: " + (item.so2Value || item.so2) + " ppm");
  }

  if (item.dataTime || item.measured_at) {
    lines.push("");
    lines.push("측정시간: " + (item.dataTime || item.measured_at));
  }

  Agent.respond(lines.join("\n"));
})();
