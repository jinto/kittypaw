package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jinto/kittypaw/core"
)

func TestChatBootstrapReturnsChatOnlyContract(t *testing.T) {
	cfg := core.DefaultConfig()
	cfg.Server.APIKey = "account-key"
	srv := newServerWithLocalUserAndConfig(t, "alice", "pw", &cfg)
	cookie := loginSessionCookie(t, srv, "alice", "pw")

	req := httptest.NewRequest(http.MethodGet, "/api/chat/bootstrap", nil)
	req.RemoteAddr = "203.0.113.10:4321"
	req.Host = "chat.example.test"
	req.AddCookie(cookie)
	rr := httptest.NewRecorder()
	srv.setupRoutes().ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("chat bootstrap code = %d body=%s", rr.Code, rr.Body.String())
	}
	var body map[string]any
	if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
		t.Fatalf("decode chat bootstrap: %v", err)
	}
	if _, ok := body["api_key"]; ok {
		t.Fatalf("chat bootstrap exposed control api_key: %s", rr.Body.String())
	}
	if got := body["ws_url"]; got != "ws://chat.example.test/chat/ws" {
		t.Fatalf("ws_url = %v, want chat-scoped websocket", got)
	}
	if got := body["account_id"]; got != "alice" {
		t.Fatalf("account_id = %v, want alice", got)
	}
	if got := body["is_default"]; got != true {
		t.Fatalf("is_default = %v, want true", got)
	}
}

func TestChatBootstrapAllowsLocalFirstRunWithoutControlToken(t *testing.T) {
	root := t.TempDir()
	cfg := core.DefaultConfig()
	cfg.Server.APIKey = "account-key"
	srv := newAuthTestServer(t, root, "alice", &cfg)

	req := httptest.NewRequest(http.MethodGet, "/api/chat/bootstrap", nil)
	req.RemoteAddr = "127.0.0.1:1234"
	req.Host = "127.0.0.1:3000"
	rr := httptest.NewRecorder()
	srv.setupRoutes().ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("local first-run chat bootstrap code = %d body=%s", rr.Code, rr.Body.String())
	}
	if strings.Contains(rr.Body.String(), "account-key") || strings.Contains(rr.Body.String(), "api_key") {
		t.Fatalf("local first-run chat bootstrap leaked control token: %s", rr.Body.String())
	}
}

func TestChatBootstrapRejectsRemoteFirstRunWithoutSession(t *testing.T) {
	root := t.TempDir()
	srv := newAuthTestServer(t, root, "alice", &core.Config{})

	req := httptest.NewRequest(http.MethodGet, "/api/chat/bootstrap", nil)
	req.RemoteAddr = "203.0.113.10:4321"
	req.Host = "chat.example.test"
	rr := httptest.NewRecorder()
	srv.setupRoutes().ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("remote first-run chat bootstrap code = %d, want 403; body=%s", rr.Code, rr.Body.String())
	}
}
