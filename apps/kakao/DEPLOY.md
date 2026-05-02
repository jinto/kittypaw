# KittyKakao 배포 메모

## 웹훅 설정

웹훅 설정은 카카오 오픈빌더에서 설정한다. (2026.04.16 현재)

1. 챗봇 관리자 센터에 스킬 메뉴에서 "기본 스킬"을 추가한다.
2. 시나리오에서 "폴백 블록"의 파라미터에 "기본스킬"을 넣는다.
3. 스킬에서 "기본스킬"을 생성하고, 웹훅 URL에 릴레이서버 주소를 넣는다: `https://<relay-host>/webhook?secret=WEBHOOK_SECRET`

## 서버 배포

### 사전 준비

```bash
# cross-compile 도구 (로컬 macOS)
cargo install cross

# fabric (로컬)
pip install fabric
```

### 최초 셋업

```bash
# 서버에 디렉토리, nginx, systemd 설정
fab setup

# 서버에 SSH 접속 후 .env 편집
ssh <your-host>
vi /home/ubuntu/kittykakao/.env
```

### 배포

```bash
fab deploy     # 빌드 → 업로드 → 재시작
fab status     # 서비스 상태 확인
fab logs       # 로그 확인
fab rollback   # 이전 바이너리로 복원
```

### DNS 설정

- A 레코드: 도메인 → 서버 IP
- Cloudflare 사용 시 프록시 모드 ON, WebSockets 활성화 확인

### 검증

```bash
curl https://<your-domain>/health
```
