#!/usr/bin/env bash
# Secretary Smoke runner.
#
# Reads each fixture jsonl, sends the input to KittyPaw via `kittypaw chat`,
# captures the response, then asks a small judge LLM to score each expected
# behavior. Antipattern substrings are matched deterministically (no LLM).
#
# Output: eval/secretary_smoke/results/{category}.jsonl + summary.md
#
# Required env:
#   ANTHROPIC_API_KEY (or read from default tenant config.toml)

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
EVAL_DIR="$ROOT_DIR/eval/secretary_smoke"
FIX_DIR="$EVAL_DIR/fixtures"
OUT_DIR="$EVAL_DIR/results"
SUMMARY="$OUT_DIR/summary.md"
LOCK_DIR="$EVAL_DIR/.runner.lock"
KITTY_BIN="${KITTY_BIN:-$ROOT_DIR/bin/kittypaw}"
JUDGE_MODEL="${JUDGE_MODEL:-claude-haiku-4-5-20251001}"
RUN_ACCOUNT="${KITTYPAW_ACCOUNT:-auto}"
RUN_PROVIDER="${KITTYPAW_EVAL_PROVIDER:-configured}"
RUN_MODEL="${KITTYPAW_EVAL_MODEL:-configured}"
RUN_SERVER="${KITTYPAW_EVAL_SERVER:-${KITTYPAW_EVAL_DAEMON:-local}}"
FINISHED=0

mkdir -p "$OUT_DIR"

write_summary_header() {
  {
    echo "# Secretary Smoke Results"
    echo
    echo "Date: $(date -u +'%Y-%m-%d %H:%M UTC')"
    echo "State: RUNNING"
    echo "Provider: $RUN_PROVIDER"
    echo "Model: $RUN_MODEL"
    echo "Judge model: $JUDGE_MODEL"
    echo "Account: $RUN_ACCOUNT"
    echo "Server: $RUN_SERVER"
    echo
  } > "$SUMMARY"
}

cleanup() {
  rmdir "$LOCK_DIR" 2>/dev/null || true
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
  cleanup
  echo "STATE: $state"
  [[ -n "$detail" ]] && echo "$detail" >&2
  exit "$code"
}

trap 'rc=$?; if (( rc != 0 && FINISHED == 0 )); then cleanup; echo "STATE: INFRA"; echo "runner aborted with exit $rc" >&2; exit 2; fi; cleanup' EXIT

need_cmd() {
  if ! command -v "$1" >/dev/null 2>&1; then
    finish INFRA 2 "required command not found: $1"
  fi
}

write_summary_header

if [[ "${KITTYPAW_EVAL_SKIP:-}" == "1" ]]; then
  finish NOT_RUN 3 "KITTYPAW_EVAL_SKIP=1"
fi

need_cmd jq
need_cmd curl
need_cmd python3

# Single-instance lock (prevents the multi-runner race we hit during the
# first eval pass — multiple bash run.sh writing to results/ simultaneously
# corrupted the per-category jsonl and the summary).
#
# `mkdir` is atomic across POSIX, macOS-friendly (flock is Linux-only).
if ! mkdir "$LOCK_DIR" 2>/dev/null; then
  finish INFRA 2 "Another runner already holds $LOCK_DIR"
fi

# Resolve Anthropic key.
if [[ -z "${ANTHROPIC_API_KEY:-}" ]]; then
  ANTHROPIC_API_KEY=$(grep -m1 'api_key = "sk-ant' ~/.kittypaw/accounts/default/config.toml 2>/dev/null | sed 's/.*"\(sk-ant[^"]*\)".*/\1/')
fi
if [[ -z "$ANTHROPIC_API_KEY" ]]; then
  finish INFRA 2 "ANTHROPIC_API_KEY not set and not found in accounts/default/config.toml"
fi

# Strip ANSI + spinner glyphs + REPL prompts from `kittypaw chat <text>`
# output, leaving only the model's reply. The script is in a sibling file
# because heredoc-as-stdin and pipe-as-stdin collide for `python3 - <<EOF`.
CLEAN_CHAT_PY="$EVAL_DIR/clean_chat.py"
strip_chat_output() {
  python3 "$CLEAN_CHAT_PY"
}

# Substring antipattern check. Returns 0 if any antipattern matched.
check_antipatterns() {
  local response="$1"
  shift
  local hit=0
  for pat in "$@"; do
    if [[ "$response" == *"$pat"* ]]; then
      hit=1
      echo "  antipattern_hit=$pat" >&2
    fi
  done
  return $hit
}

