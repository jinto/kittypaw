# Team Space Membership and Account Scheduler Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the active family-account metaphor with explicit team-space membership, member-scoped shared reads and fanout, and account-aware scheduled execution.

**Architecture:** Keep persisted `is_shared = true` for compatibility, add `[team_space].members` as the authority for team-space access, and preserve the existing `Share.read` / `Fanout.*` skill APIs. Server startup builds one account-scoped scheduler per loaded account; hot-add and remove keep the scheduler set in sync with `AccountRouter`, `AccountRegistry`, and `accountDeps`.

**Tech Stack:** Go, BurntSushi TOML, goja sandbox, existing `core`, `engine`, `server`, and `cli` packages, standard `go test`.

---

## File Structure

- Modify `apps/kittypaw/core/config.go`: add `TeamSpaceConfig`, `Config.TeamSpace`, `IsTeamSpaceAccount`, and membership helpers.
- Modify `apps/kittypaw/core/config_test.go`: add TOML parsing/default tests for `[team_space]`.
- Modify `apps/kittypaw/core/account.go`: replace/alias family validation with team-space account and membership validation.
- Modify `apps/kittypaw/core/account_test.go`: add team-space membership validation tests and update family validation expectations.
- Modify `apps/kittypaw/core/share.go`: change cross-account read validation from per-path `[share.*]` allowlists to member + shareable-surface validation.
- Modify `apps/kittypaw/core/share_test.go`: add member, non-member, legacy share-only, memory, workspace, and operational-file tests.
- Modify `apps/kittypaw/engine/share.go`: update target role checks and error wording to team-space terminology.
- Modify `apps/kittypaw/engine/share_test.go`: update fixtures and expectations for team-space membership.
- Modify `apps/kittypaw/core/types.go`: rename internal push event constant to `EventTeamSpacePush = "team_space.push"` with a compatibility alias if needed during migration.
- Modify `apps/kittypaw/core/fanout.go`: restrict fanout targets to team-space members and emit team-space push events.
- Modify `apps/kittypaw/core/fanout_test.go`: update event-name and membership tests.
- Modify `apps/kittypaw/server/server.go`: replace single scheduler with account scheduler manager; rename family push dispatch helpers to team-space push helpers.
- Create `apps/kittypaw/server/account_schedulers.go`: account-keyed scheduler lifecycle manager.
- Create `apps/kittypaw/server/account_schedulers_test.go`: scheduler manager unit tests.
- Modify `apps/kittypaw/server/account_deps.go` and `apps/kittypaw/server/account_config.go`: use `IsTeamSpaceAccount` and construct member-scoped fanout.
- Modify `apps/kittypaw/server/admin.go`: start/stop schedulers on hot-add/remove and update terminology.
- Modify `apps/kittypaw/server/account_session_test.go` and `apps/kittypaw/server/family_push_test.go`: update to team-space fixtures and add membership behavior.
- Modify `apps/kittypaw/cli/cmd_account.go` and `apps/kittypaw/cli/cmd_account_test.go`: scrub removed account IDs from `[team_space].members`.
- Modify active docs and metadata touched by the feature: `apps/kittypaw/core/skillmeta.go`, `apps/kittypaw/README.md`, `html/index.html`, `html/en/index.html`, `html/ja/index.html`.

---

### Task 1: Add Team-Space Config and Membership Validation

**Files:**
- Modify: `apps/kittypaw/core/config.go`
- Modify: `apps/kittypaw/core/config_test.go`
- Modify: `apps/kittypaw/core/account.go`
- Modify: `apps/kittypaw/core/account_test.go`

- [ ] **Step 1: Write failing config parsing tests**

Add these tests to `apps/kittypaw/core/config_test.go` near the existing config shape tests:

```go
func TestTeamSpaceConfigParsing(t *testing.T) {
	tomlContent := `
is_shared = true

[team_space]
members = ["alice", "bob"]
`
	var cfg Config
	if _, err := toml.Decode(tomlContent, &cfg); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !cfg.IsTeamSpaceAccount() {
		t.Fatal("is_shared=true must mark account as team space")
	}
	if got := cfg.TeamSpace.Members; len(got) != 2 || got[0] != "alice" || got[1] != "bob" {
		t.Fatalf("TeamSpace.Members = %#v, want alice,bob", got)
	}
}

func TestTeamSpaceConfigDefaultsDenyAll(t *testing.T) {
	var cfg Config
	if _, err := toml.Decode(`is_shared = true`, &cfg); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !cfg.IsTeamSpaceAccount() {
		t.Fatal("is_shared=true must mark account as team space")
	}
	if len(cfg.TeamSpace.Members) != 0 {
		t.Fatalf("missing [team_space].members must default empty, got %#v", cfg.TeamSpace.Members)
	}
	if cfg.TeamSpaceHasMember("alice") {
		t.Fatal("empty team-space members must deny all accounts")
	}
}

func TestLegacyShareParsingStillLoads(t *testing.T) {
	tomlContent := `
is_shared = true

[share.alice]
read = ["memory/weather.json"]
`
	var cfg Config
	if _, err := toml.Decode(tomlContent, &cfg); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(cfg.Share) != 1 || len(cfg.Share["alice"].Read) != 1 {
		t.Fatalf("legacy Share did not parse: %#v", cfg.Share)
	}
	if cfg.TeamSpaceHasMember("alice") {
		t.Fatal("[share.alice] must not imply team-space membership")
	}
}
```

- [ ] **Step 2: Run config tests and verify RED**

Run from `apps/kittypaw`:

```bash
go test ./core -run 'TestTeamSpaceConfig|TestLegacyShareParsingStillLoads'
```

Expected: FAIL because `Config.TeamSpace`, `IsTeamSpaceAccount`, and `TeamSpaceHasMember` do not exist.

- [ ] **Step 3: Implement config types and helpers**

In `apps/kittypaw/core/config.go`, add `TeamSpace` beside `IsShared`:

```go
	// IsShared marks an account as a team-space/coordinator account. Team
	// spaces run scheduled skills for explicit members and fanout to member
	// accounts, but MUST NOT own chat channels.
	IsShared  bool            `toml:"is_shared"`
	IsFamily  bool            `toml:"-"`
	TeamSpace TeamSpaceConfig `toml:"team_space"`

	// Share is the legacy per-path allowlist. It still parses for old configs,
	// but team-space membership is owned by TeamSpace.Members.
	Share map[string]ShareConfig `toml:"share"`
```

Add this type after `ShareConfig`:

