# Team Space Membership and Account Scheduler Design

## Summary

KittyPaw currently models the cross-account coordinator as a "family" account in
parts of the code and docs, even though the actual product concept is a shared
team space. This design replaces the metaphor with a team-space model, adds
explicit team-space membership, and makes scheduled skill execution account
aware.

The persisted compatibility flag remains `is_shared = true` for now. The new
conceptual and configuration surface is `[team_space]`.

## Goals

- Introduce a team-space membership list owned by the team-space account.
- Deny team-space access when a team space has no explicit members.
- Let team-space members read all shareable team-space user data.
- Restrict team-space fanout to explicit members.
- Run schedulers per account so team-space scheduled skills execute in the
  team-space session and can fan out to members.
- Remove user-visible "family" terminology from the active code and docs touched
  by this work.

## Non-Goals

- Do not add roles such as owner/admin/member.
- Do not implement per-feature or per-file permissions.
- Do not treat `[share.<account>]` as membership.
- Do not expose secrets, config files, local auth files, database files, or other
  operational account internals through cross-account reads.
- Do not change the persisted `is_shared` key in this iteration.

## Terminology

- **Personal account:** An account that owns channels and receives direct user
  messages.
- **Team space:** An account with `is_shared = true`. It owns shared automation,
  memory, workspace data, and member fanout. It must not own chat channels.
- **Member:** A personal account ID listed in the team-space account's
  `[team_space].members` array.

## Configuration

Add a `TeamSpaceConfig` to `core.Config`:

```toml
is_shared = true

[team_space]
members = ["alice", "bob"]
```

Expected Go shape:

```go
type Config struct {
    IsShared  bool            `toml:"is_shared"`
    IsFamily  bool            `toml:"-"`
    TeamSpace TeamSpaceConfig `toml:"team_space"`
}

type TeamSpaceConfig struct {
    Members []string `toml:"members"`
}
```

`IsFamily` remains only as a compatibility bridge while old tests and old
in-memory construction are migrated. New code should use an
`IsTeamSpaceAccount()` helper that currently returns `IsShared || IsFamily`.

Membership validation:

- Every member ID must pass `ValidateAccountID`.
- A team space must not list itself as a member.
- At server startup, reload, and hot-add validation, member IDs must resolve to
  existing non-team-space accounts.
- Missing or empty `[team_space].members` means no members. Access is deny-all.

Removing a personal account should also remove that account ID from every
team-space membership list, similar to the existing shared config scrub path.
This must update both disk-side config and the running server's in-memory
account/session config so a stale member cannot block live fanout broadcasts.

## Cross-Account Reads

`Share.read(accountID, path)` remains the skill API for cross-account reads. The
authorization rule changes:

1. The target `accountID` must be a team-space account.
2. The caller account must be listed in the target team space's members.
3. The requested path must stay inside the team-space shareable data surface.

The old `[share.<account>] read = [...]` allowlist no longer grants team-space
access. It can remain parsed for migration compatibility, but it should not be
consulted for the new team-space read path.

Shareable data surface:

- `memory/...` resolves under the team-space account's `memory/` directory.
- `workspace/<alias>/...` resolves under the matching configured workspace root
  for the team-space account.

Rejected surfaces:

- `config.toml`
- `account.toml`
- `secrets.json`
- SQLite or store files
- hidden operational directories
- absolute paths
- traversal paths
- symlink or hardlink escapes

The existing `openNoFollow`, symlink boundary, and hardlink checks should remain
in the single validation chokepoint.

Error strings should use "team space" rather than "family account". Unknown
target and non-team-space target should still collapse to the same external
error so account ID probing does not reveal which IDs exist.

## Fanout

`Fanout.send(accountID, payload)` and `Fanout.broadcast(payload)` remain the
skill API. Only team-space sessions expose `Fanout`.

Rules:

- `Fanout.send` rejects targets that are not members of the source team space.
- `Fanout.broadcast` sends only to the source team space's members.
- Team-space self-targets remain rejected.
- Unknown member IDs should be rejected during config validation, so runtime
  fanout should normally only see valid registered accounts.

Rename internal event terminology from family push to team-space push:

- `EventFamilyPush` becomes `EventTeamSpacePush`.
- The event value becomes `team_space.push`.
- Dispatcher and retry helper names should use team-space terminology.
- Dispatch should still accept the pre-rename wire literal for queued or
  persisted events, but new events must emit `team_space.push`.

This event is internal to the local server event channel; skills continue to use
`Fanout`, not the event type directly.

## Account-Aware Scheduling

The server currently constructs one scheduler for the default account session.
That prevents non-default personal accounts and team spaces from running their
own scheduled skills.

Replace the single scheduler field with an account-keyed scheduler manager:

```go
type AccountSchedulers struct {
    schedulers map[string]*engine.Scheduler
}
```

Expected behavior:

- `server.NewWithServerConfig` creates one scheduler per account, using that
  account's `engine.Session` and `PackageManager`.
- `ListenAndServe` starts every scheduler with the server scheduler context.
- `AddAccount` registers and starts a scheduler for the new account if the
  server is already running.
- `RemoveAccount` stops and removes that account's scheduler.
- Replacing a scheduler for a running account stops the old scheduler before
  starting the replacement so the account never has two active scheduler loops.
- Shutdown stops and waits for all account schedulers.

Each scheduler runs only against its own account store and package manager.
Team-space scheduled skills therefore execute with the team-space session, see
`Fanout`, do not see `Share`, and can send messages only to team-space members.
Personal-account scheduled skills keep the existing behavior but run for every
personal account, not only the default account.

## Naming Migration

Use these names for new or touched code:

- `TeamSpaceConfig`
- `IsTeamSpaceAccount`
- `ValidateTeamSpaceAccounts`
- `ValidateTeamSpaceMemberships`
- `EventTeamSpacePush`
- `deliverTeamSpacePush`
- `enqueueTeamSpacePushForRetry`
- `resolveTeamSpacePushChannel`

Keep persisted `is_shared` and CLI `--is-shared` in this iteration. User-facing
docs may call the concept "team space" while explaining that the current config
flag is `is_shared`.

## Tests

Core tests:

- TOML parses `[team_space] members = [...]`.
- Missing members defaults to deny-all.
- Invalid member IDs are rejected.
- A team space cannot list itself as a member.
- Unknown member IDs are rejected during full account validation.
- Shared accounts with channels remain rejected.

Read tests:

- Member can `Share.read(team, "memory/file")`.
- Non-member cannot read the same file.
- `[share.<account>]` alone does not grant access.
- Member can read `workspace/<alias>/file` under a configured team-space
  workspace root.
- Traversal, absolute path, symlink escape, hardlink escape, and operational
  file requests are rejected.

Fanout tests:

- Team-space `Fanout.send` succeeds for a member.
- Team-space `Fanout.send` rejects a non-member.
- `Fanout.broadcast` sends only to members.
- Personal accounts still do not see the `Fanout` global.

Scheduler tests:

- `NewWithServerConfig` creates a scheduler for every account.
- Starting the server starts every account scheduler.
- Hot-added accounts get schedulers.
- Removed accounts stop their schedulers.
- Removed personal accounts are scrubbed from live team-space membership before
  subsequent broadcasts.
- Scheduler replacement stops the old scheduler before starting the new one.
- A team-space scheduled package runs in a session with `Fanout` available.

Docs and terminology tests:

- CLI and skill metadata no longer expose "family account".
- Root command still does not expose a `family` command.
- Active README and website status copy use "Team space".

## Rollout

The first implementation can be a breaking change for old shared/family test
fixtures that had no explicit members. That is intentional. Operators must add
`[team_space].members` to enable cross-account reads or fanout.

Existing configs with `[share.<account>]` should continue to parse, but the
field becomes legacy for team-space access. A later migration can remove it once
there is no active use.
