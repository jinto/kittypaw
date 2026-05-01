#!/usr/bin/env bash
# kittyapi prod smoke test — Plan 8 v2 D4(A) bash + curl + jq.
# Verifies HTTP 200 + JSON envelope `response.header.resultCode == "00"` for
# every external-API-backed endpoint, plus /health + /discovery + /v1/geo/resolve.
#
# Usage:
#   bash deploy/smoke.sh                              # default https://api.kittypaw.app
#   BASE_URL=http://localhost:9712 bash deploy/smoke.sh
#   make smoke
#
# Exit: 0 on all-pass, 1 on any failure (rate-limit warnings don't fail).
# Throttled by `sleep 0.5` between calls to stay under anon 5rpm/IP gate.

set -uo pipefail

BASE="${BASE_URL:-https://api.kittypaw.app}"
THROTTLE="${SMOKE_THROTTLE:-0.5}"

PASS=0
FAIL=0
WARN=0
FAIL_LIST=()

if [[ -t 1 ]]; then
    G='\033[32m'; R='\033[31m'; Y='\033[33m'; N='\033[0m'
else
    G=''; R=''; Y=''; N=''
fi

# Split body and trailing HTTP code from a single curl response.
_split_body_code() {
    local raw="$1"
    BODY=$(printf '%s' "$raw" | sed '$d')
    CODE=$(printf '%s' "$raw" | tail -n1)
}

# do_curl PATH → sets BODY + CODE. Auto-recovers from 429 by waiting one
# full anon rate-limit window (60s + 1s margin) and retrying once.
do_curl() {
    local url="$1"
    local raw
    raw=$(curl -sS -w $'\n%{http_code}' "$url" 2>/dev/null || printf '\n000')
    _split_body_code "$raw"
    if [[ "$CODE" == "429" ]]; then
        printf "${Y}⚠${N} 429 rate-limit hit — waiting 61s for window reset...\n" >&2
        sleep 61
        raw=$(curl -sS -w $'\n%{http_code}' "$url" 2>/dev/null || printf '\n000')
        _split_body_code "$raw"
    fi
}

check_status() {
    local path="$1"
    local expected="$2"
    local desc="${3:-$path}"
    do_curl "${BASE}${path}"
    if [[ "$CODE" == "$expected" ]]; then
        printf "${G}✓${N} %s [%s]\n" "$desc" "$CODE"
        PASS=$((PASS + 1))
    else
        printf "${R}✗${N} %s [expected %s, got %s]\n" "$desc" "$expected" "$CODE"
        FAIL=$((FAIL + 1))
        FAIL_LIST+=("$desc")
    fi
    sleep "$THROTTLE"
}

check_envelope() {
    local path="$1"
    local desc="${2:-$path}"
    do_curl "${BASE}${path}"

    if [[ "$CODE" != "200" ]]; then
        printf "${R}✗${N} %s [HTTP %s]\n" "$desc" "$CODE"
        FAIL=$((FAIL + 1))
        FAIL_LIST+=("$desc")
        sleep "$THROTTLE"
        return
    fi

    local rc
    rc=$(printf '%s' "$BODY" | jq -r '.response.header.resultCode // "MISSING"' 2>/dev/null || echo "PARSE_ERR")
    : "${rc:=PARSE_ERR}"

    case "$rc" in
        "00")
            printf "${G}✓${N} %s [200 + resultCode=00]\n" "$desc"
            PASS=$((PASS + 1))
            ;;
        "22" | "99" | "LIMITED_NUMBER_OF_SERVICE_REQUESTS_EXCEEDS_ERROR")
            printf "${Y}⚠${N} %s [rate-limited resultCode=%s, skipping]\n" "$desc" "$rc"
            WARN=$((WARN + 1))
            ;;
        "MISSING" | "PARSE_ERR")
            printf "${R}✗${N} %s [200 but malformed envelope: %s]\n" "$desc" "$rc"
            FAIL=$((FAIL + 1))
            FAIL_LIST+=("$desc")
            ;;
        *)
            printf "${R}✗${N} %s [resultCode=%s]\n" "$desc" "$rc"
            FAIL=$((FAIL + 1))
            FAIL_LIST+=("$desc")
            ;;
    esac
    sleep "$THROTTLE"
}

