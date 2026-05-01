package server

import (
	"context"
	"net/http"
	"net/http/httptest"
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

type nilAuth struct{}

func (nilAuth) Authenticate(*http.Request) (openai.Principal, error) {
	return openai.Principal{}, openai.ErrUnauthorized
}

type nilBroker struct{}

func (nilBroker) Request(context.Context, broker.Request) (<-chan protocol.Frame, error) {
	return nil, broker.ErrDeviceOffline
}
