# Multi-Workspace Management With Aliases

## Goal

KittyPaw should let each local account register multiple file workspaces and
manage them from both the Web Settings UI and the CLI. File tools should support
human-readable workspace aliases so users can target a specific workspace without
typing absolute paths.

## Existing State

- The store already has a `workspaces` table with `id`, `name`, and `root_path`.
- The server already exposes workspace list/create/delete endpoints.
- The engine already caches all workspace root paths through `Session.AllowedPaths`.
- Live indexing already supports multiple workspaces.
- The current setup flow mainly treats the first configured allowed path as the
  default workspace.

The missing pieces are user-facing management, account-scoped API handling, alias
rules, and clear path resolution semantics.

## Path Semantics

File tool path resolution should use three forms:

- `memo.txt`: relative path inside the account's primary workspace.
- `notes:memo.txt`: path inside the workspace whose alias is `notes`.
- `/Users/jinto/Projects/foo/memo.txt`: absolute path, allowed only if it is
  inside any registered workspace.

The `alias:path` form is the preferred explicit form. Alias names should be
simple slugs: lowercase letters, numbers, `_`, and `-`. The colon separates the
workspace alias from the path. The path after the colon must still be validated
after joining with the workspace root, so `notes:../secret.txt` must be rejected
if it escapes the workspace.

## Workspace Alias Model

Use the existing `workspaces.name` column as the workspace alias/display name.
For user-created workspaces it should be a slug and unique within that account's
database. Because each account has its own DB, account-local uniqueness is
enough.

Existing seeded workspaces may currently have a full path as their `name`. The
implementation should normalize these to a safe default alias when possible:

- Prefer the account ID for the setup-created default workspace.
- Otherwise derive from the directory basename.
- If a derived alias collides, append `-2`, `-3`, etc.

The first workspace by `created_at` remains the primary workspace for MVP. Web
and CLI can show it as "Primary". A later enhancement may add explicit
`set-primary`; the initial implementation should not add a schema field for
primary selection.

## Web Settings

Settings should include a Workspaces section for the logged-in account:

- List workspace alias, root path, and primary marker.
- Add workspace with path and optional alias.
- Remove workspace with confirmation.
- Rename alias if the user needs to change it.

The Web UI must operate on the authenticated/requested account, not a process
global store. Workspace API handlers should resolve the request account and use
that account's store, session, and live indexer.

## CLI

Add `kittypaw workspace` commands:

```text
kittypaw workspace list [--account ACCOUNT]
kittypaw workspace add PATH [--alias ALIAS] [--account ACCOUNT]
kittypaw workspace remove ALIAS_OR_ID [--account ACCOUNT]
kittypaw workspace rename ALIAS_OR_ID NEW_ALIAS [--account ACCOUNT]
```

For mutating commands, the CLI should print the target account and config path
before applying changes:

```text
Account: jinto
Config: /Users/jinto/.kittypaw/accounts/jinto/config.toml
Add workspace:
  alias: notes
  path: /Users/jinto/Documents/notes

Continue? (y/N):
```

No password prompt is required for local CLI mutations. The local OS user can
already access the account files, so a password prompt would add friction without
meaningfully improving security. For scripts, a future `--yes` flag can skip the
confirmation, but only after the account is explicitly resolved and printed.

## Config And Store Synchronization

The DB `workspaces` table is the runtime source of truth for multiple
workspaces. The TOML `sandbox.allowed_paths` list remains the setup/bootstrap
seed source and backward-compatible config surface.

When setup writes a default workspace, it should seed the DB as today. Workspace
changes made through Web Settings or CLI should update the DB and refresh the
session allowed-path cache. Updating TOML for every workspace mutation is not
required for MVP, but the first/default setup path should remain in TOML for
backward compatibility.

## Error Handling

- Duplicate alias: reject with a clear message.
- Duplicate root path: reject with a clear message.
- Invalid alias: explain allowed characters.
- Missing path: reject.
- Non-directory path: reject, or offer create only in setup flows where that
  pattern already exists.
- Removing the primary workspace is allowed. If another workspace exists, the
  next oldest workspace becomes primary. If it is the only workspace, require an
  extra confirmation that file access will become disabled.
- API failures after delete should continue to fail closed by clearing cached
  allowed paths.

## Testing

Add tests for:

- Alias validation and uniqueness.
- `alias:path` resolution, including traversal rejection.
- Relative path resolution to the primary workspace.
- Web API account scoping for list/create/delete.
- CLI mutations showing the selected account before confirmation.
- Existing absolute-path behavior remains unchanged.
- Live indexer and allowed-path cache refresh after add/remove.

## Non-Goals

- Remote multi-user administration.
- Per-subdirectory permission rules beyond existing workspace containment.
- Fine-grained read/write/delete permissions in the first pass.
- Full TOML synchronization for every workspace row.
