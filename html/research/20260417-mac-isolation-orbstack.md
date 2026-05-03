---
name: Mac Multi-Tenant Isolation via OrbStack
date: 2026-04-17
status: decided
decision: α1 — OrbStack as Mac prerequisite, single Linux cgroup path
---

# Mac Multi-Tenant Isolation — OrbStack 결정 연구

> Historical research snapshot. This document captures the code and service
> shape observed at the time of the research; use repository README,
> ARCHITECTURE.md, and app README/DEPLOY docs for the current live shape.

## 문제

Per-Tenant Isolation 스펙 v5 는 Mac / Linux 격리 수준이 비대칭이었다:

- Linux: process + cgroup v2 `memory.max` / `cpu.max` — **hard enforcement**
- Mac: process boundary only — **soft**, cgroup 없음

사용자 결정: Mac multi-tenant 지원 필요 + "둘의 아키텍처가 다른 것이 마음에 들지 않음".

## 왜 Mac 에 hard isolation 이 어려운가

OS 프리미티브 부재:

| 기술 | Mac 지원 | 비고 |
|-----|---------|------|
| `cgroup v2` | ❌ | Linux 전용 커널 기능 |
| `RLIMIT_AS` (setrlimit 가상 메모리) | ❌ | **호출은 성공 / OS 가 무시** (Python bug #34602 검증) |
| `sandbox-exec` | 🟡 | 파일/네트워크만. CPU/메모리 한계 없음. Apple 공식 비권장 |
| `launchd` resource limits | ❌ | plist `HardResourceLimits` 는 setrlimit wrap → 메모리 cap 무시 |
| `Virtualization.framework` | ✅ | Apple Silicon 전용, VM 수준 (heavyweight, tenant 당 100-200MB) |
| `taskinfo` / `proc_pidinfo` | 🟡 | 관찰만 가능, enforcement 없음 |

결론: **Mac 에서 커널 수준 hard cap 은 불가**. Apple 이 이런 기능을 제공하지 않음 — Mac App Store sandbox 를 제외하고.

## 고려된 옵션

### S1. Linux VM on Mac (OrbStack / Colima / Lima) — **선정**

Mac 에서 경량 Linux VM 을 돌리고, KittyPaw 는 Linux 바이너리로 VM 안에서 실행.

- **OrbStack**: Apple Silicon 네이티브 lightweight VM, Docker Desktop 대체재. 무료 (personal), 상용 license 필요 (team)
- **Colima** / **Lima**: 오픈소스 대안, QEMU + lima 기반
- 공통: Linux 커널을 그대로 제공 → `cgroup v2` 를 Mac 위에서도 사용

**장점**:
- 코드 경로 단일 (Linux-only isolation 로직)
- cgroup / tableflip / Pdeathsig 등 Linux 전용 기능 Mac 에서도 동작
- 배포 이미지 단일 (Linux 바이너리 하나)
- "Mac 지원 = Linux runtime provision" 로 재정의

**단점**:
- Mac 유저에게 OrbStack 설치 요구 (Docker Desktop 경험 수준)
- VM startup 오버헤드 (약 1-3 초, session-wide)
- OrbStack = personal 무료지만 team 상용 라이선스 있음

### S2. Virtualization.framework per-tenant

각 tenant = Apple VZ.framework 로 띄운 lightweight Linux VM.

- **기각 사유**: Apple Silicon 전용, VM 오케스트레이션 복잡도 폭증, tenant 당 100-200MB 고정 비용. 과공학.

### S3. Soft watchdog (RSS polling)

Linux 커널 없이 userspace 에서 RSS 모니터링 + OOM 시 supervisor restart.

- **장점**: OrbStack 불필요
- **단점**: 평균 격리만 가능. Runaway tenant 가 1 초 내 메모리 폭증 시 못 잡음. Hard cap 아님
- **v5 에서 사용된 프레이밍** — 사용자가 asymmetry 문제로 거부

### S4. 각 tenant = Docker container

`docker run` 으로 tenant 별 컨테이너 spawn. Docker API 를 supervisor 가 호출.

- **기각 사유**: Docker daemon 의존, orchestration 비용 (docker API latency, image pull, network 설정), KittyPaw supervisor 가 docker-as-service 가 됨. Heavyweight.

### S5. sandbox-exec + setrlimit 조합

Mac native primitive 조합.

- **기각 사유**: CPU/메모리 cap 불가 (위 표). 파일/네트워크 ACL 만 가능해서 Layer 2 threat 해결 못함.

## OrbStack 과 Docker 의 구분 — 혼동 해소

사용자 초기 질문: "OrbStack 설치하면 각 데몬을 docker 로 격리하겠다는 건가?"

**답**: NO. OrbStack 과 Docker 와 cgroup 은 다른 층.

```
층 1: Linux 커널 (cgroup, namespace 등 원시 격리 기능 제공)
   ↓ Mac 은 이 층 자체가 없음
층 2: 런타임 환경 (Linux 커널을 Mac 에 제공하는 도구)
   ├─ OrbStack: 이 목적만. 결과는 "Linux 커널이 쓸 수 있게 됨"
   ├─ Docker Desktop: OrbStack + 컨테이너 레이어 추가
   └─ Colima / Lima: OrbStack 유사
층 3: 격리 메커니즘 (커널 기능을 사용하는 방식)
   ├─ bare process + cgroup 배정: 컨테이너 없이 직접
   ├─ Docker container: cgroup + namespace + overlayfs 묶음
   └─ systemd slice: cgroup + unit 파일 관리
```

**OrbStack = 층 2 만 제공**. 그 위에서 격리는 별도 선택.

**KittyPaw 의 선택**: 층 3 에서 **bare process + cgroup** (Docker 미사용). 즉:

```
Mac:
  OrbStack (Linux kernel)
    └─ kittypawd-supervisor (Linux 바이너리, bare process)
         └─ fork → tenant child → cgroup attach
             (Docker 컨테이너 아님. 그냥 Linux 프로세스)

Linux:
  host (Linux kernel 직접)
    └─ kittypawd-supervisor
         └─ fork → tenant child → cgroup attach
             (Mac 과 동일 코드)
```

컨테이너 배포 이미지 (`docker run kittypaw:latest`) 도 **가능**하지만, 그건 **KittyPaw 자체를 컨테이너로 배포**하는 옵션이지, **tenant 격리를 위한 컨테이너**가 아니다. tenant 격리는 컨테이너 내부에서 cgroup delegation 으로 처리.

## 결정 — α1 (OrbStack prerequisite + Linux cgroup 단일 path)

| 항목 | 내용 |
|-----|------|
| Mac 지원 방식 | OrbStack 설치 필수, Linux 바이너리 실행 |
| 코드 경로 | Linux-only isolation (OS 분기 최소) |
| tenant 격리 기술 | cgroup v2 (bare process, 컨테이너 아님) |
| 배포 이미지 | Linux 바이너리 (추후 Docker 이미지도 배포 옵션) |
| "KittyPaw is Mac-native" 포기 | ✅ — prerequisite 명시 |

## 사용자 서사

**이전 (v5)**: "KittyPaw 는 Linux 와 Mac 에서 각각 설치 가능. Mac 에서는 격리가 약합니다."

**이후 (v6)**: "KittyPaw 는 Linux 런타임 위에서 실행됩니다. Mac 유저는 OrbStack (또는 Colima) 을 설치하면 완전히 동일한 환경이 제공됩니다."

## 영향 범위

- 스펙 v5 → v6 변경
- README / docs: Mac 설치 가이드에 OrbStack 설치 단계 추가
- Platform Matrix: "Mac = polling best-effort" → "Mac = OrbStack 내부에서 Linux 와 동일"
- AC 2-Mac (userspace memory balloon) 삭제 — cgroup 있으므로 AC 2 에 통합
- CI matrix: `macos-14-large` + OrbStack pre-install step 추가, 또는 Linux matrix 로 일원화 (Mac 은 OrbStack 자체 테스트만)

## 참고

- [OrbStack 공식](https://orbstack.dev/)
- [Colima GitHub](https://github.com/abiosoft/colima)
- [Lima GitHub](https://github.com/lima-vm/lima)
- [Python bug #34602 — RLIMIT_AS ignored on macOS](https://bugs.python.org/issue34602)
- [Virtualization.framework docs](https://developer.apple.com/documentation/virtualization)
