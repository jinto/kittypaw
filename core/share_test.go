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

	ownerBase = filepath.Join(root, "accounts", "family")
	if err := os.MkdirAll(filepath.Join(ownerBase, "memory"), 0o755); err != nil {
		t.Fatalf("mkdir owner: %v", err)
	}
	if err := os.WriteFile(filepath.Join(ownerBase, "memory", "weather.json"), []byte(`{"temp":18}`), 0o644); err != nil {
		t.Fatalf("write weather: %v", err)
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
		Share: map[string]ShareConfig{
			"alice": {Read: []string{"memory/weather.json"}},
		},
	}
	return ownerBase, outsideFile, cfg
}

// TestValidateSharedReadPath_Allowed pins the happy path: a reader listed
// in the owner's Share map, asking for a path in the allowlist, should
// receive the realpath to the file. Without this the whole feature is
// DoA — every cross-account read would silently fail.
func TestValidateSharedReadPath_Allowed(t *testing.T) {
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

// TestValidateSharedReadPath_TraversalMatrix is the anti-cutcorner guard.
// Every attack shape listed must be rejected at the API boundary — a single
// variant slipping through lets a hostile path string escape the account
// sandbox. The test IS the security contract.
func TestValidateSharedReadPath_TraversalMatrix(t *testing.T) {
	ownerBase, outsideFile, cfg := setupShareFixture(t)

	// Prep: symlink inside ownerBase that escapes to the outside file.
	escSymlink := filepath.Join(ownerBase, "esc.txt")
	if err := os.Symlink(outsideFile, escSymlink); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	// Add to allowlist so we isolate the symlink-rejection path from the
	// allowlist-rejection path.
	cfg.Share["alice"] = ShareConfig{Read: []string{
		"memory/weather.json",
		"esc.txt",
		"hard.txt",
		"memory/../memory/weather.json",
	}}

	// Prep: hardlink that cross-references an outside inode. On hfs/apfs
	// hardlinks across directories share st_nlink > 1 within the same
	// filesystem; t.TempDir allocates one filesystem so this works.
	hardPath := filepath.Join(ownerBase, "hard.txt")
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
		{"symlink escape", "alice", "esc.txt", ErrCrossAccountBoundary},
		{"hardlink escape", "alice", "hard.txt", ErrCrossAccountHardlink},
		{"unknown peer", "mallory", "memory/weather.json", ErrCrossAccountUnauthorized},
		{"allowlist miss", "alice", "memory/private.json", ErrCrossAccountNotAllowlisted},
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
// entry pointing at a deleted file should get ErrCrossAccountNotFound, not
// a boundary error. Distinct errors let the sandbox surface a useful
// message to the skill author ("ENOENT") vs. a policy message ("not
// allowed"). Conflating them has burned us before.
func TestValidateSharedReadPath_NotFound(t *testing.T) {
	ownerBase, _, cfg := setupShareFixture(t)
	cfg.Share["alice"] = ShareConfig{Read: []string{"memory/missing.json"}}

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
