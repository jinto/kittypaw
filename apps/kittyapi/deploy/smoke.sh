#!/usr/bin/env bash
set -euo pipefail

BASE="${KITTYAPI_BASE_URL:-${BASE_URL:-https://api.kittypaw.app}}"

pass() { printf "ok - %s\n" "$1"; }
fail() {
    printf "not ok - %s\n" "$1" >&2
    exit 1
}

status() {
    local path="$1"
    curl -sS -o /dev/null -w '%{http_code}' "${BASE}${path}"
}

expect_status() {
    local path="$1"
    local want="$2"
    local name="$3"
    local got
    got="$(status "$path" || printf '000')"
    if [[ "$got" == "$want" ]]; then
        pass "$name"
    else
        fail "$name: got HTTP $got, want $want"
    fi
}

expect_status "/health" "200" "health"

health_body="$(curl -fsS "${BASE}/health")"
python3 - "$health_body" <<'PY'
import json
import sys

body = sys.argv[1]
data = json.loads(body)
if data.get("status") != "healthy":
    raise SystemExit(f"unexpected health body: {body}")
version = data.get("version") or "unknown"
commit = data.get("commit") or "unknown"
print(f"ok - health version {version} ({commit})")
PY

expect_status "/discovery" "404" "discovery closed on api host"
expect_status "/.well-known/jwks.json" "404" "jwks closed on api host"
expect_status "/auth/google" "404" "auth closed on api host"

# The first data call should reach the handler. Without upstream API keys it
# may return 400/502; 404 would mean route wiring regressed.
data_code="$(status "/v1/almanac/lunar-date?solYear=2026&solMonth=05&solDay=01" || printf '000')"
if [[ "$data_code" == "404" || "$data_code" == "000" ]]; then
    fail "almanac route reachable: got HTTP $data_code"
fi
pass "almanac route reachable"
