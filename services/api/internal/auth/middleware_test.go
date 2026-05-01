package auth_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/kittypaw-app/kittyapi/internal/auth"
)

func TestMiddlewareValidJWT(t *testing.T) {
	userStore := newMockUserStore()
	_, _ = userStore.CreateOrUpdate(t.Context(), "google", "123", "t@t.com", "Test", "")

	token, _ := auth.Sign("user-google-123", testSecret, 15*time.Minute)

	handler := auth.Middleware(testSecret, userStore)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		if user == nil {
			t.Fatal("expected user in context")
		}
		if user.ID != "user-google-123" {
			t.Fatalf("expected user-google-123, got %q", user.ID)
		}
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestMiddlewareAnonymous(t *testing.T) {
	userStore := newMockUserStore()

	handler := auth.Middleware(testSecret, userStore)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		if user != nil {
			t.Fatal("expected nil user for anonymous")
		}
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestMiddlewareInvalidJWT(t *testing.T) {
	userStore := newMockUserStore()

	handler := auth.Middleware(testSecret, userStore)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer invalid-token")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}

func TestMiddlewareMalformedHeader(t *testing.T) {
	userStore := newMockUserStore()

	handler := auth.Middleware(testSecret, userStore)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "NotBearer something")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}

func TestHandleMeAuthenticated(t *testing.T) {
	userStore := newMockUserStore()
	_, _ = userStore.CreateOrUpdate(t.Context(), "google", "123", "t@t.com", "Test", "")

	token, _ := auth.Sign("user-google-123", testSecret, 15*time.Minute)

	handler := auth.Middleware(testSecret, userStore)(http.HandlerFunc(auth.HandleMe))

	req := httptest.NewRequest(http.MethodGet, "/auth/me", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var user map[string]any
	if err := json.NewDecoder(w.Body).Decode(&user); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if user["email"] != "t@t.com" {
		t.Fatalf("expected email t@t.com, got %v", user["email"])
	}
}

func TestHandleMeAnonymous(t *testing.T) {
	userStore := newMockUserStore()

	handler := auth.Middleware(testSecret, userStore)(http.HandlerFunc(auth.HandleMe))

	req := httptest.NewRequest(http.MethodGet, "/auth/me", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}
