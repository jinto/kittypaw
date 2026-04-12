# Sandbox Runtime Alternatives: deno 의존성 제거

> 2026-04-13 | 조사 배경: gopaw는 JS 샌드박스로 deno subprocess를 사용 중. 외부 런타임 의존성을 제거하여 단일 바이너리 배포를 달성하는 방법을 조사.

## 핵심 결론

**goja (순수 Go JS 엔진)가 가장 단순하고 실용적인 해법이다.**

QuickJS+Wasm+wazero는 과잉이었다. `go get` 한 줄이면 끝나는 방법이 있다.

## 후보 비교

### Tier 1: 실질적 후보

| | goja | modernc.org/quickjs | fastschema/qjs (Wasm) |
|---|---|---|---|
| 방식 | 순수 Go JS 엔진 | C→Go 트랜스파일 (ccgo) | QuickJS→Wasm→wazero |
| CGO | 없음 | 없음 | 없음 |
| 외부 의존성 | 없음 | 없음 | wazero |
| 레이어 수 | 1 | 1 | 3 |
| ES 지원 | ~ES2021 | ES2023 | ES2023 |
| 바이너리 추가 | ~11MB | ~5-8MB | ~15MB+ |
| Stars | 6,820 | 신규 | 556 |
| Go 함수 호출 | 94ns/call 직접 호출 | RegisterFunc | SetFunc |
| 프로덕션 검증 | Grafana k6 (27k stars) | modernc.org/sqlite와 동일 철학 | 소규모 |

### Tier 2: 탈락

| 후보 | 탈락 이유 |
|---|---|
| **deno 번들링** | 156MB 바이너리. 꼬리가 몸통을 흔듦 |
| **deno go:embed** | 크로스 컴파일 불가, 100MB+ 바이너리 |
| **deno auto-download** | 첫 실행 시 네트워크 필요. 임시방편 |
| **Starlark** | Python처럼 보이지만 Python이 아님 → LLM이 잘못된 코드 생성 |
| **Lua (GopherLua)** | LLM의 Lua 생성 품질이 JS 대비 열등 |
| **Tengo, Risor** | LLM 학습 데이터 부족. 코드 생성 불가 |
| **Yaegi (Go 인터프리터)** | 2년간 릴리스 없음, 174개 오픈 버그, 24MB |
| **v8go** | CGO 필요 → No CGO 원칙 위반 |

## goja vs modernc.org/quickjs 심층 비교

### goja의 장점
- **Grafana k6에서 프로덕션 검증** — 27k stars 프로젝트가 정확히 같은 패턴(Go 앱에서 JS 실행 + Go 함수 호출)으로 사용 중
- **API가 직관적** — `vm.Set("funcName", goFunc)` 한 줄로 Go 함수 노출
- **커뮤니티가 크고 안정적** — 6,820 stars, 6년 이상 유지보수
- **디버깅 용이** — 순수 Go이므로 Go 디버거로 JS 실행 과정 추적 가능

### modernc.org/quickjs의 장점
- **ES2023 완전 지원** — goja에 없는 async iterators, modules 지원
- **바이너리 더 작음** — ~5-8MB vs ~11MB
- **gopaw 철학과 일치** — 이미 modernc.org/sqlite를 같은 이유(ccgo, No CGO)로 사용 중

### GoPaw에서 중요한 것

GoPaw의 LLM이 생성하는 코드 특성:
- 변수 선언, 함수 호출, 문자열/JSON 처리, 연산
- `Http.get()`, `Env.get()` 등 스킬 함수 동기 호출
- `return` 문으로 결과 반환

goja에서 빠진 기능 중 GoPaw에 필요한 것: **없음**.
- async iterators → 스킬 호출이 동기이므로 불필요
- import/export → 단일 스크립트 실행이므로 불필요
- WeakRef → 단발성 실행이므로 불필요

## 추천

### 1순위: goja

```
go get github.com/dop251/goja
```

이유:
1. 가장 단순한 마이그레이션 경로 (JS 유지, subprocess만 제거)
2. Grafana k6에서 검증된 패턴
3. exec.go + wrapper.go → ~50줄로 교체 가능
4. LLM이 생성하는 JS 코드 변경 불필요 (시스템 프롬프트 수정 없음)

### 2순위: modernc.org/quickjs

goja의 ES 지원이 실제로 부족할 경우의 폴백. modernc 생태계와의 일관성이 장점.

### 보류: fastschema/qjs

goja → modernc/quickjs 둘 다 문제가 있을 때만 고려. 레이어가 많아 복잡도 증가.

## 마이그레이션 스케치 (goja)

현재:
```
LLM → JS 코드 → wrapper.go (스텁 생성) → exec.go (deno subprocess + stdin/stdout 파이프)
```

이후:
```
LLM → JS 코드 → goja VM (vm.Set으로 스킬 함수 직접 등록, vm.RunString으로 실행)
```

삭제 대상:
- `sandbox/exec.go`: subprocess 생성, 파이프 I/O, pickRuntime()
- `sandbox/wrapper.go`: Deno/Node 감지 JS, stdin/stdout 프로토콜, jsIOHelpers

유지:
- `sandbox/sandbox.go`: Sandbox 구조체, SkillResolver 인터페이스
- 스킬 레지스트리 기반 함수 등록 로직 (wrapper.go에서 추출하여 재사용)

## 출처

- goja: https://github.com/dop251/goja
- modernc.org/quickjs: https://pkg.go.dev/modernc.org/quickjs / https://gitlab.com/cznic/quickjs
- fastschema/qjs: https://github.com/fastschema/qjs
- wazero: https://github.com/tetratelabs/wazero
- Grafana k6: https://github.com/grafana/k6
- GopherLua: https://github.com/yuin/gopher-lua
- Starlark: https://github.com/google/starlark-go
- Yaegi: https://github.com/traefik/yaegi
