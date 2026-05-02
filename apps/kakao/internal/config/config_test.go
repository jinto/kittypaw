package config

import "testing"

func TestLoadDefaults(t *testing.T) {
	t.Setenv("WEBHOOK_SECRET", "")
	t.Setenv("DAILY_LIMIT", "")
	t.Setenv("MONTHLY_LIMIT", "")
	t.Setenv("CHANNEL_URL", "")
	t.Setenv("DATABASE_PATH", "")
	t.Setenv("BIND_ADDR", "")

	cfg := Load()

	if cfg.WebhookSecret != "" {
		t.Fatalf("WebhookSecret = %q, want empty", cfg.WebhookSecret)
	}
	if cfg.DailyLimit != 10_000 {
		t.Fatalf("DailyLimit = %d, want 10000", cfg.DailyLimit)
	}
	if cfg.MonthlyLimit != 100_000 {
		t.Fatalf("MonthlyLimit = %d, want 100000", cfg.MonthlyLimit)
	}
	if cfg.ChannelURL != "" {
		t.Fatalf("ChannelURL = %q, want empty", cfg.ChannelURL)
	}
	if cfg.DatabasePath != "relay.db" {
		t.Fatalf("DatabasePath = %q, want relay.db", cfg.DatabasePath)
	}
	if cfg.BindAddr != "0.0.0.0:8787" {
		t.Fatalf("BindAddr = %q, want 0.0.0.0:8787", cfg.BindAddr)
	}
}

func TestLoadReadsConfiguredValues(t *testing.T) {
	t.Setenv("WEBHOOK_SECRET", "secret")
	t.Setenv("DAILY_LIMIT", "12")
	t.Setenv("MONTHLY_LIMIT", "34")
	t.Setenv("CHANNEL_URL", "https://pf.kakao.com/test")
	t.Setenv("DATABASE_PATH", "/tmp/relay.db")
	t.Setenv("BIND_ADDR", "/tmp/kittykakao.sock")

	cfg := Load()

	if cfg.WebhookSecret != "secret" {
		t.Fatalf("WebhookSecret = %q, want secret", cfg.WebhookSecret)
	}
	if cfg.DailyLimit != 12 {
		t.Fatalf("DailyLimit = %d, want 12", cfg.DailyLimit)
	}
	if cfg.MonthlyLimit != 34 {
		t.Fatalf("MonthlyLimit = %d, want 34", cfg.MonthlyLimit)
	}
	if cfg.ChannelURL != "https://pf.kakao.com/test" {
		t.Fatalf("ChannelURL = %q", cfg.ChannelURL)
	}
	if cfg.DatabasePath != "/tmp/relay.db" {
		t.Fatalf("DatabasePath = %q", cfg.DatabasePath)
	}
	if cfg.BindAddr != "/tmp/kittykakao.sock" {
		t.Fatalf("BindAddr = %q", cfg.BindAddr)
	}
}
