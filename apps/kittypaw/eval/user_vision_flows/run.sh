#!/usr/bin/env bash
#
# User-vision multi-turn regression smoke.
#
# Runs canonical user-visible chat flows through `kittypaw chat`, then judges
# provider-independent behavior outcomes. The assertions intentionally avoid
# exact prose, emoji, source-name, or one-provider phrasing checks.
#
# Exit state contract:
#   PASS    -> 0
#   FAIL    -> 1
#   INFRA   -> 2
#   NOT_RUN -> 3
#
# Usage:
#   ./eval/user_vision_flows/run.sh
#   FLOW=clarify ./eval/user_vision_flows/run.sh
#   KITTYPAW_EVAL_PROVIDER=openai ./eval/user_vision_flows/run.sh

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
KITTY_BIN="${KITTY_BIN:-$ROOT_DIR/bin/kittypaw}"
CLEAN_CHAT="$ROOT_DIR/eval/secretary_smoke/clean_chat.py"
BASELINE_FILE="$ROOT_DIR/eval/user_vision_flows/provider_baselines.json"
OUT_DIR="$ROOT_DIR/eval/user_vision_flows/results"
SUMMARY="$OUT_DIR/summary.md"
RESULTS_JSONL="$OUT_DIR/results.jsonl"
JUDGE_MODEL="${JUDGE_MODEL:-claude-haiku-4-5-20251001}"
RUN_ACCOUNT="${KITTYPAW_ACCOUNT:-auto}"
RUN_PROVIDER="${KITTYPAW_EVAL_PROVIDER:-configured}"
RUN_MODEL="${KITTYPAW_EVAL_MODEL:-configured}"
RUN_SERVER="${KITTYPAW_EVAL_SERVER:-${KITTYPAW_EVAL_DAEMON:-local}}"
FINISHED=0
LAST_FLOW_DETAIL=""

declare -A FLOWS
declare -A FLOW_BEHAVIORS
declare -A FLOW_THRESHOLDS

mkdir -p "$OUT_DIR"

provider_family() {
  local provider
  provider=$(printf '%s' "$1" | tr '[:upper:]' '[:lower:]')
  case "$provider" in
    *anthropic*|*claude*) echo "anthropic" ;;
    *openai*|*gpt*) echo "openai" ;;
    *gemini*|*google*) echo "gemini" ;;
    *) echo "default" ;;
  esac
}

PROVIDER_FAMILY="$(provider_family "$RUN_PROVIDER")"

write_summary_header() {
  {
    echo "# User Vision Flow Results"
    echo
    echo "Date: $(date -u +'%Y-%m-%d %H:%M UTC')"
    echo "State: RUNNING"
    echo "Provider: $RUN_PROVIDER"
    echo "Provider family: $PROVIDER_FAMILY"
    echo "Model: $RUN_MODEL"
    echo "Judge model: $JUDGE_MODEL"
    echo "Account: $RUN_ACCOUNT"
    echo "Server: $RUN_SERVER"
    echo
    echo "| Flow | State | Detail |"
    echo "|---|---|---|"
  } > "$SUMMARY"
  : > "$RESULTS_JSONL"
}

finish() {
  local state="$1"
  local code="$2"
  local detail="${3:-}"
  FINISHED=1
  {
    echo
    echo "State: $state"
    [[ -n "$detail" ]] && echo "Detail: $detail"
  } >> "$SUMMARY"
  echo "STATE: $state"
  [[ -n "$detail" ]] && echo "$detail" >&2
  exit "$code"
}

trap 'rc=$?; if (( rc != 0 && FINISHED == 0 )); then echo "STATE: INFRA"; echo "runner aborted with exit $rc" >&2; exit 2; fi' EXIT

need_cmd() {
  if ! command -v "$1" >/dev/null 2>&1; then
    finish INFRA 2 "required command not found: $1"
  fi
}

