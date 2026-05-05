package connect

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestGmailProviderAuthURL(t *testing.T) {
	provider := NewGmailProvider(GmailConfig{
		ClientID: "connect-client-id",
		BaseURL:  "https://connect.kittypaw.app",
		AuthURL:  "https://accounts.example/auth",
	}, nil)

	raw := provider.AuthURL("state-1", "verifier-1")
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	q := u.Query()
	if got := u.String(); !strings.HasPrefix(got, "https://accounts.example/auth?") {
		t.Fatalf("auth URL = %q", got)
	}
	if q.Get("client_id") != "connect-client-id" {
		t.Fatalf("client_id = %q", q.Get("client_id"))
	}
	if q.Get("redirect_uri") != "https://connect.kittypaw.app/connect/gmail/callback" {
		t.Fatalf("redirect_uri = %q", q.Get("redirect_uri"))
	}
	if q.Get("scope") != GmailReadOnlyScope {
		t.Fatalf("scope = %q", q.Get("scope"))
	}
	if q.Get("code_challenge_method") != "S256" || q.Get("code_challenge") == "" {
		t.Fatalf("PKCE params missing: %s", raw)
	}
	if q.Get("access_type") != "offline" {
		t.Fatalf("access_type = %q", q.Get("access_type"))
	}
	if q.Get("include_granted_scopes") != "true" {
		t.Fatalf("include_granted_scopes = %q", q.Get("include_granted_scopes"))
	}
	if q.Get("prompt") != "consent" {
		t.Fatalf("prompt = %q", q.Get("prompt"))
	}
}

func TestGmailProviderExchangeAndRefresh(t *testing.T) {
	var tokenForms []url.Values
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			if err := r.ParseForm(); err != nil {
				t.Fatalf("ParseForm: %v", err)
			}
			copied := make(url.Values, len(r.Form))
			for k, v := range r.Form {
				copied[k] = append([]string(nil), v...)
			}
			tokenForms = append(tokenForms, copied)
			w.Header().Set("Content-Type", "application/json")
			switch r.Form.Get("grant_type") {
			case "authorization_code":
				fmt.Fprint(w, `{"access_token":"access-1","refresh_token":"refresh-1","token_type":"Bearer","expires_in":3600,"scope":"`+GmailReadOnlyScope+`"}`)
			case "refresh_token":
				fmt.Fprint(w, `{"access_token":"access-2","token_type":"Bearer","expires_in":3600,"scope":"`+GmailReadOnlyScope+`"}`)
			default:
				t.Fatalf("grant_type = %q", r.Form.Get("grant_type"))
			}
		case "/userinfo":
			if got := r.Header.Get("Authorization"); got != "Bearer access-1" {
				t.Fatalf("userinfo Authorization = %q", got)
			}
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"email":"alice@example.com"}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()

	provider := NewGmailProvider(GmailConfig{
		ClientID:     "connect-client-id",
		ClientSecret: "connect-secret",
		BaseURL:      "https://connect.kittypaw.app",
		TokenURL:     ts.URL + "/token",
		UserInfoURL:  ts.URL + "/userinfo",
	}, ts.Client())

	tokens, err := provider.ExchangeCode(t.Context(), "google-code", "verifier-1")
	if err != nil {
		t.Fatalf("ExchangeCode: %v", err)
	}
	if tokens.Provider != "gmail" || tokens.AccessToken != "access-1" || tokens.RefreshToken != "refresh-1" || tokens.Email != "alice@example.com" {
		b, _ := json.Marshal(tokens)
		t.Fatalf("tokens = %s", b)
	}
	if tokenForms[0].Get("client_id") != "connect-client-id" {
		t.Fatalf("exchange client_id = %q", tokenForms[0].Get("client_id"))
	}
	if tokenForms[0].Get("redirect_uri") != "https://connect.kittypaw.app/connect/gmail/callback" {
		t.Fatalf("exchange redirect_uri = %q", tokenForms[0].Get("redirect_uri"))
	}
	if tokenForms[0].Get("code_verifier") != "verifier-1" {
		t.Fatalf("exchange code_verifier = %q", tokenForms[0].Get("code_verifier"))
	}

	refreshed, err := provider.Refresh(t.Context(), "refresh-1")
	if err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if refreshed.Provider != "gmail" || refreshed.AccessToken != "access-2" || refreshed.RefreshToken != "" {
		b, _ := json.Marshal(refreshed)
		t.Fatalf("refreshed = %s", b)
	}
	if tokenForms[1].Get("grant_type") != "refresh_token" || tokenForms[1].Get("refresh_token") != "refresh-1" {
		t.Fatalf("refresh form = %v", tokenForms[1])
	}
}
