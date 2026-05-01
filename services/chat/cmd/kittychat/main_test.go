package main

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/kittypaw-app/kittychat/internal/config"
)

func TestNewServerBuildsRunnableRouter(t *testing.T) {
	cfg := config.Config{
		BindAddr:       ":0",
		APIToken:       "api_secret",
		DeviceToken:    "dev_secret",
		UserID:         "user_1",
		DeviceID:       "dev_1",
		LocalAccountID: "alice",
		Version:        "test",
	}
	router := newRouter(cfg)
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
}