```go
// TeamSpaceConfig controls which personal accounts can use a team-space
// account. An empty Members list is deny-all.
type TeamSpaceConfig struct {
	Members []string `toml:"members"`
}
```

Replace the old `IsSharedAccount` implementation block with:

```go
func (c *Config) IsTeamSpaceAccount() bool {
	return c != nil && (c.IsShared || c.IsFamily)
}

func (c *Config) IsSharedAccount() bool {
	return c.IsTeamSpaceAccount()
}

func (c *Config) TeamSpaceHasMember(accountID string) bool {
	if c == nil || !c.IsTeamSpaceAccount() {
		return false
	}
	for _, member := range c.TeamSpace.Members {
		if member == accountID {
			return true
		}
	}
	return false
}
```

Keep `NormalizeRuntimeFields` setting `IsFamily = true` when `IsShared` is true for compatibility.

- [ ] **Step 4: Run config tests and verify GREEN**

Run:

```bash
go test ./core -run 'TestTeamSpaceConfig|TestLegacyShareParsingStillLoads'
```

Expected: PASS.

- [ ] **Step 5: Write failing membership validation tests**

Add these tests to `apps/kittypaw/core/account_test.go` near the existing shared-account validation tests:

```go
func TestValidateTeamSpaceAccounts_RejectsChannels(t *testing.T) {
	accounts := []*Account{
		{ID: "alice", Config: &Config{}},
		{ID: "team", Config: &Config{
			IsShared: true,
			Channels: []ChannelConfig{{ChannelType: ChannelTelegram, Token: "x"}},
		}},
	}
	err := ValidateTeamSpaceAccounts(accounts)
	if err == nil {
		t.Fatal("expected team-space-with-channels to error")
	}
	if !strings.Contains(err.Error(), "team") || !strings.Contains(err.Error(), "telegram") {
		t.Errorf("error should cite account id and channel type: %q", err.Error())
	}
}

func TestValidateTeamSpaceMemberships(t *testing.T) {
	accounts := []*Account{
		{ID: "team", Config: &Config{IsShared: true, TeamSpace: TeamSpaceConfig{Members: []string{"alice", "bob"}}}},
		{ID: "alice", Config: &Config{}},
		{ID: "bob", Config: &Config{}},
	}
	if err := ValidateTeamSpaceMemberships(accounts); err != nil {
		t.Fatalf("valid members rejected: %v", err)
	}
}

func TestValidateTeamSpaceMemberships_RejectsUnknownMember(t *testing.T) {
	accounts := []*Account{
		{ID: "team", Config: &Config{IsShared: true, TeamSpace: TeamSpaceConfig{Members: []string{"alice", "ghost"}}}},
		{ID: "alice", Config: &Config{}},
	}
	err := ValidateTeamSpaceMemberships(accounts)
	if err == nil {
		t.Fatal("expected unknown member error")
	}
	if !strings.Contains(err.Error(), "team") || !strings.Contains(err.Error(), "ghost") {
		t.Errorf("error should cite team and missing member: %q", err.Error())
	}
}

func TestValidateTeamSpaceMemberships_RejectsSelfMember(t *testing.T) {
	accounts := []*Account{
		{ID: "team", Config: &Config{IsShared: true, TeamSpace: TeamSpaceConfig{Members: []string{"team"}}}},
	}
	err := ValidateTeamSpaceMemberships(accounts)
	if err == nil {
		t.Fatal("expected self-member error")
	}
	if !strings.Contains(err.Error(), "must not list itself") {
		t.Errorf("error should cite self membership: %q", err.Error())
	}
}
```

- [ ] **Step 6: Run membership tests and verify RED**

Run:

```bash
go test ./core -run 'TestValidateTeamSpace'
```

Expected: FAIL because validation functions do not exist.

- [ ] **Step 7: Implement team-space validation**

In `apps/kittypaw/core/account.go`, replace `ValidateFamilyAccounts` with a new function and leave a compatibility wrapper:

```go
// ValidateTeamSpaceAccounts fails fast when a team-space account declares
// channel configs. Team spaces are coordinators, not channel owners.
func ValidateTeamSpaceAccounts(accounts []*Account) error {
	var offenders []string
	for _, t := range accounts {
		if t == nil || t.Config == nil || !t.Config.IsTeamSpaceAccount() {
			continue
		}
		if len(t.Config.Channels) == 0 {
			continue
		}
		types := make([]string, 0, len(t.Config.Channels))
		for _, ch := range t.Config.Channels {
			types = append(types, string(ch.ChannelType))
		}
		offenders = append(offenders, fmt.Sprintf("%s:%v", t.ID, types))
	}
	if len(offenders) == 0 {
		return nil
	}
	return fmt.Errorf("team space must not declare channels: %v", offenders)
}

func ValidateFamilyAccounts(accounts []*Account) error {
	return ValidateTeamSpaceAccounts(accounts)
}
```

Add membership validation below it:

```go
// ValidateTeamSpaceMemberships verifies that every configured team-space member
// is an existing personal account. Missing members mean deny-all and are valid.
func ValidateTeamSpaceMemberships(accounts []*Account) error {
	byID := make(map[string]*Account, len(accounts))
	for _, account := range accounts {
		if account == nil {
			continue
		}
		byID[account.ID] = account
	}

	var problems []string
	for _, team := range accounts {
		if team == nil || team.Config == nil || !team.Config.IsTeamSpaceAccount() {
			continue
		}
		for _, member := range team.Config.TeamSpace.Members {
			if err := ValidateAccountID(member); err != nil {
				problems = append(problems, fmt.Sprintf("%s:%s invalid member id: %v", team.ID, member, err))
				continue
			}
			if member == team.ID {
				problems = append(problems, fmt.Sprintf("%s must not list itself as a team-space member", team.ID))
				continue
			}
			target := byID[member]
			if target == nil {
				problems = append(problems, fmt.Sprintf("%s references unknown member %s", team.ID, member))
				continue
			}
			if target.Config != nil && target.Config.IsTeamSpaceAccount() {
				problems = append(problems, fmt.Sprintf("%s member %s is another team space", team.ID, member))
			}
		}
	}
	if len(problems) == 0 {
		return nil
	}
	return fmt.Errorf("team-space membership validation failed: %s", strings.Join(problems, "; "))
}
```

- [ ] **Step 8: Run core tests and verify GREEN**

Run:

```bash
go test ./core -run 'TestValidateTeamSpace|TestValidateFamilyAccounts|TestTeamSpaceConfig|TestLegacyShareParsingStillLoads'
```

Expected: PASS.

- [ ] **Step 9: Commit Task 1**

Run:

