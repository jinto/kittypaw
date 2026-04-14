# GitHub Registry Package System

## Goal

GoPaw 패키지 시스템에 GitHub 기반 레지스트리를 연결하여, `gopaw pkg install <id>`로 원격 패키지를 설치할 수 있게 한다.

## Decisions

| 결정 | 내용 |
|------|------|
| 패키지 소스 | `github.com/kittypaw/skills` — sync JS로 재작성하여 업로드 |
| 바이너리 내장 | 안 함. 모든 패키지는 `gopaw pkg install`로 설치 |
| async 전처리 | 불필요. 레지스트리 패키지가 처음부터 sync JS |
| 기본 제공 패키지 | 향후 결정. 현재는 수동 install만 지원 |

## Registry Structure

```
github.com/kittypaw/skills/
├── index.json                # 패키지 카탈로그 ([]RegistryEntry)
├── rss-digest/
│   ├── package.toml
│   ├── main.js               # sync JS (GoPaw goja 호환)
│   └── README.md
├── weather-briefing/
│   └── ...
└── reminder/
    └── ...
```

Base URL: `https://raw.githubusercontent.com/kittypaw/skills/main`

### index.json Format

```json
[
  {
    "id": "rss-digest",
    "name": "RSS Digest",
    "version": "1.0.0",
    "description": "RSS 피드 요약 → 텔레그램",
    "author": "KittyPaw Team",
    "url": "https://raw.githubusercontent.com/kittypaw/skills/main/rss-digest"
  }
]
```

## Code Changes

### 1. `core/registry.go` — DownloadPackage 멀티파일 다운로드

현재: 단일 body를 main.js로 저장 (미완성)
변경: entry.URL을 디렉토리 URL로 취급, 개별 파일 GET

```
DownloadPackage(entry):
  1. GET {entry.URL}/package.toml → tmpDir/package.toml
  2. GET {entry.URL}/main.js     → tmpDir/main.js
  3. GET {entry.URL}/README.md   → tmpDir/README.md  (404 무시)
  4. return tmpDir
```

기본 레지스트리 URL 상수 추가:
```go
const DefaultRegistryURL = "https://raw.githubusercontent.com/kittypaw/skills/main"
```

### 2. `core/config.go` — Registry 설정

Config 구조체에 Registry 섹션 추가:
```toml
[registry]
url = "https://raw.githubusercontent.com/kittypaw/skills/main"
```

기본값은 DefaultRegistryURL. 사설 레지스트리/미러 대응.

### 3. `cmd/gopaw/main.go` — CLI 커맨드

#### `gopaw pkg search [query]`
- 레지스트리 FetchIndex()
- query로 ID/Name/Description 필터링
- 테이블 출력

#### `gopaw pkg install <id>` 확장
- 인자가 디렉토리 경로면 → 기존 로컬 설치
- 아니면 → 레지스트리에서 검색 후 DownloadPackage → Install

#### `gopaw pkg info <id>`
- 설치된 패키지 상세 정보 (meta + config fields + permissions)

### 4. 건드리지 않는 것

- `sandbox/exec.go` — 현행 유지
- `engine/executor.go` — 현행 유지
- `engine/schedule.go` — 현행 유지
- `core/package_manager.go` — 현행 유지 (async 전처리 없음)

## Constraints

- SSRF 방어: 기존 registry.go의 host/scheme 검증 유지
- 다운로드 크기: 파일당 10MB 제한 유지
- 오프라인 캐시: FetchIndex 캐시 폴백 현행 유지

## Non-Goals

- tar.gz 아카이브 지원 (파일 2-3개라 불필요)
- 패키지 버전 관리/업그레이드 (1차에서는 재설치로 처리)
- 바이너리 번들링 / OOBE 자동 설치

## Acceptance Criteria

1. `gopaw pkg search` — 레지스트리 인덱스 출력
2. `gopaw pkg search rss` — 필터링 동작
3. `gopaw pkg install rss-digest` — GitHub에서 다운로드 + 설치
4. `gopaw pkg install ./local-dir` — 기존 로컬 설치 유지
5. `gopaw pkg info rss-digest` — 상세 정보 출력
6. 오프라인 시 캐시된 인덱스 폴백
7. 기존 테스트 깨지지 않음