# /v1/geo/resolve has its own JSON shape (no `response.header` envelope).
# curl -G with --data-urlencode handles Hangul query safely.
check_geo() {
    local query="$1"
    local desc="$2"
    local raw
    raw=$(curl -sS -w $'\n%{http_code}' "${BASE}/v1/geo/resolve" --data-urlencode "q=${query}" -G 2>/dev/null || printf '\n000')
    _split_body_code "$raw"
    if [[ "$CODE" == "429" ]]; then
        printf "${Y}⚠${N} 429 rate-limit hit — waiting 61s for window reset...\n" >&2
        sleep 61
        raw=$(curl -sS -w $'\n%{http_code}' "${BASE}/v1/geo/resolve" --data-urlencode "q=${query}" -G 2>/dev/null || printf '\n000')
        _split_body_code "$raw"
    fi

    if [[ "$CODE" != "200" ]]; then
        printf "${R}✗${N} %s [HTTP %s]\n" "$desc" "$CODE"
        FAIL=$((FAIL + 1))
        FAIL_LIST+=("$desc")
        sleep "$THROTTLE"
        return
    fi
    if printf '%s' "$BODY" | jq -e '.lat and .lon and .name_matched' >/dev/null 2>&1; then
        printf "${G}✓${N} %s [200 + lat/lon/name_matched]\n" "$desc"
        PASS=$((PASS + 1))
    else
        printf "${R}✗${N} %s [200 but missing fields: %s]\n" "$desc" "$BODY"
        FAIL=$((FAIL + 1))
        FAIL_LIST+=("$desc")
    fi
    sleep "$THROTTLE"
}

if ! command -v jq >/dev/null 2>&1; then
    printf "${R}✗${N} jq not found — install with 'brew install jq' or 'apt install jq'\n"
    exit 1
fi

echo "=== kittyapi smoke: ${BASE} ==="

echo
echo "--- Infrastructure ---"
check_status "/health" "200"
check_status "/discovery" "200"

echo
echo "--- Calendar (KASI SpcdeInfoService) ---"
check_envelope "/v1/calendar/holidays?solYear=2025" "calendar/holidays"
check_envelope "/v1/calendar/anniversaries?solYear=2025" "calendar/anniversaries"
check_envelope "/v1/calendar/solar-terms?solYear=2025" "calendar/solar-terms"

echo
echo "--- Almanac (KASI LrsrCld + RiseSet) ---"
check_envelope "/v1/almanac/lunar-date?solYear=2026&solMonth=05&solDay=01" "almanac/lunar-date"
check_envelope "/v1/almanac/solar-date?lunYear=2026&lunMonth=03&lunDay=15" "almanac/solar-date"
check_envelope "/v1/almanac/sun?locdate=20260501&latitude=37.5665&longitude=126.9780" "almanac/sun"

echo
echo "--- Weather (KMA) ---"
check_envelope "/v1/weather/kma/village-fcst?lat=37.5665&lon=126.978" "weather/village-fcst"
check_envelope "/v1/weather/kma/ultra-srt-ncst?lat=37.5665&lon=126.978" "weather/ultra-srt-ncst"
check_envelope "/v1/weather/kma/ultra-srt-fcst?lat=37.5665&lon=126.978" "weather/ultra-srt-fcst"

echo
echo "--- Air (한국환경공단) ---"
check_envelope "/v1/air/airkorea/realtime/city?sidoName=%EC%84%9C%EC%9A%B8" "air/airkorea/realtime/city (서울)"
check_envelope "/v1/air/airkorea/realtime/station?stationName=%EC%A2%85%EB%A1%9C%EA%B5%AC&dataTerm=DAILY" "air/airkorea/realtime/station (종로구)"
check_envelope "/v1/air/airkorea/forecast?informCode=PM10" "air/airkorea/forecast (PM10)"
check_envelope "/v1/air/airkorea/forecast/weekly" "air/airkorea/forecast/weekly"
check_envelope "/v1/air/airkorea/unhealthy" "air/airkorea/unhealthy"

echo
echo "--- Geo (places DB + addresses fallthrough) ---"
check_geo "강남역" "geo/resolve (강남역)"

TOTAL=$((PASS + FAIL))
echo
echo "=== Summary ==="
printf "Passed: ${G}%d${N}/%d\n" "$PASS" "$TOTAL"
if (( WARN > 0 )); then
    printf "Warned: ${Y}%d${N} (rate-limited, not failed)\n" "$WARN"
fi
if (( FAIL > 0 )); then
    printf "Failed: ${R}%d${N}\n" "$FAIL"
    for f in "${FAIL_LIST[@]}"; do
        printf "  ${R}✗${N} %s\n" "$f"
    done
    exit 1
fi
echo "All smoke checks passed."