```bash
git add apps/kittypaw/core/config.go apps/kittypaw/core/config_test.go apps/kittypaw/core/account.go apps/kittypaw/core/account_test.go
git commit -m "feat: add team space membership config"
```

---

### Task 2: Replace Per-Path Share Allowlist with Member-Scoped Team-Space Reads

**Files:**
- Modify: `apps/kittypaw/core/share.go`
- Modify: `apps/kittypaw/core/share_test.go`
- Modify: `apps/kittypaw/engine/share.go`
- Modify: `apps/kittypaw/engine/share_test.go`

- [ ] **Step 1: Write failing core read tests**

Update `setupShareFixture` in `apps/kittypaw/core/share_test.go` so it creates a team-space account directory:

```go
func setupShareFixture(t *testing.T) (ownerBase, outsideFile string, cfg *Config) {
	t.Helper()
	root := t.TempDir()

	ownerBase = filepath.Join(root, "accounts", "team")
	if err := os.MkdirAll(filepath.Join(ownerBase, "memory"), 0o755); err != nil {
		t.Fatalf("mkdir owner: %v", err)
	}
	if err := os.WriteFile(filepath.Join(ownerBase, "memory", "weather.json"), []byte(`{"temp":18}`), 0o644); err != nil {
		t.Fatalf("write weather: %v", err)
	}
	if err := os.WriteFile(filepath.Join(ownerBase, "config.toml"), []byte("is_shared=true\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	outside := filepath.Join(root, "outside")
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatalf("mkdir outside: %v", err)
	}
	outsideFile = filepath.Join(outside, "secret.txt")
	if err := os.WriteFile(outsideFile, []byte("nope"), 0o644); err != nil {
		t.Fatalf("write secret: %v", err)
	}

	cfg = &Config{
		IsShared: true,
		TeamSpace: TeamSpaceConfig{Members: []string{"alice"}},
	}
	return ownerBase, outsideFile, cfg
}
```

Replace `TestValidateSharedReadPath_Allowed` with:

```go
func TestValidateSharedReadPath_MemberCanReadMemory(t *testing.T) {
	ownerBase, _, cfg := setupShareFixture(t)

	got, err := ValidateSharedReadPath(cfg, ownerBase, "alice", "memory/weather.json")
	if err != nil {
		t.Fatalf("expected allow, got error: %v", err)
	}
	want, _ := filepath.EvalSymlinks(filepath.Join(ownerBase, "memory", "weather.json"))
	if got != want {
		t.Errorf("expected realpath %q, got %q", want, got)
	}
}
```

Add these tests:

```go
func TestValidateSharedReadPath_NonMemberRejected(t *testing.T) {
	ownerBase, _, cfg := setupShareFixture(t)
	_, err := ValidateSharedReadPath(cfg, ownerBase, "bob", "memory/weather.json")
	if !errors.Is(err, ErrCrossAccountUnauthorized) {
		t.Errorf("non-member must reject with unauthorized, got %v", err)
	}
}

func TestValidateSharedReadPath_LegacyShareDoesNotGrantMembership(t *testing.T) {
	ownerBase, _, _ := setupShareFixture(t)
	cfg := &Config{
		IsShared: true,
		Share: map[string]ShareConfig{
			"alice": {Read: []string{"memory/weather.json"}},
		},
	}
	_, err := ValidateSharedReadPath(cfg, ownerBase, "alice", "memory/weather.json")
	if !errors.Is(err, ErrCrossAccountUnauthorized) {
		t.Errorf("legacy share-only config must not grant membership, got %v", err)
	}
}

func TestValidateSharedReadPath_RejectsOperationalFiles(t *testing.T) {
	ownerBase, _, cfg := setupShareFixture(t)
	for _, req := range []string{"config.toml", "secrets.json", "account.toml", "data/kittypaw.db"} {
		t.Run(req, func(t *testing.T) {
			_, err := ValidateSharedReadPath(cfg, ownerBase, "alice", req)
			if !errors.Is(err, ErrCrossAccountNotShareable) {
				t.Errorf("request %q should reject as not shareable, got %v", req, err)
			}
		})
	}
}

func TestValidateSharedReadPath_MemberCanReadWorkspaceAlias(t *testing.T) {
	root := t.TempDir()
	ownerBase := filepath.Join(root, "accounts", "team")
	workspaceRoot := filepath.Join(root, "workspace")
	if err := os.MkdirAll(workspaceRoot, 0o755); err != nil {
		t.Fatalf("mkdir workspace: %v", err)
	}
	if err := os.WriteFile(filepath.Join(workspaceRoot, "plan.md"), []byte("ship it"), 0o644); err != nil {
		t.Fatalf("write workspace file: %v", err)
	}
	cfg := &Config{
		IsShared: true,
		TeamSpace: TeamSpaceConfig{Members: []string{"alice"}},
		Workspace: WorkspaceConfig{Roots: []WorkspaceRoot{{Alias: "ops", Path: workspaceRoot, Access: "read_write"}}},
	}
	got, err := ValidateSharedReadPath(cfg, ownerBase, "alice", "workspace/ops/plan.md")
	if err != nil {
		t.Fatalf("expected workspace allow, got %v", err)
	}
	want, _ := filepath.EvalSymlinks(filepath.Join(workspaceRoot, "plan.md"))
	if got != want {
		t.Errorf("realpath = %q, want %q", got, want)
	}
}
```

- [ ] **Step 2: Run core share tests and verify RED**

Run:

```bash
go test ./core -run 'TestValidateSharedReadPath'
```

Expected: FAIL because `ErrCrossAccountNotShareable` does not exist and current code still requires `[share.*]`.

- [ ] **Step 3: Implement member-scoped read validation**

In `apps/kittypaw/core/share.go`, add a new sentinel:

```go
	ErrCrossAccountNotShareable = errors.New("cross-account read: path is not in team-space shareable data")
```

Replace the allowlist section in `ValidateSharedReadPath` with:

```go
	if ownerCfg == nil || !ownerCfg.IsTeamSpaceAccount() || !ownerCfg.TeamSpaceHasMember(readerAccountID) {
		return "", ErrCrossAccountUnauthorized
	}

	abs, boundaryBase, err := resolveTeamSpaceSharedPath(ownerCfg, ownerBaseDir, cleaned)
	if err != nil {
		return "", err
	}
```

Add helper functions in the same file:

