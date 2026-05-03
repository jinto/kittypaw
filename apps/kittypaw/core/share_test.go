package core

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// setupShareFixture creates an owner account layout on disk: baseDir with a
// real file plus an "outside" directory that hostile symlinks/hardlinks try
// to escape to. Every share test starts from this topology so the traversal
// checks exercise actual filesystem state rather than string manipulation.
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
		IsShared:  true,
		TeamSpace: TeamSpaceConfig{Members: []string{"alice"}},
	}
	return ownerBase, outsideFile, cfg
}

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
		IsShared:  true,
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

func TestValidateSharedReadPath_RejectsSymlinkedMemoryRootEscape(t *testing.T) {
	root := t.TempDir()
	ownerBase := filepath.Join(root, "accounts", "team")
	outsideMemory := filepath.Join(root, "outside-memory")
	if err := os.MkdirAll(ownerBase, 0o755); err != nil {
		t.Fatalf("mkdir owner: %v", err)
	}
	if err := os.MkdirAll(outsideMemory, 0o755); err != nil {
		t.Fatalf("mkdir outside memory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(outsideMemory, "weather.json"), []byte(`{"temp":99}`), 0o644); err != nil {
		t.Fatalf("write outside weather: %v", err)
	}
	if err := os.Symlink(outsideMemory, filepath.Join(ownerBase, "memory")); err != nil {
		t.Fatalf("symlink memory: %v", err)
	}
	cfg := &Config{
		IsShared:  true,
		TeamSpace: TeamSpaceConfig{Members: []string{"alice"}},
	}

	_, err := ValidateSharedReadPath(cfg, ownerBase, "alice", "memory/weather.json")
	if !errors.Is(err, ErrCrossAccountBoundary) {
		t.Errorf("symlinked memory root must reject as boundary escape, got %v", err)
	}
}

// TestValidateSharedReadPath_TraversalMatrix is the anti-cutcorner guard.
// Every attack shape listed must be rejected at the API boundary — a single
// variant slipping through lets a hostile path string escape the account
// sandbox. The test IS the security contract.
func TestValidateSharedReadPath_TraversalMatrix(t *testing.T) {
	ownerBase, outsideFile, cfg := setupShareFixture(t)

	// Prep: symlink inside ownerBase that escapes to the outside file.
	escSymlink := filepath.Join(ownerBase, "memory", "esc.txt")
	if err := os.Symlink(outsideFile, escSymlink); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	// Prep: hardlink that cross-references an outside inode. On hfs/apfs
	// hardlinks across directories share st_nlink > 1 within the same
	// filesystem; t.TempDir allocates one filesystem so this works.
	hardPath := filepath.Join(ownerBase, "memory", "hard.txt")
	if err := os.Link(outsideFile, hardPath); err != nil {
		t.Skipf("hardlink unsupported on this FS: %v", err)
	}

	cases := []struct {
		name    string
		reader  string
		req     string
		wantErr error
	}{
		{"absolute path", "alice", "/etc/passwd", ErrCrossAccountAbsolute},
		{"dotdot prefix", "alice", "../../etc/passwd", ErrCrossAccountTraversal},
		{"embedded dotdot", "alice", "memory/../../../etc/passwd", ErrCrossAccountTraversal},
		{"symlink escape", "alice", "memory/esc.txt", ErrCrossAccountBoundary},
		{"hardlink escape", "alice", "memory/hard.txt", ErrCrossAccountHardlink},
		{"unknown peer", "mallory", "memory/weather.json", ErrCrossAccountUnauthorized},
		{"not shareable", "alice", "private.json", ErrCrossAccountNotShareable},
		{"empty path", "alice", "", ErrCrossAccountPath},
		{"null byte", "alice", "memory/\x00/weather.json", ErrCrossAccountPath},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ValidateSharedReadPath(cfg, ownerBase, tc.reader, tc.req)
			if !errors.Is(err, tc.wantErr) {
				t.Errorf("req=%q reader=%q want %v, got %v", tc.req, tc.reader, tc.wantErr, err)
			}
		})
	}
}

// TestValidateSharedReadPath_NotFound splits the "file missing" signal
// from the "policy violation" signals — a reader with a legit allowlist
// membership request pointing at a deleted file should get ErrCrossAccountNotFound, not
// a boundary error. Distinct errors let the sandbox surface a useful
// message to the skill author ("ENOENT") vs. a policy message ("not
// allowed"). Conflating them has burned us before.
func TestValidateSharedReadPath_NotFound(t *testing.T) {
	ownerBase, _, cfg := setupShareFixture(t)

	_, err := ValidateSharedReadPath(cfg, ownerBase, "alice", "memory/missing.json")
	if !errors.Is(err, ErrCrossAccountNotFound) {
		t.Errorf("expected ErrCrossAccountNotFound, got %v", err)
	}
}

// TestValidateSharedReadPath_NilConfig enforces that an account with no
// Share map (the default for every personal account) rejects all reads.
// If this silently allowed reads, the fail-closed invariant breaks.
func TestValidateSharedReadPath_NilConfig(t *testing.T) {
	ownerBase, _, _ := setupShareFixture(t)
	_, err := ValidateSharedReadPath(&Config{}, ownerBase, "alice", "memory/weather.json")
	if !errors.Is(err, ErrCrossAccountUnauthorized) {
		t.Errorf("nil share must reject; got %v", err)
	}
}