# Behavior definitions — short prompt for the judge LLM.
behavior_def() {
  case "$1" in
    clarify_intent) echo "The response acknowledges the query is ambiguous and either asks a clarifying question or explicitly states a working interpretation before proceeding." ;;
    enumerate_interpretations) echo "The response lists two or more possible interpretations of the user query." ;;
    suggest_skill) echo "The response surfaces that a domain-specific tool/skill could give better information (mentions install, recommend, or a specific skill name)." ;;
    expand_query) echo "The response proposes search refinement: a different keyword, a specific source/site, or asks the user for guidance on where to look next." ;;
    honest_uncertainty) echo "The response explicitly acknowledges uncertainty or limitation in the data/source instead of fabricating a confident answer." ;;
    source_timestamp) echo "The response indicates the time-frame or source of the information (mentions 'as of', 'today', or that data may be outdated)." ;;
    first_person) echo "The response speaks from the assistant's perspective (e.g. 'I checked', '찾아본 결과로는') without framing the tool/search output as something the user provided." ;;
    *) echo "Unknown behavior" ;;
  esac
}

# Ask the judge LLM whether `response` exhibits `behavior` for `query`.
# Echoes one of: PASS / PARTIAL / FAIL.
judge_behavior() {
  local query="$1"
  local response="$2"
  local behavior="$3"
  local def
  def=$(behavior_def "$behavior")

  local body
  body=$(jq -n \
    --arg model "$JUDGE_MODEL" \
    --arg q "$query" \
    --arg r "$response" \
    --arg b "$behavior" \
    --arg d "$def" \
    '{
      model: $model,
      max_tokens: 80,
      messages: [{
        role: "user",
        content: ("You evaluate whether an AI assistant response exhibits a target behavior.\n\nUser query: " + $q + "\n\nAssistant response:\n" + $r + "\n\nBehavior name: " + $b + "\nBehavior definition: " + $d + "\n\nAnswer with strictly one token (PASS / PARTIAL / FAIL) on the first line, then a one-sentence reason.")
      }]
    }')

  local api_response
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

  local result
  result=$(echo "$api_response" | jq -r '.content[0].text // empty' | head -n1 | tr -d '[:space:]' | tr '[:lower:]' '[:upper:]')
  if [[ -z "$result" ]]; then
    return 2
  fi

  case "$result" in
    PASS) echo "PASS" ;;
    PARTIAL) echo "PARTIAL" ;;
    *) echo "FAIL" ;;
  esac
}

# Score a single fixture file. Echoes an aggregate JSON line.
score_category() {
  local fixture="$1"
  local category
  category=$(basename "$fixture" .jsonl)
  local out="$OUT_DIR/${category}.jsonl"
  : > "$out"

  local total_q=0
  local pass_q=0  # queries with score >= 1.5
  local antipattern_hits=0

  while IFS= read -r line; do
    [[ -z "$line" ]] && continue
    total_q=$((total_q + 1))

    local id input expected antipatterns
    id=$(echo "$line" | jq -r '.id')
    input=$(echo "$line" | jq -r '.input')
    expected=$(echo "$line" | jq -r '.expected_behaviors | join(",")')
    antipatterns=$(echo "$line" | jq -r '.antipatterns[]')

    echo "[$category] $id: $input" >&2

    # Run KittyPaw.
    local raw_response
    if ! raw_response=$("$KITTY_BIN" chat "$input" 2>&1); then
      echo "chat command failed for $category/$id:" >&2
      echo "$raw_response" >&2
      return 2
    fi
    local response
    response=$(echo "$raw_response" | strip_chat_output)

    # Antipattern check.
    local antipattern_penalty=0
    while IFS= read -r pat; do
      [[ -z "$pat" ]] && continue
      if [[ "$response" == *"$pat"* ]]; then
        antipattern_penalty=1
        antipattern_hits=$((antipattern_hits + 1))
        echo "    ANTIPATTERN: $pat" >&2
        break
      fi
    done <<< "$antipatterns"

    # Behavior judge calls.
    local points=0
    local total_b=0
    local behavior_results="["
    IFS=',' read -ra bs <<< "$expected"
    for b in "${bs[@]}"; do
      total_b=$((total_b + 1))
      local verdict
      if ! verdict=$(judge_behavior "$input" "$response" "$b"); then
        echo "judge request failed for $category/$id behavior=$b" >&2
        return 2
      fi
      [[ "$verdict" == "PASS" ]] && points=$((points + 2))
      [[ "$verdict" == "PARTIAL" ]] && points=$((points + 1))
      behavior_results+="{\"behavior\":\"$b\",\"verdict\":\"$verdict\"},"
      echo "    $b -> $verdict" >&2
    done
    behavior_results="${behavior_results%,}]"

    # Compute score: (passed / total) * 2 - penalty (0 or 1).
    local score
    if (( total_b > 0 )); then
      score=$(awk "BEGIN { s = ($points / $total_b) - $antipattern_penalty * 0.5; if (s < 0) s = 0; printf \"%.2f\", s }")
    else
      score="0.00"
    fi

    # Track pass count.
    awk -v s="$score" 'BEGIN { exit (s >= 1.5) ? 0 : 1 }' && pass_q=$((pass_q + 1)) || true

    jq -n \
      --arg id "$id" \
      --arg input "$input" \
      --arg category "$category" \
      --arg response "$response" \
      --argjson behaviors "$behavior_results" \
      --argjson penalty "$antipattern_penalty" \
      --arg score "$score" \
      '{id: $id, input: $input, category: $category, response: $response, behaviors: $behaviors, antipattern_penalty: $penalty, score: ($score | tonumber)}' >> "$out"
  done < "$fixture"

  jq -n \
    --arg category "$category" \
    --argjson total "$total_q" \
    --argjson pass "$pass_q" \
    --argjson antihit "$antipattern_hits" \
    '{category: $category, total: $total, pass: $pass, antipattern_hits: $antihit}'
}

