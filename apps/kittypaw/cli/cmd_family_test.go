package main

import (
	"bufio"
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// validTok is the shortest Telegram-format token that passes ValidateTelegramToken.
// Defined inline per test would clutter diffs without adding safety.
const (
	validTok1 = "11111:ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghij"
	validTok2 = "22222:ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghij"
	validTok3 = "33333:ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghij"
)

// Batch stdin would let an automated script blow away existing accounts in a loop.
// The wizard must refuse unless stdin is a real TTY.
func TestFamilyInit_NonTTY_Rejects(t *testing.T) {
	var stdout, stderr bytes.Buffer
	f := &familyInitFlags{}

	err := runFamilyInit(context.Background(), f, false, strings.NewReader(""), &stdout, &stderr)
	if err == nil {
		t.Fatal("expected error for non-TTY stdin, got nil")
	}
	if !strings.Contains(err.Error(), "interactive") {
		t.Errorf("error should mention interactive-only, got %q", err.Error())
	}
}

// scanExistingAccounts powers idempotency (skip a name that's already on disk)
// AND in-run token dedup (reject a token already claimed by a peer).
// If either side is wrong the wizard corrupts the accounts dir.
func TestScanExistingAccounts_BuildsSeenSet(t *testing.T) {
	accountsDir := t.TempDir()

	// alice: personal account with a bot token.
	aliceToken := "11111:ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghij"
	writeAccountConfig(t, accountsDir, "alice", `
admin_chat_ids = ["111"]
[[channels]]
channel_type = "telegram"
token = "`+aliceToken+`"
`)
	// family: no channels, IsFamily-style.
	writeAccountConfig(t, accountsDir, "family", `
is_family = true
`)
	// bob: account with a different token.
	bobToken := "22222:ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghij"
	writeAccountConfig(t, accountsDir, "bob", `
admin_chat_ids = ["222"]
[[channels]]
channel_type = "telegram"
token = "`+bobToken+`"
`)
	// non-directory entries and hidden staging must be ignored.
	if err := os.WriteFile(filepath.Join(accountsDir, "loose.txt"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write loose file: %v", err)
	}

	seen, err := scanExistingAccounts(accountsDir)
	if err != nil {
		t.Fatalf("scanExistingAccounts: %v", err)
	}

	for _, id := range []string{"alice", "bob", "family"} {
		if _, ok := seen.accounts[id]; !ok {
			t.Errorf("accounts set missing %q; got %v", id, seen.accounts)
		}
	}
	if owner := seen.tokens[aliceToken]; owner != "alice" {
		t.Errorf("alice's token owner = %q, want alice", owner)
	}
	if owner := seen.tokens[bobToken]; owner != "bob" {
		t.Errorf("bob's token owner = %q, want bob", owner)
	}
	// family has no channel → no token entry for it.
	for token, owner := range seen.tokens {
		if owner == "family" {
			t.Errorf("family must not contribute a token, but token %q is mapped to it", token)
		}
	}
}

func writeAccountConfig(t *testing.T, accountsDir, id, body string) {
	t.Helper()
	dir := filepath.Join(accountsDir, id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte(body), 0o644); err != nil {
		t.Fatalf("write config for %s: %v", id, err)
	}
}

// Re-running the wizard with a half-finished household must NOT overwrite the
// already-onboarded accounts — the idempotent skip is the only thing standing
// between the admin and a wiped alice dir.
func TestProvisionMember_AlreadyExistsSkips(t *testing.T) {
	accountsDir := t.TempDir()
	writeAccountConfig(t, accountsDir, "alice", `admin_chat_ids = ["111"]`)

	var stdout, stderr bytes.Buffer
	entry := provisionMember(accountsDir, "alice",
		"11111:ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghij", "111",
		&stdout, &stderr)

	if entry.Status != statusSkippedExisting {
		t.Errorf("status = %q, want %q", entry.Status, statusSkippedExisting)
	}
	if entry.Name != "alice" {
		t.Errorf("name = %q, want alice", entry.Name)
	}
	// Prior config must be byte-identical — skip must not touch it.
	got, err := os.ReadFile(filepath.Join(accountsDir, "alice", "config.toml"))
	if err != nil {
		t.Fatalf("read alice config: %v", err)
	}
	if string(got) != `admin_chat_ids = ["111"]` {
		t.Errorf("alice config got rewritten: %q", string(got))
	}
}

// If the token is bogus the wizard must record a FAILED entry and leave disk
// untouched — swallowing the error would mint a broken account the daemon
// can't boot.
func TestProvisionMember_InvalidTokenFails(t *testing.T) {
	accountsDir := t.TempDir()

	var stdout, stderr bytes.Buffer
	entry := provisionMember(accountsDir, "charlie",
		"not-a-real-token", "111", &stdout, &stderr)

	if entry.Status != statusFailed {
		t.Errorf("status = %q, want %q", entry.Status, statusFailed)
	}
	if !strings.Contains(entry.Reason, "token") {
		t.Errorf("reason should mention token, got %q", entry.Reason)
	}
	if _, err := os.Stat(filepath.Join(accountsDir, "charlie")); !os.IsNotExist(err) {
		t.Errorf("no account dir should be created on validation failure")
	}
}

// AC-W1: the primary happy path. Admin types three name/token/chatID
// triples then a blank line to finish — wizard must produce three OK
// entries and three accounts on disk.
func TestPromptMembers_ThreeValidMembers(t *testing.T) {
	accountsDir := t.TempDir()
	seen := &seenSet{accounts: map[string]struct{}{}, tokens: map[string]string{}}

	// Each member: name / token / chatID. Blank line on the 4th name ends the loop.
	input := strings.Join([]string{
		"alice", validTok1, "111",
		"bob", validTok2, "222",
		"charlie", validTok3, "333",
		"", // stop
	}, "\n") + "\n"

	var stdout, stderr bytes.Buffer
	entries := promptMembers(context.Background(), bufio.NewReader(strings.NewReader(input)),
		&stdout, &stderr, accountsDir, 10, seen)

	if len(entries) != 3 {
		t.Fatalf("entries = %d, want 3 (stdout=%q stderr=%q)",
			len(entries), stdout.String(), stderr.String())
	}
	for i, want := range []string{"alice", "bob", "charlie"} {
		if entries[i].Name != want {
			t.Errorf("entries[%d].Name = %q, want %q", i, entries[i].Name, want)
		}
		if entries[i].Status != statusOK {
			t.Errorf("entries[%d].Status = %q, want %q", i, entries[i].Status, statusOK)
		}
		if _, err := os.Stat(filepath.Join(accountsDir, want, "config.toml")); err != nil {
			t.Errorf("config.toml missing for %s: %v", want, err)
		}
	}
}

// Stop-on-blank is the primary way the admin ends the wizard — the loop
// must treat blank name as a clean terminator, not as a validation error.
func TestPromptMembers_BlankNameStops(t *testing.T) {
	accountsDir := t.TempDir()
	seen := &seenSet{accounts: map[string]struct{}{}, tokens: map[string]string{}}

	input := "alice\n" + validTok1 + "\n111\n\n" // blank name terminates

	var stdout, stderr bytes.Buffer
	entries := promptMembers(context.Background(), bufio.NewReader(strings.NewReader(input)),
		&stdout, &stderr, accountsDir, 10, seen)

	if len(entries) != 1 || entries[0].Name != "alice" {
		t.Fatalf("entries = %+v, want single alice", entries)
	}
}

// --max guards against a runaway script that pastes 50 members by accident;
// after max, the loop must stop WITHOUT consuming more stdin.
func TestPromptMembers_MaxReached(t *testing.T) {
	accountsDir := t.TempDir()
	seen := &seenSet{accounts: map[string]struct{}{}, tokens: map[string]string{}}

	// 3 members in input, but max=2 means only 2 get committed.
	input := strings.Join([]string{
		"alice", validTok1, "111",
		"bob", validTok2, "222",
		"charlie", validTok3, "333",
	}, "\n") + "\n"

	var stdout, stderr bytes.Buffer
	entries := promptMembers(context.Background(), bufio.NewReader(strings.NewReader(input)),
		&stdout, &stderr, accountsDir, 2, seen)

	if len(entries) != 2 {
		t.Fatalf("entries = %d, want 2 (max=2)", len(entries))
	}
	if _, err := os.Stat(filepath.Join(accountsDir, "charlie")); !os.IsNotExist(err) {
		t.Errorf("charlie should not exist after max-reached stop")
	}
}

// Uppercase or special chars in the name would poison the filesystem layout
// AND the AccountRouter key. The wizard must re-prompt (not fail) so the user
// can correct without restarting.
func TestPromptMembers_InvalidNameRePrompts(t *testing.T) {
	accountsDir := t.TempDir()
	seen := &seenSet{accounts: map[string]struct{}{}, tokens: map[string]string{}}

	input := "Alice\nalice\n" + validTok1 + "\n111\n\n"

	var stdout, stderr bytes.Buffer
	entries := promptMembers(context.Background(), bufio.NewReader(strings.NewReader(input)),
		&stdout, &stderr, accountsDir, 10, seen)

	if len(entries) != 1 || entries[0].Name != "alice" {
		t.Fatalf("entries = %+v, want single alice (after re-prompt)", entries)
	}
	if !strings.Contains(stderr.String(), "invalid account id") {
		t.Errorf("stderr should explain why Alice was rejected, got %q", stderr.String())
	}
}

// D6: two members cannot share a bot token — silently accepting it would
// land a second account with a duplicate token that blocks daemon startup.
func TestPromptMembers_DuplicateTokenRejected(t *testing.T) {
	accountsDir := t.TempDir()
	seen := &seenSet{accounts: map[string]struct{}{}, tokens: map[string]string{}}

	// alice uses validTok1. bob tries validTok1 → rejected → bob types validTok2 → accepted.
	input := strings.Join([]string{
		"alice", validTok1, "111",
		"bob", validTok1, validTok2, "222",
		"",
	}, "\n") + "\n"

	var stdout, stderr bytes.Buffer
	entries := promptMembers(context.Background(), bufio.NewReader(strings.NewReader(input)),
		&stdout, &stderr, accountsDir, 10, seen)

	if len(entries) != 2 {
		t.Fatalf("entries = %d, want 2 (alice + bob after re-prompt)", len(entries))
	}
	if entries[1].Name != "bob" || entries[1].Status != statusOK {
		t.Errorf("entries[1] = %+v, want bob ok", entries[1])
	}
	if !strings.Contains(stderr.String(), "already used") && !strings.Contains(stderr.String(), "duplicate") {
		t.Errorf("stderr should explain duplicate token rejection, got %q", stderr.String())
	}
}

// The family account is the one place that learns cross-account read grants;
// if the [share.family] stanza is missing, the admin has nowhere to add
// paths later without editing TOML by hand — regression we cannot tolerate.
func TestCreateFamilyAccount_Default(t *testing.T) {
	accountsDir := t.TempDir()
	seen := &seenSet{accounts: map[string]struct{}{}, tokens: map[string]string{}}

	var stdout, stderr bytes.Buffer
	entry := createFamilyAccount(accountsDir, seen, &stdout, &stderr)

	if entry.Status != statusOK {
		t.Fatalf("status = %q, want ok (stderr=%q)", entry.Status, stderr.String())
	}
	if entry.Name != "family" {
		t.Errorf("name = %q, want family", entry.Name)
	}
	cfgPath := filepath.Join(accountsDir, "family", "config.toml")
	body, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("read family config: %v", err)
	}
	got := string(body)
	if !strings.Contains(got, "is_family = true") {
		t.Errorf("config missing is_family flag:\n%s", got)
	}
	if !strings.Contains(got, "[share.family]") {
		t.Errorf("config missing [share.family] stanza:\n%s", got)
	}
}

// Re-running after the family account already exists must be a clean skip,
// not a failure — the admin may be onboarding one more person six months
// after the initial household setup.
func TestCreateFamilyAccount_IdempotentWhenExists(t *testing.T) {
	accountsDir := t.TempDir()
	writeAccountConfig(t, accountsDir, "family", `is_family = true`)
	seen := &seenSet{
		accounts: map[string]struct{}{"family": {}},
		tokens:   map[string]string{},
	}

	var stdout, stderr bytes.Buffer
	entry := createFamilyAccount(accountsDir, seen, &stdout, &stderr)

	if entry.Status != statusSkippedExisting {
		t.Errorf("status = %q, want %q", entry.Status, statusSkippedExisting)
	}
	// Pre-existing family config must be byte-identical.
	got, err := os.ReadFile(filepath.Join(accountsDir, "family", "config.toml"))
	if err != nil {
		t.Fatalf("read family config: %v", err)
	}
	if string(got) != `is_family = true` {
		t.Errorf("pre-existing family config was modified: %q", string(got))
	}
}

// Exit code must reflect whether ANY member failed — a single failed
// member is enough to fail CI, because silently exiting 0 on partial
// failure is the classic "ship broken" trap.
func TestPrintSummary_ExitCodes(t *testing.T) {
	cases := []struct {
		name    string
		entries []memberEntry
		want    int
	}{
		{"empty", nil, 0},
		{"all ok", []memberEntry{{Status: statusOK}, {Status: statusOK}}, 0},
		{"ok + skipped", []memberEntry{{Status: statusOK}, {Status: statusSkippedExisting}}, 0},
		{"ok + failed", []memberEntry{{Status: statusOK}, {Status: statusFailed}}, 1},
		{"all failed", []memberEntry{{Status: statusFailed}}, 1},
		{"two failed", []memberEntry{{Status: statusFailed}, {Status: statusFailed}}, 2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			got := printSummary(tc.entries, &buf)
			if got != tc.want {
				t.Errorf("printSummary(%s) = %d, want %d (output=%q)",
					tc.name, got, tc.want, buf.String())
			}
		})
	}
}

