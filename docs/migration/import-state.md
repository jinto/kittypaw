# Monorepo Import State

Date: 2026-05-02

## Imported Repositories

| Source | Source HEAD | Target path | Monorepo merge commit |
| --- | --- | --- | --- |
| `../kittypaw` | `fdd6dda3dc49a7f5edef0ed87918fff13d46aa9b` | `apps/kittypaw` | `4c12fa8` |
| `../kittyapi` | `4b9cd1bbcdf1f3dff8d025ac5021d81aff9b0359` | `services/api` | `1e24c6d` |
| `../kittychat` | `0cb68b996632cbd33c3b3319ca486a4f60b7198e` | `services/chat` | `22a85de` |
| `../kittykakao` | `4fa65ffc1e8dd28b6b71e02c8f1aef73ea7efef7` | `services/kakao` | `34ac840` |

## Import Notes

- Each repository was imported through a temporary local clone rewritten with
  `git filter-repo --to-subdirectory-filter`.
- Original repositories were not mutated.
- Imported commit messages were rewritten to Conventional Commits scope form:
  `type(module[/subscope]): subject`.
- Existing `kittypaw` release tags were namespaced to `kittypaw/v*`.
- `../kittykakao` had uncommitted working tree changes at import time. Per user
  decision, only committed `main` history was imported.
- Temporary clones live under `tmp/import/`, which is ignored by git.

## Verification

Passed:

- `make contracts-check`
- `go test ./services/chat/... -count=1`
- `go test ./services/api/... -short -count=1`
- `cargo test --manifest-path services/kakao/Cargo.toml`
- `go test ./apps/kittypaw/... -short -count=1`
