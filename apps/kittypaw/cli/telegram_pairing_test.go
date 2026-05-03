package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/jinto/kittypaw/core"
)

func TestDefaultTelegramPairingClientUsesLocalServerWhenAccountDiscoveryFails(t *testing.T) {
	root := t.TempDir()
	t.Setenv("KITTYPAW_CONFIG_DIR", root)

	accountsDir := filepath.Join(root, "accounts")
	for _, id := range []string{"alice", "bob"} {
		if _, err := core.InitAccount(accountsDir, id, core.AccountOpts{}); err != nil {
			t.Fatalf("seed account %s: %v", id, err)
		}
	}

	token := "123456:ABCDEFGHIJKLMNOPQRSTUVWXYZabcd"
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/telegram/pairing/chat-id" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("x-api-key") != "" || r.URL.Query().Get("token") != "" {
			t.Fatalf("local fallback must not send an auth token")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"paired","chat_id":"8172543364","source":"active_channel"}`))
	}))
	t.Cleanup(ts.Close)

	oldLocalURLs := telegramPairingLocalBaseURLs
	telegramPairingLocalBaseURLs = func() []string { return []string{ts.URL} }
	t.Cleanup(func() { telegramPairingLocalBaseURLs = oldLocalURLs })

	status, err := defaultTelegramPairingClient("new-account")(context.Background(), token)
	if err != nil {
		t.Fatalf("defaultTelegramPairingClient: %v", err)
	}
	if status.ChatID != "8172543364" || status.Source != "active_channel" {
		t.Fatalf("status = %#v, want active channel chat id", status)
	}
}

func TestDefaultTelegramPairingClientFallsBackWhenLocalServerDoesNotOwnToken(t *testing.T) {
	root := t.TempDir()
	t.Setenv("KITTYPAW_CONFIG_DIR", root)

	accountsDir := filepath.Join(root, "accounts")
	for _, id := range []string{"alice", "bob"} {
		if _, err := core.InitAccount(accountsDir, id, core.AccountOpts{}); err != nil {
			t.Fatalf("seed account %s: %v", id, err)
		}
	}

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":"no running Telegram channel is using this bot token"}`, http.StatusNotFound)
	}))
	t.Cleanup(ts.Close)

	oldLocalURLs := telegramPairingLocalBaseURLs
	telegramPairingLocalBaseURLs = func() []string { return []string{ts.URL} }
	t.Cleanup(func() { telegramPairingLocalBaseURLs = oldLocalURLs })

	_, err := defaultTelegramPairingClient("new-account")(context.Background(), "123456:ABCDEFGHIJKLMNOPQRSTUVWXYZabcd")
	if !errors.Is(err, errTelegramPairingServerUnavailable) {
		t.Fatalf("error = %v, want errTelegramPairingServerUnavailable", err)
	}
}