// End-to-end: the wizard must produce three personal accounts + one family
// account from a single stdin script. This is the canonical AC-W1 case and
// the last thing to break before release.
func TestRunFamilyInit_EndToEnd_HappyPath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	input := strings.Join([]string{
		"alice", validTok1, "111",
		"bob", validTok2, "222",
		"charlie", validTok3, "333",
		"",
	}, "\n") + "\n"

	var stdout, stderr bytes.Buffer
	f := &familyInitFlags{max: 10, noFamily: false}

	err := runFamilyInit(context.Background(), f, true,
		strings.NewReader(input), &stdout, &stderr)
	if err != nil {
		t.Fatalf("runFamilyInit: %v (stderr=%q)", err, stderr.String())
	}

	for _, name := range []string{"alice", "bob", "charlie", "family"} {
		cfg := filepath.Join(home, ".kittypaw", "accounts", name, "config.toml")
		if _, err := os.Stat(cfg); err != nil {
			t.Errorf("%s config.toml missing: %v", name, err)
		}
	}
	// No staging dirs left behind — InitAccount is atomic, but double-check
	// the cleanliness invariant from AC-W6.
	entries, _ := os.ReadDir(filepath.Join(home, ".kittypaw", "accounts"))
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".") && strings.HasSuffix(e.Name(), ".staging") {
			t.Errorf("staging dir leaked: %s", e.Name())
		}
	}
}

