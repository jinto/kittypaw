package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jinto/kittypaw/core"
)

func TestApplyDiscoveryStoresChatRelayURL(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/discovery" {
			t.Fatalf("path = %s, want /discovery", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{
			"api_base_url":"https://api.kittypaw.app",
			"auth_base_url":"https://api.kittypaw.app/auth",
			"chat_relay_url":"https://chat.kittypaw.app",
			"kakao_relay_url":"https://kakao.kittypaw.app",
			"skills_registry_url":"https://github.com/kittypaw-app/skills"
		}`)
	}))
	defer ts.Close()

	t.Setenv("KITTYPAW_CONFIG_DIR", t.TempDir())
	secrets, err := core.LoadAccountSecrets("alice")
	if err != nil {
		t.Fatalf("LoadAccountSecrets: %v", err)
	}
	mgr := core.NewAPITokenManager("", secrets)

	gotAPIBase := applyDiscovery(ts.URL, mgr)
	if gotAPIBase != "https://api.kittypaw.app" {
		t.Fatalf("applyDiscovery returned API base = %q", gotAPIBase)
	}
	gotChatRelay, ok := mgr.LoadChatRelayURL(ts.URL)
	if !ok || gotChatRelay != "https://chat.kittypaw.app" {
		t.Fatalf("LoadChatRelayURL = (%q, %v), want chat relay URL", gotChatRelay, ok)
	}
	gotAuthBase, ok := mgr.LoadAuthBaseURL(ts.URL)
	if !ok || gotAuthBase != "https://api.kittypaw.app/auth" {
		t.Fatalf("LoadAuthBaseURL = (%q, %v), want auth base URL", gotAuthBase, ok)
	}
}
