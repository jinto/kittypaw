# Maintenance — kittypaw-api

운영자가 정기적으로 수행해야 하는 데이터 동기화 / 헬스 체크 작업 목록.

## 정기 작업 (Recurring)

| 데이터 / 작업 | 주기 | 명령 | 비고 |
|---|---|---|---|
| **Wikidata 한국 랜드마크** 갱신 | 주 1회 | `make seed-wikidata` | SPARQL 페이징 + transactional swap. 약 10k row, 2~5분. 실패 시 `--resume`로 재개 가능 |
| **서울교통공사 1~8호선 좌표** 갱신 | 연 1회 또는 노선/역 신규 개통 시 | `make seed-seoul-metro` | data.go.kr 15099316 CSV 수동 다운로드 → `testdata/seoul_metro.csv` 갱신 후 실행 |
| **별칭 사전 (alias_overrides)** 큐레이션 | 월 1회 (corpus 미스 패턴 검토) | 새 마이그레이션 추가 | corpus 결과 보고 후 50개 + α |
| **resolve precision 게이트** 측정 | 6개월마다 | `make benchmark-resolve` | precision < 0.85 → alias 보강 또는 fuzzy threshold 조정 |
| **행안부 도로명주소 (PR-2 머지 후)** | 일 1회 | `make seed-juso` | EPSG:5179 → WGS84 변환. 수백만 row, 30분~1시간. 차분 갱신 권장 |

### cron 예시 (Linux)

```cron
# 매주 일요일 03:00 — Wikidata 갱신
0 3 * * 0 cd /opt/kittypaw-api && make seed-wikidata >> /var/log/kittypaw/seed-wikidata.log 2>&1

# 매년 1월 1일 04:00 — 서울교통공사 (CSV 사전 갱신 필수, 자동화 불가)
0 4 1 1 * cd /opt/kittypaw-api && make seed-seoul-metro >> /var/log/kittypaw/seed-seoul-metro.log 2>&1

# 분기 1회 — resolve precision 측정
0 5 1 */3 * cd /opt/kittypaw-api && make benchmark-resolve >> /var/log/kittypaw/benchmark.log 2>&1
```

## 1회 사전 작업 (One-time)

PR-1 / Plan 6 도입 시 반드시 1회 수행:

- [ ] **PostgreSQL `pg_trgm` extension 설치** (superuser 권한)

  ```sql
  CREATE EXTENSION pg_trgm;
  ```

- [ ] **AWS RDS 매니지드 환경**: 파라미터 그룹에 `rds.extensions = 'pg_trgm'` 추가 후 재부팅

- [ ] `make migrate` (또는 `migrate -path migrations -database "$DATABASE_URL" up`) 실행하여 003/004 마이그레이션 적용

- [ ] `make seed-wikidata` 첫 임포트 (~10k row)
- [ ] data.go.kr 15099316에서 CSV 다운로드 → `testdata/seoul_metro.csv` 저장
- [ ] `make seed-seoul-metro` 첫 임포트 (~280 row)

## 데이터 다운로드 절차

### Wikidata SPARQL — 자동
- 엔드포인트: `query.wikidata.org/sparql` (인증 없음)
- 라이센스: **CC0 1.0** (Public Domain — 출처 표기·재배포 의무 없음)
- 실패 시 체크포인트 복구: `go run ./cmd/seed-wikidata --resume`

### 서울교통공사 1~8호선 좌표 — 수동
1. 브라우저로 https://www.data.go.kr/data/15099316/fileData.do 접속
2. 우측 상단 "다운로드" 클릭 (data.go.kr 회원가입 필수)
3. 받은 파일을 `testdata/seoul_metro.csv`로 저장 (UTF-8)
4. `make seed-seoul-metro` 실행

CSV 컬럼: `연번, 호선, 역명, 구분, 위도, 경도`

라이센스: **이용허락범위 제한 없음** (data.go.kr 표기). 영리화 진입 직전에는 서울교통공사에 서면 확인 권장.

### 행안부 도로명주소 DB — PR-2 (예정)
- https://business.juso.go.kr 시도별 분할 다운로드
- EPSG:5179 (UTM-K) → WGS84 변환 필요
- 일간 차분 갱신 (생성/변경/삭제)
- 라이센스: **이용허락범위 제한 없음**

## 모니터링 & 헬스 체크

- [ ] `places` 테이블 row 수 추세 (Wikidata 임포트 후 ~10k 유지, 급감 시 알림)
- [ ] `alias_overrides` row 수 (50 이상 유지)
- [ ] `make benchmark-resolve` precision 추세 (분기별 측정)
- [ ] PostgreSQL 백업 사이즈 — PR-2 행안부 진입 시 급증 (~수 GB)
- [ ] cron 작업 로그 파일 — 7일 이상 미갱신 시 알림 (cron 자체 실패)

### cron 실패 알림 (TODO — follow-up issue)

현재는 운영자가 로그를 수동 확인. Slack/email 알림 채널 도입은 별도 follow-up 이슈로 추적.

## 라이센스 추적

| Source 식별자 | 라이센스 | 출처 표기 의무 | 영리 사용 | 변경/통합 |
|---|---|---|---|---|
| `wikidata` | CC0 1.0 | 없음 | 가능 | 가능 |
| `kogl_seoul_metro` | "이용허락범위 제한 없음" (data.go.kr 15099316) | 권장 | 가능 (확정 위해 영리화 직전 서면 문의 권장) | 가능 |
| `kittypaw_alias` | 자체 큐레이션 | — | — | — |
| `kogl_juso` (PR-2) | "이용허락범위 제한 없음" (data.go.kr 15050417) | 권장 | 가능 | 가능 |

응답에는 `source` 필드만 노출, `license` 필드는 포함하지 않음 (kittypaw 일반 약관에서 출처별 라이센스를 흡수). 공급자 정책 변경 모니터링은 데이터셋 페이지 분기별 1회 확인 권장.

## 관련 문서

- 설계 결정: [`.claude/plans/geo-address-coords.md`](../.claude/plans/geo-address-coords.md) (v8)
- 태스크 목록: [`TASKS.md`](../TASKS.md) Plan 6
- API 사용법: [`README.md`](../README.md) `/v1/geo/resolve` 섹션
