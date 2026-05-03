package auth_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/kittypaw-app/kittyportal/internal/auth"
)

func TestCLICallbackCodeModeUsesStyledCodePage(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /token", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"access_token": "google-at"})
	})
	mux.HandleFunc("GET /userinfo", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"id":      "g-user-1",
			"email":   "test@gmail.com",
			"name":    "Test User",
			"picture": "https://avatar.example.com/1",
		})
	})
	googleServer := httptest.NewServer(mux)
	defer googleServer.Close()

	h, googleCfg := setupGoogleTest(t, googleServer)
	h.GoogleTokenURL = googleServer.URL + "/token"
	h.GoogleUserInfoURL = googleServer.URL + "/userinfo"
	codeStore := auth.NewCLICodeStore()
	t.Cleanup(codeStore.Close)

	state, err := h.StateStore.CreateWithMeta("test-verifier", map[string]string{"mode": "code"})
	if err != nil {
		t.Fatalf("create state: %v", err)
	}
	req := httptest.NewRequest(http.MethodGet, "/auth/cli/callback?code=test-code&state="+state, nil)
	w := httptest.NewRecorder()

	h.HandleCLICallback(auth.CLILoginConfig{
		GoogleCfg: googleCfg,
		CodeStore: codeStore,
		BaseURL:   "http://localhost:8080",
	}).ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); ct != "text/html; charset=utf-8" {
		t.Fatalf("Content-Type = %q, want html", ct)
	}
	if cc := w.Header().Get("Cache-Control"); cc != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", cc)
	}
	body := w.Body.String()
	for _, want := range []string{
		`id="portal-cli-code"`,
		`KittyPaw Portal`,
		`Enter this code in your terminal`,
		`This code expires in 5 minutes.`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("CLI code page missing %q:\n%s", want, body)
		}
	}
}