```go
func resolveTeamSpaceSharedPath(ownerCfg *Config, ownerBaseDir, cleaned string) (abs string, boundaryBase string, err error) {
	parts := strings.Split(cleaned, string(filepath.Separator))
	if len(parts) == 0 {
		return "", "", ErrCrossAccountNotShareable
	}
	switch parts[0] {
	case "memory":
		return filepath.Join(ownerBaseDir, cleaned), filepath.Join(ownerBaseDir, "memory"), nil
	case "workspace":
		if len(parts) < 3 {
			return "", "", ErrCrossAccountNotShareable
		}
		alias := parts[1]
		rel := filepath.Join(parts[2:]...)
		for _, root := range ownerCfg.WorkspaceRoots() {
			if root.Alias == alias && root.Path != "" {
				return filepath.Join(root.Path, rel), root.Path, nil
			}
		}
		return "", "", ErrCrossAccountNotShareable
	default:
		return "", "", ErrCrossAccountNotShareable
	}
}
```

Then change the symlink boundary base from `ownerBaseDir` to `boundaryBase`:

```go
	realBase, err := filepath.EvalSymlinks(boundaryBase)
```

Keep the existing `EvalSymlinks`, prefix check, hardlink check, and final return.

- [ ] **Step 4: Run core share tests and verify GREEN**

Run:

```bash
go test ./core -run 'TestValidateSharedReadPath'
```

Expected: PASS.

- [ ] **Step 5: Write failing engine share tests**

In `apps/kittypaw/engine/share_test.go`, update fixtures to use `team` account IDs and `TeamSpaceConfig{Members: []string{"alice"}}`. Add or update a test with this shape:

```go
func TestShareRead_RejectsNonMember(t *testing.T) {
	sess, _ := newShareFixture(t)
	out, _ := executeShare(context.Background(), mustCall(t, "team", "memory/weather.json"), &Session{
		AccountID:       "bob",
		AccountRegistry: sess.AccountRegistry,
	})
	var resp map[string]string
	if err := json.Unmarshal([]byte(out), &resp); err != nil {
		t.Fatalf("json: %v", err)
	}
	if !strings.Contains(resp["error"], "team space") {
		t.Errorf("expected team-space membership error, got %q", resp["error"])
	}
}
```

Update existing expectations from `"target is not the family account"` to `"target is not the team space"`.

- [ ] **Step 6: Run engine share tests and verify RED**

Run:

```bash
go test ./engine -run 'TestShareRead|TestFamily_ShareReadE2E'
```

Expected: FAIL because `engine/share.go` still returns family wording and fixtures still depend on `[share.*]`.

- [ ] **Step 7: Update engine share role check and wording**

In `apps/kittypaw/engine/share.go`, change the owner gate to:

```go
	owner := s.AccountRegistry.Get(targetID)
	if owner == nil || owner.Config == nil || !owner.Config.IsTeamSpaceAccount() {
		reason := "target_not_team_space"
		if owner == nil {
			reason = "unknown_account"
		}
		slog.Warn("cross_account_read_rejected",
			"from", s.AccountID, "to", targetID, "path", filepath.Clean(reqPath), "reason", reason)
		return jsonResult(map[string]any{"error": "cross-account read: target is not the team space"})
	}
```

Keep the call to `core.ValidateSharedReadPath`. Its `ErrCrossAccountUnauthorized` covers non-members.

- [ ] **Step 8: Run engine tests and verify GREEN**

Run:

```bash
go test ./engine -run 'TestShareRead|TestFamily_ShareReadE2E'
```

Expected: PASS after test fixture updates.

- [ ] **Step 9: Commit Task 2**

Run:

```bash
git add apps/kittypaw/core/share.go apps/kittypaw/core/share_test.go apps/kittypaw/engine/share.go apps/kittypaw/engine/share_test.go apps/kittypaw/engine/family_integration_test.go
git commit -m "feat: authorize team space reads by membership"
```

---

### Task 3: Restrict Fanout to Team-Space Members and Rename Internal Push Event

**Files:**
- Modify: `apps/kittypaw/core/types.go`
- Modify: `apps/kittypaw/core/fanout.go`
- Modify: `apps/kittypaw/core/fanout_test.go`
- Modify: `apps/kittypaw/engine/fanout_test.go`
- Modify: `apps/kittypaw/server/server.go`
- Modify: `apps/kittypaw/server/family_push_test.go`
- Modify: `apps/kittypaw/server/account_deps.go`
- Modify: `apps/kittypaw/server/account_config.go`

- [ ] **Step 1: Write failing core fanout membership tests**

In `apps/kittypaw/core/fanout_test.go`, update `newFanoutFixture` to register the source as a team space with members:

```go
func newFanoutFixture(t *testing.T, source string, members []string, peers ...string) (*ChannelFanout, chan Event, *AccountRegistry) {
	t.Helper()
	reg := NewAccountRegistry(t.TempDir(), source)
	reg.Register(&Account{ID: source, Config: &Config{
		IsShared: true,
		TeamSpace: TeamSpaceConfig{Members: members},
	}})
	for _, id := range peers {
		reg.Register(&Account{ID: id, Config: &Config{}})
	}
	eventCh := make(chan Event, 4)
	return NewChannelFanout(eventCh, reg, source), eventCh, reg
}
```

Update existing calls accordingly, and add:

```go
func TestChannelFanout_RejectsNonMemberTarget(t *testing.T) {
	f, _, _ := newFanoutFixture(t, "team", []string{"alice"}, "alice", "bob")
	err := f.Send(context.Background(), "bob", FanoutPayload{Text: "x"})
	if !errors.Is(err, ErrFanoutUnauthorizedTarget) {
		t.Errorf("expected ErrFanoutUnauthorizedTarget, got %v", err)
	}
}

func TestChannelFanout_BroadcastSendsOnlyToMembers(t *testing.T) {
	f, ch, _ := newFanoutFixture(t, "team", []string{"alice"}, "alice", "bob")

	if err := f.Broadcast(context.Background(), FanoutPayload{Text: "hi"}); err != nil {
		t.Fatalf("broadcast: %v", err)
	}

	select {
	case ev := <-ch:
		if ev.AccountID != "alice" {
			t.Fatalf("broadcast target = %q, want alice", ev.AccountID)
		}
	case <-time.After(time.Second):
		t.Fatal("event not delivered")
	}
	select {
	case ev := <-ch:
		t.Fatalf("unexpected second event: %#v", ev)
	default:
	}
}
```

Update the event assertion in `TestChannelFanout_SendEmitsEvent`:

```go
if ev.Type != EventTeamSpacePush {
	t.Errorf("type = %q, want %q", ev.Type, EventTeamSpacePush)
}
```

- [ ] **Step 2: Run core fanout tests and verify RED**

Run:

