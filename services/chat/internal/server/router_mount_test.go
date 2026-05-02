package server

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
)

func TestRouterMountsDaemonHandler(t *testing.T) {
	router := NewRouter(Config{
		Version: "dev",
		DaemonHandler: fixedHandler{
			path: "/connect",
			body: "daemon",
		},
	})

	req := httptest.NewRequest(http.MethodGet, "/daemon/connect", nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	if rr.Body.String() != "daemon" {
		t.Fatalf("body = %q, want daemon", rr.Body.String())
	}
}

func TestRouterMountsHostedWebHandler(t *testing.T) {
	router := NewRouter(Config{
		Version: "dev",
		WebHandler: fixedWebHandler{
			path: "/auth/login/google",
			body: "web-login",
		},
	})

	req := httptest.NewRequest(http.MethodGet, "/auth/login/google", nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	if rr.Body.String() != "web-login" {
		t.Fatalf("body = %q, want web-login", rr.Body.String())
	}
}

type fixedHandler struct {
	path string
	body string
}

func (h fixedHandler) Routes() http.Handler {
	r := chi.NewRouter()
	r.Get(h.path, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(h.body))
	})
	return r
}

type fixedWebHandler struct {
	path string
	body string
}

func (h fixedWebHandler) MountRoutes(r chi.Router) {
	r.Get(h.path, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(h.body))
	})
}