# Threshold check per category. Returns 0 if passes, 1 if fails.
check_threshold() {
  local agg_json="$1"
  local category total pass antihit
  category=$(echo "$agg_json" | jq -r '.category')
  total=$(echo "$agg_json" | jq -r '.total')
  pass=$(echo "$agg_json" | jq -r '.pass')
  antihit=$(echo "$agg_json" | jq -r '.antipattern_hits')

  local threshold_msg=""
  case "$category" in
    vague)       (( pass >= 6 )) && threshold_msg="PASS" || threshold_msg="FAIL: $pass/8 (need 6+)" ;;
    domain)      (( pass >= 3 )) && threshold_msg="PASS" || threshold_msg="FAIL: $pass/5 (need 3+)" ;;
    weak_serp)   (( pass >= 3 )) && threshold_msg="PASS" || threshold_msg="FAIL: $pass/5 (need 3+)" ;;
    framing)     (( antihit < 2 )) && threshold_msg="PASS" || threshold_msg="FAIL: $antihit antipattern hits (need <2)" ;;
    stale)       (( pass >= 8 )) && threshold_msg="PASS" || threshold_msg="FAIL: $pass/10 (need 8+)" ;;
    *)           threshold_msg="UNKNOWN" ;;
  esac

  echo "$threshold_msg"
}

{
  echo "| Category | Total | Pass (≥1.5) | Antipattern hits | Threshold |"
  echo "|---|---|---|---|---|"
} >> "$SUMMARY"

categories=(vague domain weak_serp framing stale)
overall_pass=0
overall_categories=0

for cat in "${categories[@]}"; do
  fixture="$FIX_DIR/${cat}.jsonl"
  [[ ! -f "$fixture" ]] && { echo "Skipping (no fixture): $cat"; continue; }
  overall_categories=$((overall_categories + 1))
  echo "==== $cat ====" >&2

  if ! agg=$(score_category "$fixture"); then
    finish INFRA 2 "score category failed: $cat"
  fi
  threshold=$(check_threshold "$agg")
  total=$(echo "$agg" | jq -r '.total')
  pass=$(echo "$agg" | jq -r '.pass')
  antihit=$(echo "$agg" | jq -r '.antipattern_hits')

  echo "| $cat | $total | $pass | $antihit | $threshold |" >> "$SUMMARY"
  [[ "$threshold" == PASS* ]] && overall_pass=$((overall_pass + 1))
done

{
  echo
  echo "**Overall: $overall_pass / $overall_categories categories passed.**"
  echo
  if (( overall_pass >= 4 )); then
    echo "Sub-plan A pass criterion (4/5 categories) MET ✅"
  else
    echo "Sub-plan A pass criterion (4/5 categories) NOT MET ❌"
  fi
} >> "$SUMMARY"

cat "$SUMMARY"
if (( overall_pass >= 4 )); then
  finish PASS 0
else
  finish FAIL 1 "Sub-plan A pass criterion not met: $overall_pass / $overall_categories categories"
fi