```bash
go test ./core -run 'TestChannelFanout'
```

Expected: FAIL because `ErrFanoutUnauthorizedTarget` and `EventTeamSpacePush` do not exist.

- [ ] **Step 3: Implement event rename and member-scoped fanout**

In `apps/kittypaw/core/types.go`, replace the family push constant with:

```go
	EventTeamSpacePush EventType = "team_space.push"

	// EventFamilyPush is retained as a compile-time compatibility alias while
	// tests and callers migrate to team-space terminology.
	EventFamilyPush EventType = EventTeamSpacePush
```

In `apps/kittypaw/core/fanout.go`, add:

```go
	ErrFanoutUnauthorizedTarget = errors.New("fanout: target is not a team-space member")
```

Change `Send` after registry lookup:

```go
	source := f.registry.Get(f.source)
	if source == nil || source.Config == nil || !source.Config.IsTeamSpaceAccount() {
		return ErrFanoutUnauthorizedTarget
	}
	if !source.Config.TeamSpaceHasMember(accountID) {
		return ErrFanoutUnauthorizedTarget
	}

	ev := Event{Type: EventTeamSpacePush, AccountID: accountID, Payload: body}
```

Change `Broadcast` to iterate source members, not `registry.List()`:

```go
	source := f.registry.Get(f.source)
	if source == nil || source.Config == nil || !source.Config.IsTeamSpaceAccount() {
		return ErrFanoutUnauthorizedTarget
	}
	for _, id := range source.Config.TeamSpace.Members {
		if id == f.source {
			continue
		}
		if err := f.Send(ctx, id, p); err != nil {
			return fmt.Errorf("broadcast to %q: %w", id, err)
		}
	}
	return nil
```

- [ ] **Step 4: Run core fanout tests and verify GREEN**

Run:

```bash
go test ./core -run 'TestChannelFanout'
```

Expected: PASS.

- [ ] **Step 5: Update server push dispatch tests to RED**

In `apps/kittypaw/server/family_push_test.go`, update team-space fixtures:

```go
teamDeps := buildAccountDeps(t, root, "team", &core.Config{
	IsShared: true,
	TeamSpace: core.TeamSpaceConfig{Members: []string{"alice", "bob"}},
})
```

For tests with `charlie`, include charlie in `Members`. Add a negative test:

```go
func TestTeamSpaceFanoutRejectsNonMember(t *testing.T) {
	root := t.TempDir()
	teamDeps := buildAccountDeps(t, root, "team", &core.Config{
		IsShared: true,
		TeamSpace: core.TeamSpaceConfig{Members: []string{"alice"}},
	})
	aliceDeps := buildAccountDeps(t, root, "alice", &core.Config{})
	bobDeps := buildAccountDeps(t, root, "bob", &core.Config{})
	srv := New([]*AccountDeps{teamDeps, aliceDeps, bobDeps}, "test")

	teamSess := srv.accounts.Session("team")
	if teamSess == nil || teamSess.Fanout == nil {
		t.Fatal("team-space session / Fanout missing")
	}
	err := teamSess.Fanout.Send(context.Background(), "bob", core.FanoutPayload{Text: "x"})
	if !errors.Is(err, core.ErrFanoutUnauthorizedTarget) {
		t.Fatalf("Send to non-member err = %v, want ErrFanoutUnauthorizedTarget", err)
	}
}
```

Add `errors` to imports for that test file.

- [ ] **Step 6: Run server fanout tests and verify RED**

Run:

```bash
go test ./server -run 'TestTeamSpace|TestFamilyMorningBrief|TestDispatchLoop_FamilyPush'
```

Expected: FAIL where server code still uses old names and fixtures are not migrated.

- [ ] **Step 7: Update server and session fanout wiring**

In `apps/kittypaw/server/account_deps.go` and `apps/kittypaw/server/account_config.go`, replace `IsSharedAccount()` fanout checks with:

```go
if td.Account.Config.IsTeamSpaceAccount() {
	sess.Fanout = core.NewChannelFanout(eventCh, registry, td.Account.ID)
}
```

In `apps/kittypaw/server/server.go`, rename helper functions and dispatch branch:

```go
if event.Type == core.EventTeamSpacePush {
	s.deliverTeamSpacePush(ctx, event)
	continue
}
```

Rename:

- `deliverFamilyPush` to `deliverTeamSpacePush`
- `enqueueFamilyPushForRetry` to `enqueueTeamSpacePushForRetry`
- `resolveFamilyPushChannel` to `resolveTeamSpacePushChannel`

Inside those functions, update log strings from `"family"` to `"team_space"` where they are active user/operator-facing log fields.

- [ ] **Step 8: Run server fanout tests and verify GREEN**

Run:

```bash
go test ./server -run 'TestTeamSpace|TestFamilyMorningBrief|TestDispatchLoop_FamilyPush'
```

Expected: PASS after migrated names and membership fixtures.

- [ ] **Step 9: Run engine fanout tests**

Run:

```bash
go test ./engine -run 'TestFanout|TestFamily_FanoutE2E'
```

Expected: PASS after updating integration fixture team-space members.

- [ ] **Step 10: Commit Task 3**

Run:

```bash
git add apps/kittypaw/core/types.go apps/kittypaw/core/fanout.go apps/kittypaw/core/fanout_test.go apps/kittypaw/engine/fanout_test.go apps/kittypaw/engine/family_integration_test.go apps/kittypaw/server/server.go apps/kittypaw/server/family_push_test.go apps/kittypaw/server/account_deps.go apps/kittypaw/server/account_config.go
git commit -m "feat: restrict team space fanout to members"
```

---

### Task 4: Validate Memberships at Startup, Reload, Hot-Add, and Setup

**Files:**
- Modify: `apps/kittypaw/server/server.go`
- Modify: `apps/kittypaw/server/admin.go`
- Modify: `apps/kittypaw/server/account_config.go`
- Modify: `apps/kittypaw/server/api.go`
- Modify: `apps/kittypaw/cli/main.go`
- Modify: affected server and CLI validation tests

- [ ] **Step 1: Write failing startup validation test**

Add to `apps/kittypaw/server/account_session_test.go`:

```go
func TestStartChannelsRejectsUnknownTeamSpaceMember(t *testing.T) {
	root := t.TempDir()
	teamDeps := buildAccountDeps(t, root, "team", &core.Config{
		IsShared: true,
		TeamSpace: core.TeamSpaceConfig{Members: []string{"ghost"}},
	})
	srv := New([]*AccountDeps{teamDeps}, "test")

	err := srv.StartChannels(context.Background())
	if err == nil {
		t.Fatal("expected membership validation error")
	}
	if !strings.Contains(err.Error(), "team-space membership validation") {
		t.Fatalf("error = %v, want membership validation", err)
	}
}
```