json_escape() {
  jq -Rn --arg value "$1" '$value'
}

record_flow() {
  local flow="$1" state="$2" detail="${3:-}"
  echo "| $flow | $state | ${detail//|/ } |" >> "$SUMMARY"
  printf '{"flow":%s,"state":%s,"detail":%s}\n' \
    "$(json_escape "$flow")" "$(json_escape "$state")" "$(json_escape "$detail")" >> "$RESULTS_JSONL"
}

write_summary_header

if [[ "${KITTYPAW_EVAL_SKIP:-}" == "1" ]]; then
  finish NOT_RUN 3 "KITTYPAW_EVAL_SKIP=1"
fi

need_cmd jq
need_cmd curl
need_cmd python3

if [[ ! -x "$KITTY_BIN" ]]; then
  finish INFRA 2 "kittypaw binary not found at $KITTY_BIN — run 'make build' first"
fi

if [[ ! -f "$CLEAN_CHAT" ]]; then
  finish INFRA 2 "clean_chat.py missing at $CLEAN_CHAT"
fi

if [[ ! -f "$BASELINE_FILE" ]]; then
  finish INFRA 2 "provider baseline file missing at $BASELINE_FILE"
fi

if [[ -z "${ANTHROPIC_API_KEY:-}" ]]; then
  ANTHROPIC_API_KEY=$(grep -m1 'api_key = "sk-ant' ~/.kittypaw/accounts/default/config.toml 2>/dev/null | sed 's/.*"\(sk-ant[^"]*\)".*/\1/')
fi
if [[ -z "$ANTHROPIC_API_KEY" ]]; then
  finish INFRA 2 "ANTHROPIC_API_KEY not set and not found in accounts/default/config.toml"
fi

load_provider_baselines() {
  local provider="$1"
  while IFS=$'\t' read -r flow behaviors; do
    FLOW_BEHAVIORS["$flow"]="$behaviors"
  done < <(jq -r --arg provider "$provider" '
    .default as $default
    | (.[$provider] // {}) as $specific
    | ($default.behaviors + ($specific.behaviors // {}))
    | to_entries[]
    | [.key, (.value | join(","))]
    | @tsv
  ' "$BASELINE_FILE")

  while IFS=$'\t' read -r flow threshold; do
    FLOW_THRESHOLDS["$flow"]="$threshold"
  done < <(jq -r --arg provider "$provider" '
    .default as $default
    | (.[$provider] // {}) as $specific
    | ($default.thresholds + ($specific.thresholds // {}))
    | to_entries[]
    | [.key, (.value | tostring)]
    | @tsv
  ' "$BASELINE_FILE")
}

load_provider_baselines "$PROVIDER_FAMILY"

reset_server() {
  "$KITTY_BIN" server stop >/dev/null 2>&1 || true
  pkill -9 -f "kittypaw server start" 2>/dev/null || true
  rm -rf "$HOME/.kittypaw/accounts/default/packages/"* \
         "$HOME/.kittypaw/accounts/default/skills/"* 2>/dev/null || true
  sleep 1
}

run_flow() {
  local name="$1" input="$2"
  reset_server
  local raw
  if ! raw=$(printf '%s' "$input" | "$KITTY_BIN" chat 2>&1); then
    echo "chat command failed for flow $name:" >&2
    echo "$raw" >&2
    return 2
  fi
  printf '%s' "$raw" | python3 "$CLEAN_CHAT"
}

behavior_def() {
  case "$1" in
    clarifies_ambiguous_currency)
      echo "The assistant handles the ambiguous currency query by asking or confirming whether the user means exchange rate/currency, instead of jumping to a confident numeric answer." ;;
    avoids_rate_fabrication)
      echo "The assistant does not invent a specific exchange rate for an ambiguous query. If it gives a number, it must clearly be grounded in a tool/source or framed as needing confirmation." ;;
    exchange_rate_data)
      echo "The assistant provides useful exchange-rate information or a clear tool-backed exchange-rate result, not merely an offer to install/search." ;;
    skill_install_acknowledged)
      echo "The assistant clearly confirms that the relevant exchange-rate capability was installed, enabled, or is ready to use." ;;
    helpful_chitchat_after_success)
      echo "After the user praises the assistant, the response naturally acknowledges the praise without rerunning tools, reopening setup, or asking for unrelated clarification." ;;
    no_repeated_install_offer)
      echo "After the exchange-rate capability is already installed or used, the assistant does not offer to install the same capability again." ;;
    krw_reframed_response)
      echo "For a Korean-won reframing request, the assistant presents the exchange-rate result from a KRW/Korean-won perspective or explicitly explains the KRW framing." ;;
    explicit_install_request_completes)
      echo "When the user explicitly asks to install after a relevant skill was suggested, the assistant proceeds with that skill/capability and does not ask which skill again." ;;
    browses_available_skills)
      echo "The assistant lists multiple available skills or skill categories and does not auto-install a skill during browsing." ;;
    asks_choice_for_news_multimatch)
      echo "For a news-related skill query with multiple plausible matches, the assistant presents multiple news-related options and asks or lets the user choose, without auto-installing one." ;;
    *) echo "Unknown behavior." ;;
  esac
}

