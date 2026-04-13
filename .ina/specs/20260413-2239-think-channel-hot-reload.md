# Channel Hot-reload

서버 재시작 없이 채널을 동적으로 추가/제거/교체한다.

## Goal

`ChannelSpawner`가 개별 채널의 라이프사이클을 `context.WithCancel` + `done channel`로 관리하며, goroutine 종료를 보장한다. Config reload 시 자동 diff, 온보딩 위자드에서 즉시 spawn.

## Constraints

- `ChannelSpawner`는 `server.Server` 필드로 배치 — `handleReload`에서 직접 `Reconcile` 호출
- `runningChannel` 구조체: `cancelFunc` + `Channel` 인스턴스 + `done chan struct{}`
- `Stop(name)`은 cancel 호출 후 `done` 수신까지 블로킹 (goroutine 종료 보장)
- `Reconcile` 부분 실패 시 best-effort: 개별 실패 로그 + 나머지 계속 + 기존 채널 보존
- `retryPendingResponses`는 Spawner에서 동적 채널 조회 (스냅샷 map 대신)
- 디스패치 goroutine의 응답 라우팅도 Spawner에서 동적 조회

## Non-Goals

- WebSocket 채널 hot-reload (자체 HTTP 서버 포트 바인딩 문제)
- 채널 health check / 자동 재시작
- 채널별 독립 로그 스트리밍

## Acceptance Criteria

1. 서버 실행 중 `config.toml`에 Telegram 채널 추가 → `POST /api/v1/reload` → Telegram 메시지 수신 시작
2. `config.toml`에서 채널 제거 → reload → 해당 채널 goroutine 종료 확인 (`done` channel 완료)
3. 온보딩 위자드 `POST /api/setup/telegram`에서 토큰 저장 → `TrySpawn` 직접 호출 → reload 없이 즉시 수신 시작
4. `GET /api/v1/channels` → 현재 running 채널 목록 + 상태 반환
5. 채널 토큰 변경 후 reload → `ReplaceSpawn` (Stop 완료 대기 → 새 goroutine 시작)
6. Reconcile 중 일부 채널 spawn 실패 시 → 나머지 채널 정상 작동 + 실패 로그 출력

## Ontology

```
ChannelSpawner (server.Server 필드)
  ├── running map[string]runningChannel
  │     └── runningChannel { cancel context.CancelFunc, ch Channel, done chan struct{} }
  ├── eventCh chan<- core.Event (공유, 모든 채널이 같은 채널로 송신)
  ├── TrySpawn(ch Channel) error
  ├── Stop(name string) error        ← cancel + <-done 블로킹
  ├── ReplaceSpawn(ch Channel) error  ← Stop + TrySpawn
  ├── Reconcile([]ChannelConfig) error ← config diff
  ├── GetChannel(name string) Channel ← 동적 조회 (응답 라우팅 + retry용)
  └── List() []ChannelStatus          ← GET /api/v1/channels

channel.FromConfig(cfg) Channel       ← 기존 팩토리, 변경 없음

handleReload → config swap + spawner.Reconcile(newCfg.Channels)
handleSetupTelegram → spawner.TrySpawn(telegramCh)
dispatchLoop → spawner.GetChannel(event.Type) 로 응답 라우팅
retryPendingResponses → spawner.GetChannel(eventType) 로 동적 조회
```

## Multi-perspective Review

| 관점 | 판정 | 핵심 피드백 | 반영 |
|------|------|------------|------|
| Architect | 조건부 APPROVED | done channel로 Stop 완료 보장, Spawner를 Server 필드로, retry 동적 조회 | 전부 반영 |
| Critic | ITERATE→반영 | Stop 완료 보장, Reconcile 부분 실패 정책, AC3 트리거 명시 | 전부 반영 |
| CEO | SELECTIVE | 채널 상태 조회 API 추가, 롤백 전략 | 반영. 온보딩 분리 제안은 미채택 |