Add `context` to imports if absent.

- [ ] **Step 2: Run startup validation test and verify RED**

Run:

```bash
go test ./server -run TestStartChannelsRejectsUnknownTeamSpaceMember
```

Expected: FAIL because `StartChannels` does not call `ValidateTeamSpaceMemberships`.

- [ ] **Step 3: Implement validation calls in startup and config updates**

In `apps/kittypaw/server/server.go` inside `StartChannels`, after `ValidateTeamSpaceAccounts`:

```go
	if err := core.ValidateTeamSpaceMemberships(s.accountList); err != nil {
		return fmt.Errorf("team-space membership validation: %w", err)
	}
```

In `apps/kittypaw/server/admin.go` inside `AddAccount`, after team-space account validation:

```go
	proposedAccounts := append(append([]*core.Account(nil), s.accountList...), t)
	if err := core.ValidateTeamSpaceAccounts(proposedAccounts); err != nil {
		return fmt.Errorf("team space validation: %w", err)
	}
	if err := core.ValidateTeamSpaceMemberships(proposedAccounts); err != nil {
		return fmt.Errorf("team-space membership validation: %w", err)
	}
```

Use the same `proposedAccounts` variable instead of building the slice twice.

In `apps/kittypaw/server/account_config.go` inside `validateAccountConfigUpdateWithKakaoAPIURLLocked`, after team-space account validation:

```go
	if err := core.ValidateTeamSpaceMemberships(accounts); err != nil {
		return fmt.Errorf("team-space membership validation: %w", err)
	}
```

In `apps/kittypaw/server/api.go` reload validation and `apps/kittypaw/cli/main.go` bootstrap validation, call the same two validators in this order:

```go
if err := core.ValidateTeamSpaceAccounts(accounts); err != nil {
	return fmt.Errorf("team space validation: %w", err)
}
if err := core.ValidateTeamSpaceMemberships(accounts); err != nil {
	return fmt.Errorf("team-space membership validation: %w", err)
}
```

Keep `ValidateFamilyAccounts` only where compatibility tests have not yet been migrated; prefer new names in touched code.

- [ ] **Step 4: Run validation tests and verify GREEN**

Run:

```bash
go test ./server -run 'TestStartChannelsRejectsUnknownTeamSpaceMember|TestHandleReload|TestAddAccount'
```

Expected: PASS after updating old expected strings from `"shared account validation"` to the new wording where tests touch these paths.

- [ ] **Step 5: Run CLI bootstrap validation tests**

Run:

```bash
go test ./cli -run 'TestBootstrap|TestRunSetup'
```

Expected: PASS after updating expected validation strings.

- [ ] **Step 6: Commit Task 4**

Run:

```bash
git add apps/kittypaw/server/server.go apps/kittypaw/server/admin.go apps/kittypaw/server/account_config.go apps/kittypaw/server/api.go apps/kittypaw/server/account_session_test.go apps/kittypaw/server/admin_test.go apps/kittypaw/server/api_reload_validation_test.go apps/kittypaw/cli/main.go apps/kittypaw/cli/cmd_setup_test.go
git commit -m "feat: validate team space memberships"
```

---

### Task 5: Add Account-Aware Scheduler Manager

**Files:**
- Create: `apps/kittypaw/server/account_schedulers.go`
- Create: `apps/kittypaw/server/account_schedulers_test.go`
- Modify: `apps/kittypaw/server/server.go`
- Modify: `apps/kittypaw/server/admin.go`
- Modify: `apps/kittypaw/server/account_config.go`
- Modify: `apps/kittypaw/server/account_session_test.go`

- [ ] **Step 1: Write failing scheduler manager tests**

Create `apps/kittypaw/server/account_schedulers_test.go`:

```go
package server

import (
	"testing"

	"github.com/jinto/kittypaw/core"
)

func TestServerNewCreatesSchedulerPerAccount(t *testing.T) {
	root := t.TempDir()
	teamDeps := buildAccountDeps(t, root, "team", &core.Config{
		IsShared: true,
		TeamSpace: core.TeamSpaceConfig{Members: []string{"alice"}},
	})
	aliceDeps := buildAccountDeps(t, root, "alice", &core.Config{})
	bobDeps := buildAccountDeps(t, root, "bob", &core.Config{})

	srv := New([]*AccountDeps{teamDeps, aliceDeps, bobDeps}, "test")

	if got := srv.schedulers.Len(); got != 3 {
		t.Fatalf("scheduler count = %d, want 3", got)
	}
	for _, id := range []string{"team", "alice", "bob"} {
		if !srv.schedulers.Has(id) {
			t.Fatalf("scheduler missing for account %q", id)
		}
	}
}

func TestAddRemoveAccountMaintainsScheduler(t *testing.T) {
	root := t.TempDir()
	aliceDeps := buildAccountDeps(t, root, "alice", &core.Config{})
	srv := New([]*AccountDeps{aliceDeps}, "test")

	bob := buildAccountDeps(t, root, "bob", &core.Config{}).Account
	if err := srv.AddAccount(bob); err != nil {
		t.Fatalf("AddAccount: %v", err)
	}
	if !srv.schedulers.Has("bob") {
		t.Fatal("scheduler missing after AddAccount")
	}
	if err := srv.RemoveAccount("bob"); err != nil {
		t.Fatalf("RemoveAccount: %v", err)
	}
	if srv.schedulers.Has("bob") {
		t.Fatal("scheduler still registered after RemoveAccount")
	}
}
```

- [ ] **Step 2: Run scheduler tests and verify RED**

Run:

```bash
go test ./server -run 'TestServerNewCreatesSchedulerPerAccount|TestAddRemoveAccountMaintainsScheduler'
```

Expected: FAIL because `srv.schedulers` and `AccountSchedulers` do not exist.

- [ ] **Step 3: Implement AccountSchedulers**

Create `apps/kittypaw/server/account_schedulers.go`:

```go
package server

import (
	"context"
	"sync"

	"github.com/jinto/kittypaw/engine"
)

type AccountSchedulers struct {
	mu         sync.Mutex
	schedulers map[string]*engine.Scheduler
	started    map[string]bool
	ctx        context.Context
	running    bool
}

func NewAccountSchedulers() *AccountSchedulers {
	return &AccountSchedulers{
		schedulers: make(map[string]*engine.Scheduler),
		started:    make(map[string]bool),
	}
}

func (m *AccountSchedulers) Register(accountID string, scheduler *engine.Scheduler) {
	if scheduler == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.schedulers[accountID] = scheduler
	if m.running && !m.started[accountID] {
		m.started[accountID] = true
		go scheduler.Start(m.ctx)
	}
}

func (m *AccountSchedulers) Remove(accountID string) {
	m.mu.Lock()
	scheduler := m.schedulers[accountID]
	delete(m.schedulers, accountID)
	delete(m.started, accountID)
	m.mu.Unlock()
	if scheduler != nil {
		scheduler.Stop()
		scheduler.Wait()
	}
}

func (m *AccountSchedulers) StartAll(ctx context.Context) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ctx = ctx
	m.running = true
	for id, scheduler := range m.schedulers {
		if m.started[id] {
			continue
		}
		m.started[id] = true
		go scheduler.Start(ctx)
	}
}

func (m *AccountSchedulers) StopAll() {
	m.mu.Lock()
	schedulers := make([]*engine.Scheduler, 0, len(m.schedulers))
	for _, scheduler := range m.schedulers {
		schedulers = append(schedulers, scheduler)
	}
	m.running = false
	m.mu.Unlock()
	for _, scheduler := range schedulers {
		scheduler.Stop()
	}
}

func (m *AccountSchedulers) WaitAll() {
	m.mu.Lock()
	schedulers := make([]*engine.Scheduler, 0, len(m.schedulers))
	for _, scheduler := range m.schedulers {
		schedulers = append(schedulers, scheduler)
	}
	m.mu.Unlock()
	for _, scheduler := range schedulers {
		scheduler.Wait()
	}
}

func (m *AccountSchedulers) Has(accountID string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.schedulers[accountID]
	return ok
}

func (m *AccountSchedulers) Len() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.schedulers)
}
```

- [ ] **Step 4: Wire scheduler manager into Server.New**

In `apps/kittypaw/server/server.go`, replace:

```go
	scheduler       *engine.Scheduler
```

with:

```go
	schedulers      *AccountSchedulers
```

In `NewWithServerConfig`, build schedulers while iterating account deps:

```go
	schedulers := NewAccountSchedulers()
	for _, td := range accounts {
		sess := buildAccountSession(td, registry, eventCh)
		router.Register(td.Account.ID, sess)
		depsByID[td.Account.ID] = td
		schedulers.Register(td.Account.ID, engine.NewScheduler(sess, td.PkgMgr))
		if td == defaultDeps {
			defaultSession = sess
		}
	}
```

Set the server field:

```go
		schedulers:      schedulers,
```

Remove construction of the old default-only scheduler.

- [ ] **Step 5: Wire start and shutdown**

In `ListenAndServe`, replace:

```go
go s.scheduler.Start(schedCtx)
```

with:

```go
s.schedulers.StartAll(schedCtx)
```

Replace shutdown scheduler calls with:

```go
s.schedulers.StopAll()
schedCancel()
s.schedulers.WaitAll()
```

- [ ] **Step 6: Wire AddAccount and RemoveAccount**

In `AddAccount`, after session registration and `accountDeps` registration, add rollback-aware scheduler registration:

```go
	scheduler := engine.NewScheduler(sess, td.PkgMgr)
	s.schedulers.Register(t.ID, scheduler)
	rollback = append(rollback, func() { s.schedulers.Remove(t.ID) })
```

In `RemoveAccount`, before closing deps:

```go
	if s.schedulers != nil {
		s.schedulers.Remove(id)
	}
```

- [ ] **Step 7: Run scheduler manager tests and verify GREEN**

Run:

```bash
go test ./server -run 'TestServerNewCreatesSchedulerPerAccount|TestAddRemoveAccountMaintainsScheduler'
```

Expected: PASS.

- [ ] **Step 8: Run broader server account tests**

Run:

```bash
go test ./server -run 'TestServer_New|TestServerNew|TestAddAccount|TestRemoveAccount'
```

Expected: PASS after updating any direct assumptions about a single scheduler.

- [ ] **Step 9: Commit Task 5**

Run:

```bash
git add apps/kittypaw/server/account_schedulers.go apps/kittypaw/server/account_schedulers_test.go apps/kittypaw/server/server.go apps/kittypaw/server/admin.go apps/kittypaw/server/account_config.go apps/kittypaw/server/account_session_test.go
git commit -m "feat: run schedulers per account"
```

---

### Task 6: Scrub Removed Accounts from Team-Space Membership

**Files:**
- Modify: `apps/kittypaw/cli/cmd_account.go`
- Modify: `apps/kittypaw/cli/cmd_account_test.go`

- [ ] **Step 1: Write failing CLI scrub test**

In `apps/kittypaw/cli/cmd_account_test.go`, update `TestRunAccountRemove_SharedConfigScrub` or add:

```go
func TestRunAccountRemove_TeamSpaceMembershipScrub(t *testing.T) {
	home := setupRemoveFixture(t, map[string]bool{"team": true, "alice": false, "bob": false})
	teamCfgPath := filepath.Join(home, ".kittypaw", "accounts", "team", "config.toml")
	cfg, err := core.LoadConfig(teamCfgPath)
	if err != nil {
		t.Fatalf("load team config: %v", err)
	}
	cfg.TeamSpace.Members = []string{"alice", "bob"}
	if err := core.WriteConfigAtomic(cfg, teamCfgPath); err != nil {
		t.Fatalf("write team config: %v", err)
	}

	var stdout, stderr bytes.Buffer
	if err := runAccountRemove("alice", &stdout, &stderr); err != nil {
		t.Fatalf("runAccountRemove: %v", err)
	}

	reloaded, err := core.LoadConfig(teamCfgPath)
	if err != nil {
		t.Fatalf("reload team config: %v", err)
	}
	if slices.Contains(reloaded.TeamSpace.Members, "alice") {
		t.Fatal("alice still listed in team-space members after removal")
	}
	if !slices.Contains(reloaded.TeamSpace.Members, "bob") {
		t.Fatal("bob should remain in team-space members")
	}
}
```

Add `slices` to imports for Go 1.21+ if the module supports it; otherwise check manually with a small helper in the test file.

- [ ] **Step 2: Run CLI test and verify RED**

Run:

```bash
go test ./cli -run TestRunAccountRemove_TeamSpaceMembershipScrub
```

Expected: FAIL because removal only scrubs `[share.<removed>]`.

- [ ] **Step 3: Implement membership scrub**

