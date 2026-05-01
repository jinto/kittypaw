package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jinto/kittypaw/core"
)

func TestSettingsLLMUpdatesCompletedAccountConfig(t *testing.T) {
	cfg := core.DefaultConfig()
	cfg.LLM.Provider = "anthropic"
	cfg.LLM.APIKey = "old-key"
	cfg.LLM.Model = "old-model"
	srv := newServerWithLocalUserAndConfig(t, "alice", "pw", &cfg)
	cookie := loginSessionCookie(t, srv, "alice", "pw")

	req := httptest.NewRequest(http.MethodPost, "/api/settings/llm", strings.NewReader(`{
		"provider":"local",
		"local_url":"http://localhost:11434/v1",
		"local_model":"llama3.1"
	}`))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(cookie)
	rr := httptest.NewRecorder()
	srv.setupRoutes().ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("settings llm code = %d body=%s", rr.Code, rr.Body.String())
	}

	cfgPath, err := core.ConfigPathForAccount("alice")
	if err != nil {
		t.Fatalf("config path: %v", err)
	}
	written, err := core.LoadConfig(cfgPath)
	if err != nil {
		t.Fatalf("load written config: %v", err)
	}
	if written.LLM.Provider != "openai" || written.LLM.APIKey != "" || written.LLM.Model != "llama3.1" {
		t.Fatalf("written LLM = %#v, want local openai-compatible llama3.1 without API key", written.LLM)
	}
	if got := srv.accounts.Session("alice").Config.LLM.Model; got != "llama3.1" {
		t.Fatalf("runtime LLM model = %q, want llama3.1", got)
	}
}

func TestSettingsTelegramUpdatesCompletedAccountConfig(t *testing.T) {
	cfg := core.DefaultConfig()
	cfg.LLM.Provider = "anthropic"
	cfg.LLM.APIKey = "configured"
	cfg.LLM.Model = "claude-test"
	srv := newServerWithLocalUserAndConfig(t, "alice", "pw", &cfg)
	cookie := loginSessionCookie(t, srv, "alice", "pw")

	body, err := json.Marshal(map[string]string{
		"bot_token": "123456:ABCDEFGHIJKLMNOPQRSTUVWXYZabcd",
		"chat_id":   "4242",
	})
	if err != nil {
		t.Fatalf("marshal telegram body: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/settings/telegram", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(cookie)
	rr := httptest.NewRecorder()
	srv.setupRoutes().ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("settings telegram code = %d body=%s", rr.Code, rr.Body.String())
	}

	written, err := core.LoadConfig(filepath.Join(srv.accountDepsForID("alice").Account.BaseDir, "config.toml"))
	if err != nil {
		t.Fatalf("load written config: %v", err)
	}
	if len(written.Channels) != 1 || written.Channels[0].ChannelType != core.ChannelTelegram {
		t.Fatalf("channels = %#v, want one telegram channel", written.Channels)
	}
	if got := written.AdminChatIDs; len(got) != 1 || got[0] != "4242" {
		t.Fatalf("admin chat IDs = %#v, want [4242]", got)
	}
}

func TestSettingsRejectsBeforeCLISetup(t *testing.T) {
	root := t.TempDir()
	srv := newAuthTestServer(t, root, "alice", &core.Config{})

	req := httptest.NewRequest(http.MethodPost, "/api/settings/llm", strings.NewReader(`{"provider":"local","local_model":"llama3"}`))
	req.RemoteAddr = "127.0.0.1:1234"
	rr := httptest.NewRecorder()
	srv.setupRoutes().ServeHTTP(rr, req)
	if rr.Code != http.StatusConflict {
		t.Fatalf("settings before setup code = %d, want 409; body=%s", rr.Code, rr.Body.String())
	}
}

func TestSettingsWorkspacesUseLoggedInAccount(t *testing.T) {
	aliceCfg := core.DefaultConfig()
	aliceCfg.LLM.Provider = "anthropic"
	aliceCfg.LLM.APIKey = "alice-key"
	bobCfg := core.DefaultConfig()
	bobCfg.LLM.Provider = "anthropic"
	bobCfg.LLM.APIKey = "bob-key"
	srv := newMultiAccountAuthTestServer(t, "alice", map[string]string{
		"alice": "alice-pw",
		"bob":   "bob-pw",
	}, map[string]*core.Config{
		"alice": &aliceCfg,
		"bob":   &bobCfg,
	})

	workspaceDir := t.TempDir()
	body, err := json.Marshal(map[string]string{
		"name": "notes",
		"path": workspaceDir,
	})
	if err != nil {
		t.Fatalf("marshal workspace body: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/settings/workspaces", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(loginSessionCookie(t, srv, "bob", "bob-pw"))
	rr := httptest.NewRecorder()
	srv.setupRoutes().ServeHTTP(rr, req)
	if rr.Code != http.StatusCreated {
		t.Fatalf("settings workspace create code = %d body=%s", rr.Code, rr.Body.String())
	}
	canonicalWorkspaceDir, err := filepath.EvalSymlinks(filepath.Clean(workspaceDir))
	if err != nil {
		t.Fatalf("canonicalize workspace dir: %v", err)
	}

	bobWorkspaces, err := srv.accountDepsForID("bob").Store.ListWorkspaces()
	if err != nil {
		t.Fatalf("list bob workspaces: %v", err)
	}
	if len(bobWorkspaces) != 1 || bobWorkspaces[0].Name != "notes" || bobWorkspaces[0].RootPath != canonicalWorkspaceDir {
		t.Fatalf("bob workspaces = %#v, want notes at %s", bobWorkspaces, canonicalWorkspaceDir)
	}
	aliceWorkspaces, err := srv.accountDepsForID("alice").Store.ListWorkspaces()
	if err != nil {
		t.Fatalf("list alice workspaces: %v", err)
	}
	if len(aliceWorkspaces) != 0 {
		t.Fatalf("alice workspaces = %#v, want none", aliceWorkspaces)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/settings/workspaces", nil)
	req.AddCookie(loginSessionCookie(t, srv, "bob", "bob-pw"))
	rr = httptest.NewRecorder()
	srv.setupRoutes().ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("settings workspace list code = %d body=%s", rr.Code, rr.Body.String())
	}
	var listed []struct {
		Name     string `json:"name"`
		RootPath string `json:"root_path"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&listed); err != nil {
		t.Fatalf("decode workspace list: %v", err)
	}
	if len(listed) != 1 || listed[0].Name != "notes" || listed[0].RootPath != canonicalWorkspaceDir {
		t.Fatalf("listed workspaces = %#v, want bob notes workspace", listed)
	}
}
