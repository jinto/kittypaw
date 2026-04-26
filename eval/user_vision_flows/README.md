# User-vision multi-turn regression smoke

LLM-in-the-loop scenarios are not unit-testable — the same query
routes through different prompts/code each run. This script pins the
canonical multi-turn flows the user actually walked through during the
"진짜 비서답게" assistant-quality work, so a future change can be
sanity-checked with one command.

## Why a separate path from `eval/secretary_smoke/`

`secretary_smoke` is single-turn fixtures with an LLM judge — designed
for behavior-class breadth (vague / domain / weak_serp / framing /
stale). This directory is for **multi-turn flows tied to specific
user-visible commits** (clarify → install → browse → chitchat). The
assertions are substring presence, not LLM-judge — they're cheap and
local but require fresh state per run.

## Run

Build first, then:

```bash
./eval/user_vision_flows/run.sh                # all flows
FLOW=clarify ./eval/user_vision_flows/run.sh   # just one
```

Each flow stops the daemon, wipes installed packages/skills, then
pipes a multi-turn input into `kittypaw chat`. ~4 chat sessions per
full run, a few cents in LLM cost.

## Flows

| Flow | Sequence | Validates |
|---|---|---|
| `clarify` | "엔화는?" | Single-token clarify path: returns "환율 말씀이세요?" without fabricating a rate. (commit `da24a86`, `df6907c` lineage) |
| `install_chitchat` | 환율 알려줘 → 네 → 오 잘하네! | Evidence + suffix offer → install ack with ✅ + live ECB rates → chitchat ack. (commits `463a48c`, `15b615d`) |
| `browse` | 어떤 스킬들이 있어요? | `Skill.search("")` → grouped category list, no auto-install. (commit `26d25c2`) |
| `multimatch` | 뉴스 관련 스킬 있어요? | ≥2 hits surfaced as options, no auto-install. (commit `15b615d`) |
| `missing_skill` | (not yet automatable from chat) | Marker — see `engine/executor_test.go` once a deterministic harness lands. |

## When to add a flow

Whenever a commit has the shape "fixed user-visible chat regression
that took N tries to land": add the canonical sequence here so the
next refactor catches a re-break before the user does.

## When to update assertions

The substrings (`✅`, `1477.04 KRW`, `도움이 됐다니`, etc.) are
deliberately specific so the script catches both content regressions
and tone drift. If a *deliberate* wording change lands, update the
assertion in the same commit — that documents the new canonical
output.
