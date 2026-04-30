package core

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLocalAuthStoreCreateAndVerify(t *testing.T) {
	root := t.TempDir()
	st := NewLocalAuthStore(filepath.Join(root, "auth.json"))

	if err := st.CreateUser("alice", "correct horse battery staple"); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if ok, err := st.VerifyPassword("alice", "correct horse battery staple"); err != nil || !ok {
		t.Fatalf("VerifyPassword good = (%v, %v), want true nil", ok, err)
	}
	if ok, err := st.VerifyPassword("alice", "wrong"); err != nil || ok {
		t.Fatalf("VerifyPassword bad = (%v, %v), want false nil", ok, err)
	}
}

func TestLocalAuthStoreRejectsDuplicateAndInvalidID(t *testing.T) {
	root := t.TempDir()
	st := NewLocalAuthStore(filepath.Join(root, "auth.json"))

	if err := st.CreateUser("alice", "pw"); err != nil {
		t.Fatalf("CreateUser alice: %v", err)
	}
	if err := st.CreateUser("alice", "pw2"); !errors.Is(err, ErrLocalUserExists) {
		t.Fatalf("duplicate err = %v, want ErrLocalUserExists", err)
	}
	if err := st.CreateUser("../bad", "pw"); err == nil {
		t.Fatal("CreateUser accepted invalid account id")
	}
	if err := st.CreateUser("bob", ""); err == nil {
		t.Fatal("CreateUser accepted empty password")
	}
}

func TestLocalAuthStoreDisabledUserCannotLogin(t *testing.T) {
	root := t.TempDir()
	st := NewLocalAuthStore(filepath.Join(root, "auth.json"))
	if err := st.CreateUser("alice", "pw"); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if err := st.SetDisabled("alice", true); err != nil {
		t.Fatalf("SetDisabled: %v", err)
	}
	if ok, err := st.VerifyPassword("alice", "pw"); err != nil || ok {
		t.Fatalf("disabled VerifyPassword = (%v, %v), want false nil", ok, err)
	}
}

func TestLocalAuthStoreCorruptJSONReturnsError(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "auth.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatalf("write corrupt auth file: %v", err)
	}

	st := NewLocalAuthStore(path)
	if ok, err := st.VerifyPassword("alice", "pw"); err == nil || ok {
		t.Fatalf("VerifyPassword corrupt = (%v, %v), want false error", ok, err)
	} else if !strings.Contains(err.Error(), "parse local auth store") {
		t.Fatalf("error = %v, want parse local auth store", err)
	}
}

func TestLocalAuthStoreWritesSecureArgon2File(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "nested", "auth.json")
	st := NewLocalAuthStore(path)

	if err := st.CreateUser("alice", "secret-password"); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	parentInfo, err := os.Stat(filepath.Dir(path))
	if err != nil {
		t.Fatalf("stat parent: %v", err)
	}
	if got := parentInfo.Mode().Perm(); got != 0o700 {
		t.Fatalf("parent mode = %v, want 0700", got)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat auth file: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("auth file mode = %v, want 0600", got)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read auth file: %v", err)
	}
	text := string(raw)
	if strings.Contains(text, "secret-password") {
		t.Fatal("auth file contains plaintext password")
	}
	if !strings.Contains(text, "argon2id$v=1$m=65536,t=3,p=4$") {
		t.Fatalf("auth file missing argon2id hash marker: %s", text)
	}
	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Fatalf("temp file still exists or stat failed with unexpected error: %v", err)
	}
}
