package server

import (
	"context"
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
		OpenAIHandler: openai.NewHandler(nilAuth{}, nilBroker{}),
	})

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	if rr.Body.String() != `{"status":"healthy","version":"dev"}`+"\n" {
		t.Fatalf("body = %q", rr.Body.String())
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
