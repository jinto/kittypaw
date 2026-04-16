# KittyAPI 배포 메모

## 사전 준비

```bash
# fabric (로컬)
pip install fabric

# golang-migrate CLI (서버) — https://github.com/golang-migrate/migrate
curl -L https://github.com/golang-migrate/migrate/releases/download/v4.18.2/migrate.linux-amd64.tar.gz | tar xz
sudo mv migrate /usr/local/bin/
```

## 최초 셋업

```bash
# 서버에 디렉토리, nginx, systemd 설정
DEPLOY_DOMAIN=api.kittypaw.app fab setup

# 서버에 SSH 접속 후 .env 편집
ssh second
vi /home/jinto/kittyapi/.env

# PostgreSQL DB 생성
sudo -u postgres createdb kittypaw_api
sudo -u postgres createuser kittypaw

# 마이그레이션 실행
fab migrate
```

## 배포

```bash
fab deploy     # 빌드 → 업로드 → 재시작
fab status     # 서비스 상태 확인
fab logs       # 로그 확인
fab rollback   # 이전 바이너리로 복원
fab migrate    # DB 마이그레이션
```

## DNS 설정

- A 레코드: api.kittypaw.app → 서버 IP
- Cloudflare 사용 시 프록시 모드 ON

## 검증

```bash
curl https://api.kittypaw.app/health
```
