#!/usr/bin/env bash
#
# User-vision multi-turn regression smoke.
#
# Why this exists: the assistant-quality work is driven by live REPL
# scenarios the user actually walked through (clarify → install → browse →
# chitchat). The LLM in the loop makes traditional unit tests unreliable —
# the same query routes through different code each run. This script pins
# a small set of canonical flows so a future prompt/skill/UX change can
# be sanity-checked with one command.
#
# Each flow:
#   1. Stops the daemon and wipes installed packages/skills (fresh state).
#   2. Pipes a multi-turn input sequence into `kittypaw chat`.
#   3. Strips spinner/ANSI noise (clean_chat.py shared with secretary_smoke).
#   4. Runs substring assertions against the joined output.
#
# The assertions are deliberately loose — substring presence, not exact
# wording — because the LLM rephrases. Anything tighter would be flaky.
# Adjust the substrings (or add LLM-judge variants) when the canonical
# response wording shifts.
#
# Cost: ~4 chat sessions × Claude 4 = a few cents per run. Run before any
# QualityBlock / Capability / Decision / Evidence prompt change.
#
# Usage:
#   ./eval/user_vision_flows/run.sh                # all flows
#   FLOW=clarify ./eval/user_vision_flows/run.sh   # single flow

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
KITTY_BIN="${KITTY_BIN:-$ROOT_DIR/bin/kittypaw}"
CLEAN_CHAT="$ROOT_DIR/eval/secretary_smoke/clean_chat.py"

if [[ ! -x "$KITTY_BIN" ]]; then
  echo "ERROR: kittypaw binary not found at $KITTY_BIN — run 'make build' first" >&2
  exit 2
fi

if [[ ! -f "$CLEAN_CHAT" ]]; then
  echo "ERROR: clean_chat.py missing at $CLEAN_CHAT" >&2
  exit 2
fi

reset_daemon() {
  "$KITTY_BIN" stop >/dev/null 2>&1 || true
  # Force-kill any stale serve processes — `stop` only targets the
  # pid in daemon.pid, but earlier sessions may have left orphans
  # whose in-memory PipelineState cache would leak into the next
  # flow's first turn (Phase 10 cross-turn augmentation regression).
  pkill -9 -f "kittypaw serve" 2>/dev/null || true
  rm -rf "$HOME/.kittypaw/tenants/default/packages/"* \
         "$HOME/.kittypaw/tenants/default/skills/"* 2>/dev/null || true
  sleep 1
}

# assert_contains <flow_name> <substring> <full_output>
assert_contains() {
  local name="$1" needle="$2" hay="$3"
  if [[ "$hay" == *"$needle"* ]]; then
    echo "  OK   $name → '$needle'"
  else
    echo "  FAIL $name → expected '$needle'" >&2
    echo "  --- response (truncated 800ch) ---" >&2
    printf '%s\n' "${hay:0:800}" >&2
    echo "  ----------------------------------" >&2
    return 1
  fi
}

assert_not_contains() {
  local name="$1" needle="$2" hay="$3"
  if [[ "$hay" != *"$needle"* ]]; then
    echo "  OK   $name → no '$needle'"
  else
    echo "  FAIL $name → unexpected '$needle'" >&2
    return 1
  fi
}

# run_flow <name> <stdin_sequence>
# Returns the cleaned chat output via stdout.
run_flow() {
  local name="$1" input="$2"
  reset_daemon
  printf '%s' "$input" | "$KITTY_BIN" chat 2>&1 | python3 "$CLEAN_CHAT"
}

# ---- Flows ----

flow_clarify() {
  echo "[clarify] 엔화는? → '환율 말씀이세요?'"
  local out
  out=$(run_flow clarify $'엔화는?\n')
  assert_contains "clarify" "환율 말씀이세요" "$out"
  assert_not_contains "clarify-no-fabrication" "1477" "$out"
}

