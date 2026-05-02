# KittyKakao

[KittyPaw](https://github.com/kittypaw-app)를 위한 메시징 릴레이 서버. 카카오톡 챗봇 메시지를 WebSocket으로 연결된 클라이언트에 중계합니다.

```
카카오톡 사용자 ──► 카카오 오픈빌더 ──► KittyKakao ──► WebSocket ──► KittyPaw 클라이언트
                                              ◄── 비동기 콜백 ◄── 응답 ◄──┘
```

## 동작 원리

1. KittyPaw 클라이언트가 등록하면 **페어링 코드**를 받습니다
2. 카카오톡 사용자가 챗봇에 6자리 코드를 보내 계정을 연결합니다
3. 사용자가 메시지를 보내면 카카오 오픈빌더가 KittyKakao로 전달합니다
4. KittyKakao가 WebSocket을 통해 페어링된 클라이언트에 메시지를 중계합니다
5. 클라이언트가 응답하면 KittyKakao가 카카오 비동기 콜백으로 답변을 전달합니다

## 주요 기능

- **WebSocket 릴레이** — 자동 ping/pong 유지
- **6자리 페어링** — 기기-계정 연결
- **비동기 콜백** — 카카오 오픈빌더 콜백 프로토콜 지원
- **속도 제한** — 일일/월간 메시지 상한 설정
- **킬스위치** — 메시지 처리 즉시 중단
- **SSRF 방어** — 콜백 URL을 `*.kakao.com`으로 제한
- **SQLite + WAL** — 외부 의존성 없는 영속 저장소
- **Unix 소켓** — nginx와 직접 통신
- **우아한 종료** — SIGTERM 시 연결을 안전하게 정리

## 빠른 시작

```bash
# 빌드
cargo build --release

# 설정
cp deploy/env.example .env
# .env 편집 — 최소한 WEBHOOK_SECRET 설정 필요

# 실행
WEBHOOK_SECRET=your-secret ./target/release/kittykakao
```

## 설정

모든 설정은 환경변수로 관리합니다:

| 변수 | 기본값 | 설명 |
|---|---|---|
| `WEBHOOK_SECRET` | *(필수)* | 웹훅 및 관리자 인증 시크릿 |
| `BIND_ADDR` | `0.0.0.0:8787` | TCP 주소 또는 Unix 소켓 경로 (`/`로 시작) |
| `DATABASE_PATH` | `relay.db` | SQLite 데이터베이스 파일 경로 |
| `CHANNEL_URL` | *(없음)* | 등록 시 반환되는 카카오톡 채널 URL |
| `DAILY_LIMIT` | `10000` | 일일 최대 메시지 수 |
| `MONTHLY_LIMIT` | `100000` | 월간 최대 메시지 수 |
| `RUST_LOG` | `info` | 로그 레벨 |

## API

### 공개 API

| 메서드 | 경로 | 설명 |
|---|---|---|
| `POST` | `/register` | 클라이언트 등록, 토큰 + 페어링 코드 반환 |
| `GET` | `/pair-status/{token}` | 토큰의 페어링 상태 확인 |
| `POST` | `/webhook?secret=` | 카카오 오픈빌더 웹훅 엔드포인트 |
| `GET` | `/ws/{token}` | 페어링된 클라이언트용 WebSocket 연결 |
| `GET` | `/health` | 버전 및 커밋 해시 포함 헬스 체크 |

### 관리자 API

| 메서드 | 경로 | 설명 |
|---|---|---|
| `GET` | `/admin/stats?secret=` | 사용량 통계, 활성 세션, 메모리, FD 수 |
| `POST` | `/admin/killswitch?secret=` | 메시지 처리 활성화/비활성화 |

## WebSocket 프로토콜

**서버 → 클라이언트** (수신 메시지):
```json
{"id": "action_id", "text": "사용자 메시지", "user_id": "kakao_user_id"}
```

**클라이언트 → 서버** (응답):
```json
{"id": "action_id", "text": "응답 메시지"}
```

## 배포

프로덕션 배포 가이드는 [DEPLOY.md](DEPLOY.md)를 참고하세요.

`deploy/` 디렉토리에 배포 설정 파일이 준비되어 있습니다:
- `kittykakao.service` — systemd 유닛
- `kittykakao.nginx` — WebSocket 지원 nginx 리버스 프록시
- `env.example` — 환경변수 템플릿

## 개발

```bash
cargo build           # 디버그 빌드
cargo test            # 테스트 실행
cargo clippy          # 린트
RUST_LOG=debug cargo run  # 디버그 로깅으로 실행
```

## 라이선스

Elastic License 2.0. 자세한 내용은 [LICENSE](LICENSE)를 참고하세요.
