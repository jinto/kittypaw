package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Security invariant: stdin > env > flag. A regression would let a hostile env hijack stdin-typed tokens.
func TestResolveTenantToken_StdinPreferred(t *testing.T) {
	t.Setenv(tenantEnvBotToken, "env-token")
	f := &tenantAddFlags{
		telegramToken:      "flag-token",
		telegramTokenStdin: true,
	}
	var stderr bytes.Buffer

	tok, err := resolveTenantToken(f, strings.NewReader("stdin-token\n"), &stderr)
	if err != nil {
		t.Fatalf("resolveTenantToken: %v", err)
	}
	if tok != "stdin-token" {
		t.Errorf("token = %q, want stdin-token", tok)
	}
}

func TestResolveTenantToken_EnvBeatsFlag(t *testing.T) {
	t.Setenv(tenantEnvBotToken, "env-token")
	f := &tenantAddFlags{telegramToken: "flag-token"}
	var stderr bytes.Buffer

	tok, err := resolveTenantToken(f, strings.NewReader(""), &stderr)
	if err != nil {
		t.Fatalf("resolveTenantToken: %v", err)
	}
	if tok != "env-token" {
		t.Errorf("token = %q, want env-token", tok)
	}
	if !strings.Contains(stderr.String(), "ignored") {
		t.Errorf("expected shadowing warning, stderr = %q", stderr.String())
	}
}

// Silent flag path would train users into the ps-exposed habit.
func TestResolveTenantToken_FlagWarnsVisible(t *testing.T) {
	t.Setenv(tenantEnvBotToken, "")
	f := &tenantAddFlags{telegramToken: "flag-token"}
	var stderr bytes.Buffer

	tok, err := resolveTenantToken(f, strings.NewReader(""), &stderr)
	if err != nil {
		t.Fatalf("resolveTenantToken: %v", err)
	}
	if tok != "flag-token" {
		t.Errorf("token = %q, want flag-token", tok)
	}
	if !strings.Contains(stderr.String(), "process list") {
		t.Errorf("expected process-list warning, stderr = %q", stderr.String())
	}
}

// Silent accept would provision a tenant with an empty token — passes validation, fails at runtime.
func TestResolveTenantToken_StdinEmpty(t *testing.T) {
	f := &tenantAddFlags{telegramTokenStdin: true}
	var stderr bytes.Buffer

	_, err := resolveTenantToken(f, strings.NewReader("   \n"), &stderr)
	if err == nil {
		t.Fatal("expected error for empty stdin, got nil")
	}
}

// admin-chat-id is supplied so FetchTelegramChatID is skipped; tests must not hit the network.
func TestRunTenantAdd_HappyPath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	var stdout, stderr bytes.Buffer
	f := &tenantAddFlags{
		telegramToken:      "12345:ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghij",
		telegramTokenStdin: false,
		adminChatID:        "111",
	}
	if err := runTenantAdd("alice", f, strings.NewReader(""), &stdout, &stderr); err != nil {
		t.Fatalf("runTenantAdd: %v", err)
	}

	tenantDir := filepath.Join(home, ".kittypaw", "tenants", "alice")
	if info, err := os.Stat(tenantDir); err != nil || !info.IsDir() {
		t.Errorf("tenant dir missing: err=%v", err)
	}
	if !strings.Contains(stdout.String(), "alice") {
		t.Errorf("stdout should confirm tenant name, got %q", stdout.String())
	}
	// No daemon running → fallback hint should surface; exact phrasing
	// may shift, but the operator must see a recovery path.
	if !strings.Contains(stdout.String(), "kittypaw serve") {
		t.Errorf("stdout should mention how to activate, got %q", stdout.String())
	}
}

func TestRunTenantAdd_Family(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	var stdout, stderr bytes.Buffer
	f := &tenantAddFlags{isFamily: true}
	if err := runTenantAdd("family", f, strings.NewReader(""), &stdout, &stderr); err != nil {
		t.Fatalf("runTenantAdd family: %v", err)
	}

	tenantDir := filepath.Join(home, ".kittypaw", "tenants", "family")
	if _, err := os.Stat(filepath.Join(tenantDir, "config.toml")); err != nil {
		t.Errorf("family config.toml missing: %v", err)
	}
}

// Most common mistake: omitting both --is-family and any token source.
func TestRunTenantAdd_NoTokenRejected(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	var stdout, stderr bytes.Buffer
	err := runTenantAdd("charlie", &tenantAddFlags{}, strings.NewReader(""), &stdout, &stderr)
	if err == nil {
		t.Fatal("expected error for missing token, got nil")
	}
	if !strings.Contains(err.Error(), "Telegram bot token is required") {
		t.Errorf("error should explain missing token: %q", err.Error())
	}
	if _, statErr := os.Stat(filepath.Join(home, ".kittypaw", "tenants", "charlie")); !os.IsNotExist(statErr) {
		t.Errorf("no tenant dir should be created on validation failure")
	}
}

// Accepting malformed tokens would defer the failure to the first getUpdates — worse error surface.
func TestRunTenantAdd_InvalidTokenFormat(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	var stdout, stderr bytes.Buffer
	f := &tenantAddFlags{
		telegramToken: "not-a-real-token",
		adminChatID:   "111",
	}
	err := runTenantAdd("alice", f, strings.NewReader(""), &stdout, &stderr)
	if err == nil {
		t.Fatal("expected invalid-format error, got nil")
	}
	if !strings.Contains(err.Error(), "invalid telegram bot token") {
		t.Errorf("error should name the field: %q", err.Error())
	}
}

// CLI-layer rejection gives a flag-oriented message, not a config-file one.
func TestRunTenantAdd_FamilyWithTokenRejected(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	var stdout, stderr bytes.Buffer
	f := &tenantAddFlags{
		isFamily:      true,
		telegramToken: "12345:ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghij",
		adminChatID:   "111",
	}
	err := runTenantAdd("family", f, strings.NewReader(""), &stdout, &stderr)
	if err == nil {
		t.Fatal("expected rejection of family+token, got nil")
	}
	if !strings.Contains(err.Error(), "mutually exclusive") {
		t.Errorf("error should say mutually exclusive: %q", err.Error())
	}
}