flow_install_chitchat() {
  echo "[install_chitchat] 환율 알려줘 → 네 → 오 잘하네!"
  local out
  out=$(run_flow install_chitchat $'환율 알려줘\n네\n오 잘하네!\n')
  assert_contains "evidence" "환율" "$out"
  assert_contains "install-ack" "✅" "$out"
  assert_contains "live-rates" "Frankfurter" "$out"
  assert_contains "chitchat-ack" "도움이 됐다니" "$out"
}

flow_installed_dispatch() {
  echo "[installed_dispatch] 환율 알려줘 → 네 → 환율 (직접 dispatch, no re-install offer)"
  # Reproduces the 2026-04-27 transcript turn 5 regression: with the
  # exchange-rate skill already installed, the legacy LLM was emitting
  # another "설치해드릴까요?" suffix on a follow-up "환율" query, ignoring
  # the prompt's PRIORITY rule. The Phase 4 RunInstalledSkillBranch
  # short-circuits to Skill.run before the LLM is consulted.
  local out
  out=$(run_flow installed_dispatch $'환율 알려줘\n네\n환율\n')
  assert_contains "install-ack" "✅" "$out"
  assert_contains "live-rates" "Frankfurter" "$out"
  # T3 follow-up should be the rates again, not another install offer.
  # Count install-acks: usually exactly one (T2). Up to two is tolerated
  # because the legacy LLM at T1 occasionally generates an ack-shaped
  # phrase ("…스킬을 설치했어요" 같은) inside its suffix-offer prose, and
  # that LLM output is stochastic. Three or more means T3 actually
  # re-installed the skill — that we still catch.
  local ack_count
  ack_count=$(printf '%s' "$out" | grep -c "스킬을 설치했어요" || true)
  if [[ "$ack_count" -gt 2 ]]; then
    echo "  FAIL installed-dispatch-no-reinstall → '스킬을 설치했어요' count=$ack_count (want ≤2)" >&2
    return 1
  fi
  echo "  OK   installed-dispatch-no-reinstall → '스킬을 설치했어요' count=$ack_count (≤2)"
  # No new install offer on T3.
  local offer_count
  offer_count=$(printf '%s' "$out" | grep -c "설치해드릴까요\|설치를 도와드릴까요" || true)
  if [[ "$offer_count" -gt 1 ]]; then
    echo "  FAIL installed-dispatch-no-suffix-loop → install offer count=$offer_count (want ≤1)" >&2
    return 1
  fi
  echo "  OK   installed-dispatch-no-suffix-loop → install offer count=$offer_count"
}

flow_intent_aligned_format() {
  echo "[intent_aligned] 환율 알려줘 → 네 → 원화로 환율 (KRW reframe via mediateSkillOutput)"
  # Reproduces the 2026-04-27 transcript turn 3 regression: with the
  # exchange-rate skill installed, "원화로 환율" was returning USD-base
  # raw output verbatim because RunInstalledSkillBranch dispatched the
  # skill but never reframed the response. mediateSkillOutput (Phase 7)
  # passes the raw output + user query through a small LLM call so the
  # query modifier ("원화로") lands in the response without any change
  # to the skill JS itself.
  local out
  out=$(run_flow intent_aligned $'환율 알려줘\n네\n원화로 환율\n')
  assert_contains "install-ack" "✅" "$out"
  # T3 응답: KRW base reframe — "원" 또는 "KRW" 단위 표기 등장.
  # Stochastic by nature of LLM rephrasing — assertion is presence of
  # *either* token, not a specific phrase. Strong numeric assertion is
  # avoided (Round 4 placebo class re-emergence risk).
  if [[ "$out" == *"원"* ]] || [[ "$out" == *"KRW"* ]]; then
    echo "  OK   intent-aligned-krw-reframe → '원' or 'KRW' present"
  else
    echo "  FAIL intent-aligned-krw-reframe → neither '원' nor 'KRW' in response" >&2
    echo "  --- response (truncated 1200ch) ---" >&2
    printf '%s\n' "${out:0:1200}" >&2
    echo "  ----------------------------------" >&2
    return 1
  fi
  # Fabrication guard: vague hedge phrases like "약 1480원" suggest the
  # LLM invented a rate rather than reformatting the raw output. Only
  # the literal pattern is checked — too narrow to flake on legit
  # phrasings, too specific to silently approve fabrication.
  assert_not_contains "intent-aligned-no-vague-hedge" "약 14" "$out"
}

