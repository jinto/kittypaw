package server

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/jinto/kittypaw/core"
	"github.com/jinto/kittypaw/store"
)

// AC-DB: isOnboardingCompleted must treat a configured LLM key as "onboarded"
// even when the DB flag is missing. `kittypaw setup` (CLI) writes config.toml
// but deliberately does NOT set user_context.onboarding_completed — that flag
// belongs to the Web onboarding path. If this fallback regresses, CLI users
// will see Web setup screens re-open even after a successful `kittypaw setup`,
// and the G3 drop decision in the spec breaks. Either the fallback stays OR
// the CLI starts writing the flag; either fix must update this test.
func TestIsOnboardingCompleted_FallbackToLLMKey(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "kittypaw.db")
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer st.Close()

	// Sanity check: DB flag must NOT be set — this is the CLI-setup baseline.
	if _, ok, _ := st.GetUserContext("onboarding_completed"); ok {
		t.Fatal("fresh DB should not have onboarding_completed — test premise broken")
	}

	cfg := core.DefaultConfig()
	cfg.LLM.Provider = "anthropic"
	cfg.LLM.APIKey = "sk-test"
	cfg.LLM.Model = "claude-test"

	srv := &Server{
		config: &cfg,
		store:  st,
	}
	if !srv.isOnboardingCompleted() {
		t.Fatal("isOnboardingCompleted()=false with LLM.APIKey set — CLI parity broken")
	}

	// Sibling contract: no API key AND no DB flag → genuinely not onboarded.
	cfg.LLM.APIKey = ""
	cfg.LLM.BaseURL = ""
	if srv.isOnboardingCompleted() {
		t.Fatal("isOnboardingCompleted()=true with no DB flag and no LLM key — fallback is too loose")
	}
}

func TestSetupKakaoRegisterUsesDefaultAccountSecrets(t *testing.T) {
	root := t.TempDir()
	t.Setenv("KITTYPAW_CONFIG_DIR", root)

	dbPath := filepath.Join(root, "accounts", "alice", "data", "kittypaw.db")
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o700); err != nil {
		t.Fatalf("mkdir db dir: %v", err)
	}
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer st.Close()

	relay := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/register" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"token":"relay-token","pair_code":"123456","channel_url":"https://example.com/channel"}`))
	}))
	defer relay.Close()

	secrets, err := core.LoadAccountSecrets("alice")
	if err != nil {
		t.Fatalf("LoadAccountSecrets alice: %v", err)
	}
	mgr := core.NewAPITokenManager("", secrets)
	if err := mgr.SaveRelayURL(core.DefaultAPIServerURL, relay.URL); err != nil {
		t.Fatalf("SaveRelayURL: %v", err)
	}

	cfg := core.DefaultConfig()
	srv := &Server{
		config:          &cfg,
		store:           st,
		accountRegistry: core.NewAccountRegistry(filepath.Join(root, "accounts"), "alice"),
		accountList: []*core.Account{
			{ID: "alice", BaseDir: filepath.Join(root, "accounts", "alice"), Config: &cfg},
		},
	}

	req := httptest.NewRequest(http.MethodPost, "/api/setup/kakao/register", nil)
	rec := httptest.NewRecorder()
	srv.handleSetupKakaoRegister(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %q", rec.Code, rec.Body.String())
	}
	freshSecrets, err := core.LoadAccountSecrets("alice")
	if err != nil {
		t.Fatalf("reload alice secrets: %v", err)
	}
	freshMgr := core.NewAPITokenManager("", freshSecrets)
	if wsURL, ok := freshMgr.LoadKakaoRelayURL(core.DefaultAPIServerURL); !ok || wsURL == "" {
		t.Fatalf("alice Kakao relay URL = (%q, %v), want saved", wsURL, ok)
	}
	if _, err := os.Stat(filepath.Join(root, "accounts", core.DefaultAccountID, "secrets.json")); !os.IsNotExist(err) {
		t.Fatalf("default secrets should not be created, stat err=%v", err)
	}
}