judge_flow_behavior() {
  local flow="$1" input="$2" transcript="$3" behavior="$4"
  local def body api_response result
  def=$(behavior_def "$behavior")
  body=$(jq -n \
    --arg model "$JUDGE_MODEL" \
    --arg flow "$flow" \
    --arg input "$input" \
    --arg transcript "$transcript" \
    --arg behavior "$behavior" \
    --arg def "$def" \
    '{
      model: $model,
      max_tokens: 120,
      messages: [{
        role: "user",
        content: ("You evaluate a multi-turn KittyPaw assistant transcript.\n\nFlow: " + $flow + "\nUser input sequence:\n" + $input + "\n\nCleaned assistant transcript:\n" + $transcript + "\n\nTarget behavior: " + $behavior + "\nDefinition: " + $def + "\n\nAnswer with exactly one token on the first line: PASS, PARTIAL, or FAIL. Use PASS for equivalent behavior even if phrasing/source names differ across providers. Then give one short reason.")
      }]
    }')

  if ! api_response=$(curl -fsS https://api.anthropic.com/v1/messages \
    -H "x-api-key: $ANTHROPIC_API_KEY" \
    -H "anthropic-version: 2023-06-01" \
    -H "content-type: application/json" \
    -d "$body"); then
    return 2
  fi
  if echo "$api_response" | jq -e '.error' >/dev/null 2>&1; then
    return 2
  fi
  result=$(echo "$api_response" | jq -r '.content[0].text // empty' | head -n1 | tr -d '[:space:]' | tr '[:lower:]' '[:upper:]')
  case "$result" in
    PASS|PARTIAL|FAIL) echo "$result" ;;
    *) return 2 ;;
  esac
}

evaluate_flow() {
  local flow="$1" input="$2" transcript="$3"
  local behaviors="${FLOW_BEHAVIORS[$flow]:-}"
  local threshold="${FLOW_THRESHOLDS[$flow]:-1.5}"
  if [[ -z "$behaviors" ]]; then
    LAST_FLOW_DETAIL="missing baseline behaviors"
    return 2
  fi

  local points=0 total=0 detail=""
  IFS=',' read -ra bs <<< "$behaviors"
  for behavior in "${bs[@]}"; do
    total=$((total + 1))
    local verdict
    if ! verdict=$(judge_flow_behavior "$flow" "$input" "$transcript" "$behavior"); then
      LAST_FLOW_DETAIL="judge request failed for behavior=$behavior"
      return 2
    fi
    [[ "$verdict" == "PASS" ]] && points=$((points + 2))
    [[ "$verdict" == "PARTIAL" ]] && points=$((points + 1))
    detail+="$behavior=$verdict "
    echo "  $behavior -> $verdict"
  done

  local score
  score=$(awk "BEGIN { printf \"%.2f\", $points / $total }")
  LAST_FLOW_DETAIL="score=$score threshold=$threshold provider=$PROVIDER_FAMILY ${detail% }"
  awk -v s="$score" -v t="$threshold" 'BEGIN { exit (s >= t) ? 0 : 1 }'
}

