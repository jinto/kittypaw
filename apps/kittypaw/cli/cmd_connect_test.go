package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jinto/kittypaw/core"
)

func TestRootCommandRegistersConnectGmail(t *testing.T) {
	root := newRootCmd()
	cmd, _, err := root.Find([]string{"connect", "gmail"})
	if err != nil || cmd == nil || cmd.Name() != "gmail" {
		t.Fatalf("Find(connect gmail) = (%v, %v), want gmail command", cmd, err)
	}
}

func TestConnectGmailLoginURLUsesDiscoveredConnectBaseURL(t *testing.T) {
	secrets := testSecretsStore(t)
	mgr := core.NewAPITokenManager("", secrets)
	apiURL := "https://portal.kittypaw.app"
	if err := mgr.SaveConnectBaseURL(apiURL, "https://connect.kittypaw.app"); err != nil {
		t.Fatal(err)
	}

	got := connectGmailLoginURL(apiURL, mgr, "http", 12345)
	want := "https://connect.kittypaw.app/connect/gmail/login?mode=http&port=12345"
	if got != want {
		t.Fatalf("connectGmailLoginURL = %q, want %q", got, want)
	}
}

func TestConnectCallbackRejectsTokenQueryParams(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/callback?access_token=AT&refresh_token=RT", nil)
	_, err := connectCallbackCode(req)
	if err == nil {
		t.Fatal("connectCallbackCode accepted token query params")
	}
	if !strings.Contains(err.Error(), "one-time code") {
		t.Fatalf("error = %v", err)
	}
}

func TestConnectExchangeStoresGmailTokens(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/connect/cli/exchange" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"provider":"gmail","access_token":"access-1","refresh_token":"refresh-1","token_type":"Bearer","expires_in":3600,"scope":"gmail.readonly","email":"alice@example.com"}`)
	}))
	defer ts.Close()

	secrets := testSecretsStore(t)
	serviceMgr := core.NewServiceTokenManager(secrets)

	if err := exchangeConnectCode(ts.URL, "code-1", serviceMgr); err != nil {
		t.Fatalf("exchangeConnectCode: %v", err)
	}
	ns := core.ServiceTokenNamespace("gmail")
	for key, want := range map[string]string{
		"access_token":     "access-1",
		"refresh_token":    "refresh-1",
		"connect_base_url": ts.URL,
		"email":            "alice@example.com",
	} {
		if got, ok := secrets.Get(ns, key); !ok || got != want {
			t.Fatalf("%s = (%q, %v), want %q", key, got, ok, want)
		}
	}
}
