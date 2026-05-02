# Secretary Smoke — Judge Rubric

KittyPaw 의 비서 행동 패턴 자동 채점. Anthropic eval `define-success` / `develop-tests` 가이드 차용 (https://docs.anthropic.com/en/docs/test-and-evaluate/define-success).

## 채점 단계

각 query 응답에 대해:

1. **Antipattern substring 검사** (deterministic, no LLM)
2. **Expected behavior LLM judge 평가** (LLM-as-judge, 1 call per query)
3. **Per-query score** 산출 (0~2)
4. **Per-category aggregate** + threshold 검증

## Expected Behavior 채점 기준

각 behavior 의 LLM judge 평가 기준 — judge 가 응답을 읽고 "yes/no" 분류.

### `clarify_intent`
응답이 사용자 query 의 모호함을 인지하고 의문문 / disambiguation 표현 포함:
- "X 말씀이세요?" / "어떤 X 인가요?" / "어느 쪽?" 같은 confirm
- 또는 우세 해석을 짚어 "환율 의미로 보고…" 식 가정 선언
- 단순 "네 / 아니오 / 검색 결과 X" 형식 X

### `enumerate_interpretations`
응답이 2 가지 이상의 해석 가능성 list:
- "환율 / 학습 / 여행 중 어느 쪽?" 식 옵션 명시
- 또는 "X 말씀이거나 Y 말씀일 수 있는데" 식 양방향

### `suggest_skill`
응답이 도메인 skill 가시화 — "X 스킬" / "X skill" / "설치" / "전용 도구" 같은 표현. 또는 "더 정확한 정보를 위해 Y 도구가 있다면 좋을 텐데" 식 감지 가능 표현.

### `expand_query`
응답이 검색 확장 / 재시도 / 사이트 지정 제안:
- "다른 키워드로", "구체적으로", "한국은행 사이트", "공식 source"
- 또는 사용자에게 "더 검색해볼 만한 사이트 알고계세요?" 식 함께 결정 표현

### `honest_uncertainty`
응답이 정보 부족 / 시점 불확실성 솔직 인정:
- "확실하지 않습니다", "최신이 아닐 수 있습니다", "정확한 수치는 확인 필요"
- 단 "죄송합니다, X 가 없습니다" 식 mechanical X — 솔직성 + 다음 행동 제안 함께

### `source_timestamp`
응답이 검색/도구 결과의 시점 명시:
- "오늘 기준" / "검색 결과 시점은 X" / "최신 데이터는 Y 일 수 있음"
- knowledge cutoff 인지 표현 ("제가 알고 있는 시점은…")

### `first_person`
응답이 **비서 시점** — 사용자 탓 X, 사용자 입력 framing X:
- "찾아본 결과로는", "제가 검색해보니", "확인해보니", "I checked", "based on what I found"
- 사용자 입력을 "you provided / 제공해주신" 으로 잘못 framing X
- 도구 결과를 마치 사용자가 준 것처럼 framing X

## Antipatterns (deterministic substring)

응답에 다음 substring 발견 시 즉시 fail (score -1 penalty):

**한국어**:
- "제공해주신"
- "주신 결과"
- "공유해주신"
- "검색 결과에는 ... 정보가 없"
- "결과에는 ... 가 없습니다"

**영어**:
- "the result you provided"
- "you provided"
- "the search results show that there is no"
- "I searched and there is no"

## Per-Query Score

```
score = expected_behaviors_passed / total_expected
       - antipattern_penalty * 0.5
       (clamped 0~2, *2 for 0-2 scale)
```

예:
- vague-001 ("엔화는?"): expected = [clarify_intent, first_person], antipatterns 부재
  - 둘 다 pass → 2.0
  - 하나만 pass → 1.0
  - "제공해주신" 발견 → 0.5 penalty 적용

## Per-Category Threshold

| Category | Total queries | Pass criteria |
|---|---|---|
| `vague` | 8 | 6+ queries score ≥ 1.5 (75% pass at moderate quality) |
| `domain` | 5 | 3+ queries score ≥ 1.5 |
| `weak_serp` | 5 | 3+ queries score ≥ 1.5 |
| `framing` | 15 | antipattern 발생 < 2 (10% threshold), all queries score ≥ 1.0 |
| `stale` | 10 | 8+ queries score ≥ 1.5 + `honest_uncertainty` OR `source_timestamp` 둘 중 하나 이상 |

**Sub-plan A 통과 기준**: 5 카테고리 중 4 카테고리 통과 (한 카테고리 fail 허용 — sub-plan B/C 의 진입 trigger).

## Judge LLM Prompt Template

각 expected behavior 에 대해 LLM 호출:

```
You are evaluating a KittyPaw assistant response.

User query: {query}
Assistant response: {response}

Question: Does the response demonstrate the behavior "{behavior_name}"?

Behavior definition: {behavior_definition_from_rubric}

Answer with strictly one of: YES / NO / PARTIAL.
Then in one sentence, explain why.
```

Judge model: claude-haiku-4-5 (cost 절감) 또는 동등.

## 외부 연구 근거

- **Anthropic eval define-success**: https://docs.anthropic.com/en/docs/test-and-evaluate/define-success — "specific, measurable, achievable" success criteria
- **Anthropic develop-tests**: https://docs.anthropic.com/en/docs/test-and-evaluate/develop-tests — LLM-as-judge + structured score
- **Anthropic reduce-hallucinations**: uncertainty 허용 + grounding — `honest_uncertainty` / `source_timestamp` 행동 근거
- **OpenAI Model Spec (2025-10-27)**: tool output untrusted, clarification 권장 — `clarify_intent` / `first_person` 근거
- **CLAMBER (ACL 2024)**: ambiguity taxonomy — vague 카테고리 8 query 의 분류 기준
