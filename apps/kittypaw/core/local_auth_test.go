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
	st := NewLocalAuthStore(filepath.Join(root, "auth.json"))

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

func TestLocalAuthStoreUnsupportedVersionReturnsError(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "auth.json")
	if err := os.WriteFile(path, []byte(`{"version":99,"users":{}}`), 0o600); err != nil {
		t.Fatalf("write auth file: %v", err)
	}

	st := NewLocalAuthStore(path)
	if ok, err := st.VerifyPassword("alice", "pw"); err == nil || ok {
		t.Fatalf("VerifyPassword unsupported version = (%v, %v), want false error", ok, err)
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

func TestLocalAuthStoreConcurrentCreateAllowsSingleWinner(t *testing.T) {
	root := t.TempDir()
	st := NewLocalAuthStore(filepath.Join(root, "auth.json"))

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
	body := `{"version":1,"users":{"alice":{"account_id":"alice","password_hash":"argon2id$v=1$m=65536,t=3,p=5$` + salt + `$` + hash + `","created_at":"2026-04-30T00:00:00Z","updated_at":"2026-04-30T00:00:00Z"}}}`
	path := filepath.Join(root, "auth.json")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write auth file: %v", err)
	}

	st := NewLocalAuthStore(path)
	if ok, err := st.VerifyPassword("alice", "pw"); err == nil || ok {
		t.Fatalf("VerifyPassword with unexpected params = (%v, %v), want false error", ok, err)
	}
}

func TestLocalAuthStoreRejectsUnexpectedHashComponentLengths(t *testing.T) {
	root := t.TempDir()
	salt := base64.RawStdEncoding.EncodeToString(make([]byte, 17))
	hash := base64.RawStdEncoding.EncodeToString(make([]byte, 32))
	body := `{"version":1,"users":{"alice":{"account_id":"alice","password_hash":"argon2id$v=1$m=65536,t=3,p=4$` + salt + `$` + hash + `","created_at":"2026-04-30T00:00:00Z","updated_at":"2026-04-30T00:00:00Z"}}}`
	path := filepath.Join(root, "auth.json")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write auth file: %v", err)
	}

	st := NewLocalAuthStore(path)
	if ok, err := st.VerifyPassword("alice", "pw"); err == nil || ok {
		t.Fatalf("VerifyPassword with unexpected salt length = (%v, %v), want false error", ok, err)
	}

	salt = base64.RawStdEncoding.EncodeToString(make([]byte, 16))
	hash = base64.RawStdEncoding.EncodeToString(make([]byte, 33))
	body = `{"version":1,"users":{"alice":{"account_id":"alice","password_hash":"argon2id$v=1$m=65536,t=3,p=4$` + salt + `$` + hash + `","created_at":"2026-04-30T00:00:00Z","updated_at":"2026-04-30T00:00:00Z"}}}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write auth file: %v", err)
	}
	if ok, err := st.VerifyPassword("alice", "pw"); err == nil || ok {
		t.Fatalf("VerifyPassword with unexpected hash length = (%v, %v), want false error", ok, err)
	}
}

func TestLocalAuthStoreRejectsDuplicateArgonParams(t *testing.T) {
	root := t.TempDir()
	salt := base64.RawStdEncoding.EncodeToString(make([]byte, 16))
	hash := base64.RawStdEncoding.EncodeToString(make([]byte, 32))
	body := `{"version":1,"users":{"alice":{"account_id":"alice","password_hash":"argon2id$v=1$m=65536,t=3,p=4,p=4$` + salt + `$` + hash + `","created_at":"2026-04-30T00:00:00Z","updated_at":"2026-04-30T00:00:00Z"}}}`
	path := filepath.Join(root, "auth.json")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write auth file: %v", err)
	}

	st := NewLocalAuthStore(path)
	if ok, err := st.VerifyPassword("alice", "pw"); err == nil || ok {
		t.Fatalf("VerifyPassword with duplicate params = (%v, %v), want false error", ok, err)
	}
}

func TestLocalAuthStoreReplacesInsecureStaleTempFile(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "auth.json")
	if err := os.WriteFile(path+".tmp", []byte("stale"), 0o644); err != nil {
		t.Fatalf("write stale temp: %v", err)
	}

	st := NewLocalAuthStore(path)
	if err := st.CreateUser("alice", "pw"); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat auth file: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("auth file mode = %v, want 0600", got)
	}
	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Fatalf("stale fixed temp still exists or stat failed with unexpected error: %v", err)
	}
}
