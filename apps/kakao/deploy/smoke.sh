#!/usr/bin/env bash
set -euo pipefail

BASE="${BASE_URL:-https://kakao.kittypaw.app}"

body="$(curl -fsS "$BASE/health")"

python3 - "$body" <<'PY'
import json
import sys

body = sys.argv[1]
data = json.loads(body)
if data.get("status") != "healthy":
    raise SystemExit(f"unexpected health body: {body}")
print(f"✓ /health healthy ({data.get('version', 'unknown')} {data.get('commit', 'unknown')})")
PY