flow_install_explicit_request() {
  echo "[install_explicit_request] 엔화는? → 네 → 설치해줘요."
  # Reproduces the user transcript where "설치해줘요." (a complete
  # Korean sentence containing "설치", not the bare "네") used to be
  # mis-routed to a generic "어떤 스킬?" clarification. The Round-4
  # consent-trigger expansion + suffix-strict-wording should drive
  # the LLM straight from clarify → suffix offer → install in 3 turns.
  local out
  out=$(run_flow install_explicit_request $'엔화는?\n네\n설치해줘요.\n')
  assert_contains "clarify" "환율 말씀이세요" "$out"
  assert_contains "suffix-skill-name" "환율 조회" "$out"
  # 'paw>' style direct ack, not "어떤 스킬을 설치할지 알려주세요" loop
  assert_not_contains "no-clarify-loop" "어떤 스킬을 설치할지" "$out"
  assert_contains "install-ack" "✅" "$out"
  assert_contains "live-rates" "Frankfurter" "$out"
}

flow_browse() {
  echo "[browse] 어떤 스킬들이 있어요?"
  local out
  out=$(run_flow browse $'어떤 스킬들이 있어요?\n')
  assert_contains "browse-rate" "환율 조회" "$out"
  assert_contains "browse-news" "RSS 뉴스 요약" "$out"
  assert_contains "browse-weather" "현재 날씨" "$out"
  assert_not_contains "browse-no-auto-install" "✅" "$out"
}

flow_multimatch() {
  echo "[multimatch] 뉴스 관련 스킬 있어요?"
  local out
  out=$(run_flow multimatch $'뉴스 관련 스킬 있어요?\n')
  assert_contains "multimatch-rss" "RSS 뉴스" "$out"
  assert_contains "multimatch-daily" "오늘의 뉴스" "$out"
  # No auto-install — the prompt rule is "≥2 hits → ask which".
  assert_not_contains "multimatch-no-auto-install" "✅" "$out"
}

flow_missing_skill_grace() {
  echo "[missing_skill] 잘못된 id 의 Skill.run 호출이 발생했을 때 graceful 안내"
  # Indirect: this flow doesn't trigger the path reliably (the LLM picks
  # ids by itself). Kept as a marker — augment when we have a deterministic
  # way to invoke `Skill.run("nonexistent")` from chat.
  echo "  SKIP missing_skill_grace (no deterministic trigger; needs unit-level coverage in engine/executor_test.go)"
}

# ---- Driver ----

declare -A FLOWS=(
  [clarify]=flow_clarify
  [install_chitchat]=flow_install_chitchat
  [install_explicit_request]=flow_install_explicit_request
  [installed_dispatch]=flow_installed_dispatch
  [intent_aligned]=flow_intent_aligned_format
  [browse]=flow_browse
  [multimatch]=flow_multimatch
  [missing_skill]=flow_missing_skill_grace
)

run_one() {
  local name="$1"
  local fn="${FLOWS[$name]:-}"
  if [[ -z "$fn" ]]; then
    echo "ERROR: unknown flow '$name' (valid: ${!FLOWS[*]})" >&2
    exit 2
  fi
  "$fn"
}

main() {
  if [[ -n "${FLOW:-}" ]]; then
    run_one "$FLOW"
    return
  fi
  local fail=0
  for name in clarify install_chitchat install_explicit_request installed_dispatch intent_aligned browse multimatch missing_skill; do
    "${FLOWS[$name]}" || fail=$((fail+1))
    echo
  done
  if (( fail > 0 )); then
    echo "FAILED: $fail flow(s)" >&2
    exit 1
  fi
  echo "All flows passed."
}

main "$@"