// --no-family: admin already has a family account elsewhere (or doesn't
// want one). The wizard must not create family/ in that case — AC-W5.
func TestRunFamilyInit_NoFamilyFlag(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	input := "alice\n" + validTok1 + "\n111\n\n"

	var stdout, stderr bytes.Buffer
	f := &familyInitFlags{max: 10, noFamily: true}
	if err := runFamilyInit(context.Background(), f, true,
		strings.NewReader(input), &stdout, &stderr); err != nil {
		t.Fatalf("runFamilyInit: %v", err)
	}

	if _, err := os.Stat(filepath.Join(home, ".kittypaw", "accounts", "alice")); err != nil {
		t.Errorf("alice should exist: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, ".kittypaw", "accounts", "family")); !os.IsNotExist(err) {
		t.Errorf("family should NOT exist under --no-family")
	}
}

// AC-W6: Ctrl-C before any member is prompted ends the wizard cleanly —
// no accounts created, no staging dirs left behind. The stronger mid-flow
// case (commit alice, Ctrl-C before bob) is covered by inspecting that
// InitAccount is self-cleaning (tested in core/); here we cover the
// wizard-level exit path.
func TestRunFamilyInit_InterruptCleansStaging(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // canceled before any input is read

	var stdout, stderr bytes.Buffer
	f := &familyInitFlags{max: 10, noFamily: false}

	// Any non-empty script — the ctx cancellation must take precedence.
	_ = runFamilyInit(ctx, f, true,
		strings.NewReader("alice\n"+validTok1+"\n111\n\n"), &stdout, &stderr)

	accountsDir := filepath.Join(home, ".kittypaw", "accounts")
	entries, _ := os.ReadDir(accountsDir)
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".") && strings.HasSuffix(e.Name(), ".staging") {
			t.Errorf("staging dir leaked: %s", e.Name())
		}
	}
}
