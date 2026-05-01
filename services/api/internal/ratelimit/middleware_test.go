package ratelimit_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/kittypaw-app/kittyapi/internal/auth"
	"github.com/kittypaw-app/kittyapi/internal/model"
	"github.com/kittypaw-app/kittyapi/internal/ratelimit"
)

func ok200(_ http.ResponseWriter, _ *http.Request) {}

func TestMiddlewareAnonymousAllowed(t *testing.T) {
	l := ratelimit.New()
	defer l.Close()

	handler := ratelimit.Middleware(l)(http.HandlerFunc(ok200))

	for range 5 {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.RemoteAddr = "1.2.3.4:1234"
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", w.Code)
		}
	}
}

func TestMiddlewareAnonymousExceeded(t *testing.T) {
	l := ratelimit.New()
	defer l.Close()

	handler := ratelimit.Middleware(l)(http.HandlerFunc(ok200))

	for range 5 {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.RemoteAddr = "5.6.7.8:1234"
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
	}

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "5.6.7.8:1234"
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429, got %d", w.Code)
	}
	if w.Header().Get("Retry-After") == "" {
		t.Fatal("expected Retry-After header")
	}
}

func TestMiddlewareAuthenticatedHigherLimit(t *testing.T) {
	l := ratelimit.New()
	defer l.Close()

	handler := ratelimit.Middleware(l)(http.HandlerFunc(ok200))

	user := &model.User{ID: "user-1"}

	for range 60 {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		ctx := auth.ContextWithUser(req.Context(), user)
		req = req.WithContext(ctx)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", w.Code)
		}
	}

	// 61st should be denied.
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	ctx := auth.ContextWithUser(req.Context(), user)
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429 for 61st request, got %d", w.Code)
	}
}
