package main

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/kittypaw-app/kittychat/internal/config"
)

func TestNewServerBuildsRunnableRouter(t *testing.T) {
	cfg := testConfig()
	router, err := newRouter(cfg)
	if err != nil {
		t.Fatalf("newRouter() error = %v", err)
	}
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
}

func TestNewServerUsesSeededCredentialVerifier(t *testing.T) {
	router, err := newRouter(testConfig())
	if err != nil {
		t.Fatalf("newRouter() error = %v", err)
	}

	wrongReq := httptest.NewRequest(http.MethodGet, "/nodes/dev_1/v1/models", nil)
	wrongReq.Header.Set("Authorization", "Bearer wrong")
	wrongRR := httptest.NewRecorder()
	router.ServeHTTP(wrongRR, wrongReq)
	if wrongRR.Code != http.StatusUnauthorized {
		t.Fatalf("wrong token status = %d, want 401; body=%s", wrongRR.Code, wrongRR.Body.String())
	}

	validReq := httptest.NewRequest(http.MethodGet, "/nodes/dev_1/v1/models", nil)
	validReq.Header.Set("Authorization", "Bearer api_secret")
	validRR := httptest.NewRecorder()
	router.ServeHTTP(validRR, validReq)
	if validRR.Code != http.StatusServiceUnavailable {
		t.Fatalf("valid token status = %d, want 503 offline; body=%s", validRR.Code, validRR.Body.String())
	}
}

func TestNewServerRejectsInvalidCredentialSeed(t *testing.T) {
	cfg := testConfig()
	cfg.APIToken = ""

	if _, err := newRouter(cfg); err == nil {
		t.Fatal("newRouter() error = nil, want invalid identity seed error")
	}
}

func testConfig() config.Config {
	return config.Config{
		BindAddr:       ":0",
		APIToken:       "api_secret",
		DeviceToken:    "dev_secret",
		UserID:         "user_1",
		DeviceID:       "dev_1",
		LocalAccountID: "alice",
		Version:        "test",
	}
}
