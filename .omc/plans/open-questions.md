# Open Questions

## oochy-gui-workspace — 2026-03-28 (Iteration 2)

- [ ] **Vector embedding 제공자 선택** — Claude API embeddings vs 로컬 ONNX 모델 (all-MiniLM). 오프라인 지원 여부와 embedding 품질 트레이드오프에 영향.
- [ ] **SQLite vector 저장 방식** — `sqlite-vec` 확장 vs JSON serialized float array. sqlite-vec가 cosine similarity 쿼리 성능에서 우위지만 크로스 컴파일 복잡도가 증가함.
- [ ] **Agent system prompt 구조** — 파일 작업 전용 prompt를 새로 설계할지, 기존 `SYSTEM_PROMPT`에 workspace context block을 추가할지. Agent 응답 품질(특히 structured output)에 직접 영향.
- [ ] **ThreadSandbox 보안 강화 로드맵** — 현재 QuickJS VM-level 격리만 제공. 향후 `std::process::Command` 기반 프로세스 격리로 전환할지, VM-level로 충분한지. GUI에서 실행되는 코드는 LLM 생성이므로 리스크가 낮지만, 장기적 보안 전략 결정 필요.

### Resolved (Iteration 2에서 해결)

- [x] **Sandbox Windows 격리 전략** — `SandboxBackend` trait으로 추상화. `ThreadSandbox`는 `rquickjs::set_interrupt_handler()`로 timeout 구현. OS-level 격리 없음을 명시적으로 수용. (Architect 피드백 #2)
- [x] **Store 동시성 모델** — `Arc<Mutex<Store>>` 선택. `r2d2-sqlite`는 MVP에서 과도한 복잡도. (Architect 피드백 #3)
- [x] **GUI-Channel 관계** — GUI는 Channel trait bypass. Tauri commands에서 `run_agent_loop()` 직접 호출. (Critic 피드백 #5)
- [x] **DB 이중 접근** — `store.rs` + `skill_executor::open_storage_db()`를 `oochy-store` crate로 통합. (Critic 피드백 #4)
- [x] **Migration versioning** — `rusqlite_migration` crate 사용. (Critic 피드백 #7)
- [x] **LLM streaming** — `generate_stream()` + `on_token` callback 패턴. Tauri event emit으로 프론트엔드 전달. (Critic 피드백 #6)
- [x] **API key UX** — Settings UI + onboarding wizard로 비개발자 지원. (Critic 피드백 #8)
- [x] **App data directory** — Tauri `app_data_dir()` 사용. (Critic 피드백 #9)

## oochy-permission-system - 2026-03-28

- [ ] **팝업 timeout 값** — 현재 30초 제안. 너무 짧으면 읽기 전에 거부됨, 너무 길면 에이전트가 멈춰 있는 느낌. 사용자 경험 테스트 후 조정 필요.
- [ ] **glob crate 선택** — `glob` vs `globset` (burntsushi). globset이 더 빠르고 기능이 많지만 의존성 추가. 현재 프로젝트에 둘 다 없으므로 선택 필요.
- [ ] **PRAGMA foreign_keys 설정** — 워크스페이스 삭제 시 permission rules CASCADE 삭제를 위해 필요. 현재 Store에서 `foreign_keys = ON` 설정 여부 확인 필요 (rusqlite 기본값은 OFF).
- [ ] **Permission 거부 시 retry 방지** — 현재 agent_loop은 에러 시 retry. Permission 거부는 retry해도 같은 결과이므로 별도 에러 타입으로 분기하여 retry 스킵 필요할 수 있음.
- [ ] **다중 팝업 동시 발생 시 UX** — 에이전트가 여러 파일에 연속 접근하면 팝업이 쏟아질 수 있음. 큐잉 또는 배치 승인 UI 고려 필요.
- [ ] **SandboxConfig.allowed_paths 공존 방식** — spec에서 "활성화 및 연결". 새 permission system과 어떻게 공존할지. 제안: allowed_paths를 initial seed로 사용하고, 이후 SQLite 규칙이 우선.

## skill-platform — 2026-03-28

- [ ] **Secret storage strategy** — Base64 encoding is a placeholder. Should Phase 1 integrate macOS Keychain via `security-framework` crate, or defer to a follow-up? Impacts user trust if tokens are visible in plaintext config files.
- [ ] **Bundled packages and API keys** — The example packages (macro-economy-report, weather-briefing) require third-party API keys (FRED, OpenWeatherMap). Should we bundle packages that work without API keys (e.g., reminder, url-monitor) as the default showcase, and mark API-dependent ones as "requires setup"?
- [ ] **Package distribution format for Phase 2+** — When we move beyond bundled packages, should distribution be `.zip` files, GitHub repo URLs, or a custom `.kpkg` archive? This affects the install UX significantly.
- [ ] **Config field type: `cron`** — Should the cron input be a raw cron expression with validation, or a friendly UI (dropdowns for "every day at X", "every hour", etc.)? Non-developers may struggle with cron syntax.
- [ ] **Existing `teach` command relationship** — Should `teach`-generated skills be auto-packaged into the new package format, or remain as legacy `.skill.toml` + `.js`? Converting them would unify the skill management UI.
- [ ] **Scope of `File.write` sandbox global** — Should file operations be limited to a per-package `data/` directory, or should packages be able to write to a shared workspace directory? Shared access enables cross-package data but complicates security.
