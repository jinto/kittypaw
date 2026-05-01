package core

import (
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestLocalAuthStoreCreateAndVerify(t *testing.T) {
	root := t.TempDir()
	st := NewLocalAuthStore(filepath.Join(root, "accounts"))

	if exists, err := st.HasUser("alice"); err != nil || exists {
		t.Fatalf("HasUser before create = (%v, %v), want false nil", exists, err)
	}
	if err := st.CreateUser("alice", "correct horse battery staple"); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if exists, err := st.HasUser("alice"); err != nil || !exists {
		t.Fatalf("HasUser after create = (%v, %v), want true nil", exists, err)
	}
	if ok, err := st.VerifyPassword("alice", "correct horse battery staple"); err != nil || !ok {
		t.Fatalf("VerifyPassword good = (%v, %v), want true nil", ok, err)
	}
	if ok, err := st.VerifyPassword("alice", "wrong"); err != nil || ok {
		t.Fatalf("VerifyPassword bad = (%v, %v), want false nil", ok, err)
	}
}

func TestLocalAuthStoreWritesPerAccountToml(t *testing.T) {
	root := t.TempDir()
	st := NewLocalAuthStore(filepath.Join(root, "accounts"))

	if err := st.CreateUser("alice", "secret-password"); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	accountPath := filepath.Join(root, "accounts", "alice", "account.toml")
	if _, err := os.Stat(filepath.Join(root, "auth.json")); !os.IsNotExist(err) {
		t.Fatalf("root auth.json must not exist, stat err=%v", err)
	}
	info, err := os.Stat(accountPath)
	if err != nil {
		t.Fatalf("stat account.toml: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("account.toml mode = %v, want 0600", got)
	}

	raw, err := os.ReadFile(accountPath)
	if err != nil {
		t.Fatalf("read account.toml: %v", err)
	}
	text := string(raw)
	if strings.Contains(text, "account_id") {
		t.Fatalf("account.toml must not duplicate folder account id:\n%s", text)
	}
	if !strings.Contains(text, "password_hash") {
		t.Fatalf("account.toml missing password_hash:\n%s", text)
	}
}

func TestLocalAuthStoreRejectsDuplicateAndInvalidID(t *testing.T) {
	root := t.TempDir()
	st := NewLocalAuthStore(filepath.Join(root, "accounts"))

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
	st := NewLocalAuthStore(filepath.Join(root, "accounts"))
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

func TestLocalAuthStoreDeleteUser(t *testing.T) {
	root := t.TempDir()
	st := NewLocalAuthStore(filepath.Join(root, "accounts"))
	if err := st.CreateUser("alice", "pw"); err != nil {
		t.Fatalf("CreateUser alice: %v", err)
	}
	if err := st.CreateUser("bob", "pw"); err != nil {
		t.Fatalf("CreateUser bob: %v", err)
	}

	if err := st.DeleteUser("alice"); err != nil {
		t.Fatalf("DeleteUser: %v", err)
	}
	if ok, err := st.VerifyPassword("alice", "pw"); err != nil || ok {
		t.Fatalf("deleted VerifyPassword = (%v, %v), want false nil", ok, err)
	}
	if ok, err := st.VerifyPassword("bob", "pw"); err != nil || !ok {
		t.Fatalf("bob VerifyPassword = (%v, %v), want true nil", ok, err)
	}
	if err := st.DeleteUser("missing"); err != nil {
		t.Fatalf("DeleteUser missing should be idempotent: %v", err)
	}
	if err := st.DeleteUser("../bad"); err == nil {
		t.Fatal("DeleteUser accepted invalid account id")
	}
}

func TestLocalAuthStoreCorruptJSONReturnsError(t *testing.T) {
	root := t.TempDir()
	writeLocalAccountAuth(t, root, "alice", "{not toml")

	st := NewLocalAuthStore(filepath.Join(root, "accounts"))
	if ok, err := st.VerifyPassword("alice", "pw"); err == nil || ok {
		t.Fatalf("VerifyPassword corrupt = (%v, %v), want false error", ok, err)
	} else if !strings.Contains(err.Error(), "parse local account auth") {
		t.Fatalf("error = %v, want parse local account auth", err)
	}
}

func TestLocalAuthStoreUnsupportedVersionReturnsError(t *testing.T) {
	root := t.TempDir()
	writeLocalAccountAuth(t, root, "alice", `version = 99`)

	st := NewLocalAuthStore(filepath.Join(root, "accounts"))
	if ok, err := st.VerifyPassword("alice", "pw"); err == nil || ok {
		t.Fatalf("VerifyPassword unsupported version = (%v, %v), want false error", ok, err)
	}
}

func TestLocalAuthStoreWritesSecureArgon2File(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "accounts")
	st := NewLocalAuthStore(path)

	if err := st.CreateUser("alice", "secret-password"); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	accountPath := filepath.Join(path, "alice", "account.toml")
	parentInfo, err := os.Stat(filepath.Dir(accountPath))
	if err != nil {
		t.Fatalf("stat parent: %v", err)
	}
	if got := parentInfo.Mode().Perm(); got != 0o700 {
		t.Fatalf("parent mode = %v, want 0700", got)
	}

	info, err := os.Stat(accountPath)
	if err != nil {
		t.Fatalf("stat auth file: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("auth file mode = %v, want 0600", got)
	}

	raw, err := os.ReadFile(accountPath)
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
	if _, err := os.Stat(accountPath + ".tmp"); !os.IsNotExist(err) {
		t.Fatalf("temp file still exists or stat failed with unexpected error: %v", err)
	}
}

func TestLocalAuthStoreConcurrentCreateAllowsSingleWinner(t *testing.T) {
	root := t.TempDir()
	st := NewLocalAuthStore(filepath.Join(root, "accounts"))

	const workers = 8
	start := make(chan struct{})
	errs := make(chan error, workers)
	var wg sync.WaitGroup
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			errs <- st.CreateUser("alice", "pw")
		}()
	}
	close(start)
	wg.Wait()
	close(errs)

	successes := 0
	duplicates := 0
	for err := range errs {
		switch {
		case err == nil:
			successes++
		case errors.Is(err, ErrLocalUserExists):
			duplicates++
		default:
			t.Fatalf("unexpected CreateUser error: %v", err)
		}
	}
	if successes != 1 || duplicates != workers-1 {
		t.Fatalf("successes=%d duplicates=%d, want 1 and %d", successes, duplicates, workers-1)
	}
}

func TestLocalAuthStoreRejectsUnexpectedArgonParams(t *testing.T) {
	root := t.TempDir()
	salt := base64.RawStdEncoding.EncodeToString(make([]byte, 16))
	hash := base64.RawStdEncoding.EncodeToString(make([]byte, 32))
	writeLocalAccountAuth(t, root, "alice", accountAuthBody("argon2id$v=1$m=65536,t=3,p=5$"+salt+"$"+hash))

	st := NewLocalAuthStore(filepath.Join(root, "accounts"))
	if ok, err := st.VerifyPassword("alice", "pw"); err == nil || ok {
		t.Fatalf("VerifyPassword with unexpected params = (%v, %v), want false error", ok, err)
	}
}

func TestLocalAuthStoreRejectsUnexpectedHashComponentLengths(t *testing.T) {
	root := t.TempDir()
	salt := base64.RawStdEncoding.EncodeToString(make([]byte, 17))
	hash := base64.RawStdEncoding.EncodeToString(make([]byte, 32))
	writeLocalAccountAuth(t, root, "alice", accountAuthBody("argon2id$v=1$m=65536,t=3,p=4$"+salt+"$"+hash))

	st := NewLocalAuthStore(filepath.Join(root, "accounts"))
	if ok, err := st.VerifyPassword("alice", "pw"); err == nil || ok {
		t.Fatalf("VerifyPassword with unexpected salt length = (%v, %v), want false error", ok, err)
	}

	salt = base64.RawStdEncoding.EncodeToString(make([]byte, 16))
	hash = base64.RawStdEncoding.EncodeToString(make([]byte, 33))
	writeLocalAccountAuth(t, root, "alice", accountAuthBody("argon2id$v=1$m=65536,t=3,p=4$"+salt+"$"+hash))
	if ok, err := st.VerifyPassword("alice", "pw"); err == nil || ok {
		t.Fatalf("VerifyPassword with unexpected hash length = (%v, %v), want false error", ok, err)
	}
}

func TestLocalAuthStoreRejectsDuplicateArgonParams(t *testing.T) {
	root := t.TempDir()
	salt := base64.RawStdEncoding.EncodeToString(make([]byte, 16))
	hash := base64.RawStdEncoding.EncodeToString(make([]byte, 32))
	writeLocalAccountAuth(t, root, "alice", accountAuthBody("argon2id$v=1$m=65536,t=3,p=4,p=4$"+salt+"$"+hash))

	st := NewLocalAuthStore(filepath.Join(root, "accounts"))
	if ok, err := st.VerifyPassword("alice", "pw"); err == nil || ok {
		t.Fatalf("VerifyPassword with duplicate params = (%v, %v), want false error", ok, err)
	}
}

func TestLocalAuthStoreReplacesInsecureStaleTempFile(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "accounts")
	stalePath := filepath.Join(path, "alice", "account.toml.tmp")
	if err := os.MkdirAll(filepath.Dir(stalePath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(stalePath, []byte("stale"), 0o644); err != nil {
		t.Fatalf("write stale temp: %v", err)
	}

	st := NewLocalAuthStore(path)
	if err := st.CreateUser("alice", "pw"); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	accountPath := filepath.Join(path, "alice", "account.toml")
	info, err := os.Stat(accountPath)
	if err != nil {
		t.Fatalf("stat auth file: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("auth file mode = %v, want 0600", got)
	}
	if _, err := os.Stat(accountPath + ".tmp"); !os.IsNotExist(err) {
		t.Fatalf("stale fixed temp still exists or stat failed with unexpected error: %v", err)
	}
}

func writeLocalAccountAuth(t *testing.T, root, accountID, body string) {
	t.Helper()
	dir := filepath.Join(root, "accounts", accountID)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "account.toml"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func accountAuthBody(hash string) string {
	return `version = 1
password_hash = "` + hash + `"
created_at = 2026-04-30T00:00:00Z
updated_at = 2026-04-30T00:00:00Z
`
}
