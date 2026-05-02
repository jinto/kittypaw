# KittyKakao 배포 메모

## 웹훅 설정

웹훅 설정은 카카오 오픈빌더에서 설정한다. (2026.04.16 현재)

1. 챗봇 관리자 센터에 스킬 메뉴에서 "기본 스킬"을 추가한다.
2. 시나리오에서 "폴백 블록"의 파라미터에 "기본스킬"을 넣는다.
3. 스킬에서 "기본스킬"을 생성하고, 웹훅 URL에 릴레이서버 주소를 넣는다: `https://<relay-host>/webhook?secret=WEBHOOK_SECRET`

## 서버 배포

### 사전 준비

```bash
# Go toolchain (로컬 macOS)
go version

# fabric (로컬)
pip install fabric
```

### 최초 셋업

```bash
# 서버에 디렉토리, nginx, systemd 설정
fab setup

# 서버에 SSH 접속 후 .env 편집
ssh <your-host>
vi <REMOTE_DIR>/.env
```

### Fabric 없이 설치할 수 있나?

가능하다. 이 앱은 단일 Go 바이너리와 세 개의 설정 파일만 필요하다.
Fabric은 필수 런타임 의존성이 아니라 다음 작업을 묶어주는 편의 래퍼다.

1. 로컬에서 `GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build`로 바이너리 생성
2. 서버에 `{{REMOTE_DIR}}` 생성
3. `deploy/kittykakao.service`, `deploy/kittykakao.nginx`, `deploy/env.example` 업로드
4. `{{DOMAIN}}`, `{{REMOTE_DIR}}`, `{{SERVICE_USER}}`, `{{SERVICE_GROUP}}` 치환
5. systemd/nginx 위치로 복사, `systemctl daemon-reload`, `systemctl enable`, `nginx -t`, reload
6. 바이너리 업로드, 실행 권한 부여, 서비스 restart, `/health` smoke

수동 설치도 같은 명령을 `ssh`/`scp`로 실행하면 된다. 다만 최초 설치에서는
root 권한 복사, nginx 검증, systemd enable/restart, 이전 바이너리 백업 순서를
빼먹기 쉽기 때문에 현재 `fab setup`/`fab deploy`를 유지한다. 배포 자동화를 더
가볍게 만들고 싶다면 Fabric을 제거하기보다 같은 절차를 `deploy/install.sh`와
`deploy/deploy.sh`로 옮기면 된다.

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
