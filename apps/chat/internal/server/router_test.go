package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/kittypaw-app/kittychat/internal/broker"
	"github.com/kittypaw-app/kittychat/internal/openai"
	"github.com/kittypaw-app/kittychat/internal/protocol"
)

func TestRouterHealth(t *testing.T) {
	router := NewRouter(Config{
		Version:       "dev",
		Commit:        "abc1234",
		OpenAIHandler: openai.NewHandler(nilAuth{}, nilBroker{}),
	})

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	var body map[string]string
	if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body["status"] != "healthy" {
		t.Fatalf("status = %q, want healthy", body["status"])
	}
	if body["version"] != "dev" {
		t.Fatalf("version = %q, want dev", body["version"])
	}
	if body["commit"] != "abc1234" {
		t.Fatalf("commit = %q, want abc1234", body["commit"])
	}
}

func TestRouterServesHostedChatEntry(t *testing.T) {
	router := NewRouter(Config{
		Version:       "dev",
		OpenAIHandler: openai.NewHandler(nilAuth{}, nilBroker{}),
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	if ct := rr.Header().Get("Content-Type"); ct != "text/html; charset=utf-8" {
		t.Fatalf("content-type = %q, want html", ct)
	}
	if cc := rr.Header().Get("Cache-Control"); cc != "no-store" {
		t.Fatalf("cache-control = %q, want no-store", cc)
	}
	if csp := rr.Header().Get("Content-Security-Policy"); !strings.Contains(csp, "default-src 'self'") {
		t.Fatalf("content-security-policy = %q, want default-src self", csp)
	}
	if body := rr.Body.String(); !strings.Contains(body, `id="chat-entry"`) {
		t.Fatalf("hosted entry marker missing from body:\n%s", body)
	}
	if body := rr.Body.String(); !strings.Contains(body, `/assets/entry.js`) {
		t.Fatalf("entry script missing from body:\n%s", body)
	}
	if body := rr.Body.String(); !strings.Contains(body, `href="/auth/login/google"`) {
		t.Fatalf("login link should target chat BFF login route:\n%s", body)
	}
	if body := rr.Body.String(); strings.Contains(body, `disabled`) {
		t.Fatalf("login link should be enabled now that PKCE BFF is live:\n%s", body)
	}
	if body := rr.Body.String(); strings.Contains(body, `class="secondary-action" href="/manual/"`) {
		t.Fatalf("manual qa link should not render as a secondary button:\n%s", body)
	}
	if body := rr.Body.String(); !strings.Contains(body, `class="operator-link" href="/manual/"`) {
		t.Fatalf("manual qa should render as a small operator link:\n%s", body)
	}
}

func TestRouterServesHostedChatApp(t *testing.T) {
	router := NewRouter(Config{
		Version:       "dev",
		OpenAIHandler: openai.NewHandler(nilAuth{}, nilBroker{}),
	})

	req := httptest.NewRequest(http.MethodGet, "/app/", nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	if body := rr.Body.String(); !strings.Contains(body, `id="chat-app"`) {
		t.Fatalf("hosted app marker missing from body:\n%s", body)
	}
	if body := rr.Body.String(); !strings.Contains(body, `/assets/app.js`) {
		t.Fatalf("app script missing from body:\n%s", body)
	}
}

func TestRouterDoesNotServeStaticAuthCallbackWithoutWebHandler(t *testing.T) {
	router := NewRouter(Config{
		Version:       "dev",
		OpenAIHandler: openai.NewHandler(nilAuth{}, nilBroker{}),
	})

	req := httptest.NewRequest(http.MethodGet, "/auth/callback", nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rr.Code)
	}
}

func TestRouterServesHostedChatAssets(t *testing.T) {
	router := NewRouter(Config{
		Version:       "dev",
		OpenAIHandler: openai.NewHandler(nilAuth{}, nilBroker{}),
	})

	req := httptest.NewRequest(http.MethodGet, "/assets/shared.js", nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	if ct := rr.Header().Get("Content-Type"); !strings.Contains(ct, "javascript") {
		t.Fatalf("content-type = %q, want javascript", ct)
	}
	if cc := rr.Header().Get("Cache-Control"); cc != "no-store" {
		t.Fatalf("cache-control = %q, want no-store", cc)
	}
	if body := rr.Body.String(); !strings.Contains(body, "formatHTTPError") {
		t.Fatalf("hosted shared helper missing from body:\n%s", body)
	}
}

func TestRouterDoesNotExposeHostedHTMLAsAsset(t *testing.T) {
	router := NewRouter(Config{
		Version:       "dev",
		OpenAIHandler: openai.NewHandler(nilAuth{}, nilBroker{}),
	})

	req := httptest.NewRequest(http.MethodGet, "/assets/index.html", nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rr.Code)
	}
}

func TestRouterServesManualChatUI(t *testing.T) {
	router := NewRouter(Config{
		Version:       "dev",
		OpenAIHandler: openai.NewHandler(nilAuth{}, nilBroker{}),
	})

	req := httptest.NewRequest(http.MethodGet, "/manual/", nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	if ct := rr.Header().Get("Content-Type"); ct != "text/html; charset=utf-8" {
		t.Fatalf("content-type = %q, want html", ct)
	}
	if body := rr.Body.String(); !strings.Contains(body, `id="manual-chat-app"`) {
		t.Fatalf("manual ui marker missing from body:\n%s", body)
	}
	if body := rr.Body.String(); !strings.Contains(body, `placeholder="Paste KITTYCHAT_API_TOKEN"`) {
		t.Fatalf("token placeholder missing from body:\n%s", body)
	}
}

func TestRouterServesManualChatAssets(t *testing.T) {
	router := NewRouter(Config{
		Version:       "dev",
		OpenAIHandler: openai.NewHandler(nilAuth{}, nilBroker{}),
	})

	req := httptest.NewRequest(http.MethodGet, "/manual/app.js", nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	if ct := rr.Header().Get("Content-Type"); !strings.Contains(ct, "javascript") {
		t.Fatalf("content-type = %q, want javascript", ct)
	}
	if cc := rr.Header().Get("Cache-Control"); cc != "no-store" {
		t.Fatalf("cache-control = %q, want no-store", cc)
	}
	if body := rr.Body.String(); !strings.Contains(body, "formatHTTPError") {
		t.Fatalf("manual app error formatter missing from body:\n%s", body)
	}
}

type nilAuth struct{}

func (nilAuth) Authenticate(*http.Request) (openai.Principal, error) {
	return openai.Principal{}, openai.ErrUnauthorized
}

type nilBroker struct{}

func (nilBroker) Request(context.Context, broker.Request) (<-chan protocol.Frame, error) {
	return nil, broker.ErrDeviceOffline
}

func (nilBroker) Routes(string) []broker.Route {
	return nil
}