run_and_judge() {
  local flow="$1" label="$2" input="$3"
  echo "[$flow] $label"
  local out
  if ! out=$(run_flow "$flow" "$input"); then
    LAST_FLOW_DETAIL="chat command failed"
    return 2
  fi
  evaluate_flow "$flow" "$input" "$out"
}

flow_clarify() {
  run_and_judge clarify "ambiguous currency query" $'엔화는?\n'
}

flow_install_chitchat() {
  run_and_judge install_chitchat "install exchange-rate flow then chitchat" $'환율 알려줘\n네\n오 잘하네!\n'
}

flow_installed_dispatch() {
  run_and_judge installed_dispatch "installed exchange-rate follow-up dispatch" $'환율 알려줘\n네\n환율\n'
}

flow_intent_aligned_format() {
  run_and_judge intent_aligned "KRW reframing after install" $'환율 알려줘\n네\n원화로 환율\n'
}

flow_install_explicit_request() {
  run_and_judge install_explicit_request "explicit install request after clarification" $'엔화는?\n네\n설치해줘요.\n'
}

flow_browse() {
  run_and_judge browse "browse available skills" $'어떤 스킬들이 있어요?\n'
}

flow_multimatch() {
  run_and_judge multimatch "multiple news skill matches" $'뉴스 관련 스킬 있어요?\n'
}

FLOWS=(
  [clarify]=flow_clarify
  [install_chitchat]=flow_install_chitchat
  [install_explicit_request]=flow_install_explicit_request
  [installed_dispatch]=flow_installed_dispatch
  [intent_aligned]=flow_intent_aligned_format
  [browse]=flow_browse
  [multimatch]=flow_multimatch
)

FLOW_ORDER=(clarify install_chitchat install_explicit_request installed_dispatch intent_aligned browse multimatch)

run_one() {
  local name="$1"
  local fn="${FLOWS[$name]:-}"
  if [[ -z "$fn" ]]; then
    finish INFRA 2 "unknown flow '$name' (valid: ${!FLOWS[*]})"
  fi
  "$fn"
}

main() {
  if [[ -n "${FLOW:-}" ]]; then
    if run_one "$FLOW"; then
      record_flow "$FLOW" PASS "$LAST_FLOW_DETAIL"
      finish PASS 0
    else
      rc=$?
      if (( rc == 2 )); then
        record_flow "$FLOW" INFRA "$LAST_FLOW_DETAIL"
        finish INFRA 2 "flow infrastructure failure: $FLOW"
      fi
      record_flow "$FLOW" FAIL "$LAST_FLOW_DETAIL"
      finish FAIL 1 "flow assertion failed: $FLOW"
    fi
  fi

  local fail=0
  for name in "${FLOW_ORDER[@]}"; do
    LAST_FLOW_DETAIL=""
    if "${FLOWS[$name]}"; then
      record_flow "$name" PASS "$LAST_FLOW_DETAIL"
    else
      rc=$?
      if (( rc == 2 )); then
        record_flow "$name" INFRA "$LAST_FLOW_DETAIL"
        finish INFRA 2 "flow infrastructure failure: $name"
      fi
      record_flow "$name" FAIL "$LAST_FLOW_DETAIL"
      fail=$((fail + 1))
    fi
    echo
  done
  if (( fail > 0 )); then
    finish FAIL 1 "FAILED: $fail flow(s)"
  fi
  finish PASS 0
}

main "$@"