In `apps/kittypaw/cli/cmd_account.go`, update `scrubSharedShare` to also remove the account from `shared.Config.TeamSpace.Members`. Rename the function to `scrubTeamSpaceReferences` if the call sites are touched:

```go
func scrubTeamSpaceReferences(accountsDir, removed string, stderr io.Writer) error {
	accounts, err := core.LoadAccounts(accountsDir)
	if err != nil {
		return err
	}
	for _, team := range accounts {
		if team == nil || team.Config == nil || !team.Config.IsTeamSpaceAccount() {
			continue
		}
		changed := false
		if _, ok := team.Config.Share[removed]; ok {
			delete(team.Config.Share, removed)
			changed = true
		}
		members := team.Config.TeamSpace.Members[:0]
		for _, member := range team.Config.TeamSpace.Members {
			if member == removed {
				changed = true
				continue
			}
			members = append(members, member)
		}
		team.Config.TeamSpace.Members = members
		if !changed {
			continue
		}
		cfgPath := filepath.Join(team.BaseDir, "config.toml")
		if err := core.WriteConfigAtomic(team.Config, cfgPath); err != nil {
			return err
		}
		_, _ = fmt.Fprintf(stderr, "info: removed account %q from team space config at %s\n", removed, cfgPath)
	}
	return nil
}
```

Update `runAccountRemove` to call `scrubTeamSpaceReferences`.

- [ ] **Step 4: Run CLI remove tests and verify GREEN**

Run:

```bash
go test ./cli -run 'TestRunAccountRemove'
```

Expected: PASS after updating output-string expectations where needed.

- [ ] **Step 5: Commit Task 6**

Run:

```bash
git add apps/kittypaw/cli/cmd_account.go apps/kittypaw/cli/cmd_account_test.go
git commit -m "feat: scrub team space members on account removal"
```

---

### Task 7: Update Active Terminology and Public Metadata

**Files:**
- Modify: `apps/kittypaw/core/skillmeta.go`
- Modify: `apps/kittypaw/README.md`
- Modify: `html/index.html`
- Modify: `html/en/index.html`
- Modify: `html/ja/index.html`
- Modify: active comments in touched Go files
- Modify: relevant tests that assert no family CLI surface

- [ ] **Step 1: Write failing metadata terminology test**

Add to `apps/kittypaw/core/config_test.go` or a new `apps/kittypaw/core/skillmeta_test.go`:

```go
func TestSkillMetadataUsesTeamSpaceTerminology(t *testing.T) {
	for _, skill := range SkillMetadata() {
		for _, method := range skill.Methods {
			if strings.Contains(strings.ToLower(method.Signature), "family account") {
				t.Fatalf("method %s.%s exposes family-account terminology: %q", skill.Name, method.Name, method.Signature)
			}
		}
	}
}
```

Add `strings` import if using a new file.

- [ ] **Step 2: Run metadata test and verify RED**

Run:

```bash
go test ./core -run TestSkillMetadataUsesTeamSpaceTerminology
```

Expected: FAIL because `FAMILY ACCOUNT ONLY` is still present.

- [ ] **Step 3: Update skill metadata**

In `apps/kittypaw/core/skillmeta.go`, replace:

```go
FAMILY ACCOUNT ONLY
```

with:

```go
TEAM SPACE ONLY
```

- [ ] **Step 4: Update active docs and website status copy**

Replace:

```text
Family account
```

with:

```text
Team space
```

in:

- `apps/kittypaw/README.md`
- `html/index.html`
- `html/en/index.html`
- `html/ja/index.html`

Do not replace CSS `font-family`, model provider family, product family, or historical plan text unless it is active public copy.

- [ ] **Step 5: Run terminology search**

Run from repo root:

```bash
rg -n -i --glob '!**/*_test.go' 'family account|family-only|family\.push|EventFamilyPush|ValidateFamilyAccounts|IsFamily|FAMILY ACCOUNT ONLY' apps/kittypaw html README.md
```

Expected: no active production/doc hits except compatibility aliases or historical docs intentionally left in place. If compatibility aliases remain, add comments that they are legacy aliases and not user-facing terminology.

- [ ] **Step 6: Run metadata test and verify GREEN**

Run:

```bash
go test ./core -run TestSkillMetadataUsesTeamSpaceTerminology
```

Expected: PASS.

- [ ] **Step 7: Commit Task 7**

Run:

```bash
git add apps/kittypaw/core/skillmeta.go apps/kittypaw/core/skillmeta_test.go apps/kittypaw/README.md html/index.html html/en/index.html html/ja/index.html
git commit -m "docs: rename family account surface to team space"
```

---

### Task 8: Full Verification

**Files:**
- No planned edits unless verification finds failures.

- [ ] **Step 1: Run focused package tests**

Run from `apps/kittypaw`:

```bash
go test ./core ./engine ./server ./cli
```

Expected: PASS.

- [ ] **Step 2: Run repo-level app test target**

Run from repo root:

```bash
make test
```

Expected: PASS. If this target covers services outside `apps/kittypaw`, fix only failures caused by this feature.

- [ ] **Step 3: Run terminology audit**

Run from repo root:

```bash
rg -n -i 'family account|family-only|family member|family\.push|FAMILY ACCOUNT ONLY|target is not the family account' apps/kittypaw html README.md
```

Expected: only historical test names/comments or compatibility aliases remain. Active user-facing strings must use "team space".

- [ ] **Step 4: Run git status**

Run:

```bash
git status --short
```

Expected: clean worktree after commits.

- [ ] **Step 5: Final commit if verification fixes were needed**

If verification required additional fixes, commit them:

```bash
git add -A
git commit -m "test: verify team space membership flow"
```

Skip this step if no files changed during verification.

---

## Self-Review

Spec coverage:

- Team-space membership list is implemented in Task 1.
- Deny-all missing members is covered by Task 1 and Task 2 tests.
- Member reads over shareable data are covered by Task 2.
- Fanout restriction and internal event rename are covered by Task 3.
- Membership validation at startup/reload/hot-add is covered by Task 4.
- Account-aware schedulers are covered by Task 5.
- Removed-account membership cleanup is covered by Task 6.
- Active terminology cleanup is covered by Task 7.
- Full test and grep verification is covered by Task 8.

Placeholder scan:

- The plan contains concrete file paths, commands, code snippets, and expected
  outcomes for every task.

Type consistency:

- New config type is consistently `TeamSpaceConfig`.
- New helper is consistently `IsTeamSpaceAccount`.
- New event is consistently `EventTeamSpacePush` with a temporary `EventFamilyPush` alias.
- New scheduler manager is consistently `AccountSchedulers`.
