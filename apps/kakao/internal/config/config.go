package config

import (
	"os"
	"strconv"
)

type Config struct {
	WebhookSecret string
	DailyLimit    uint64
	MonthlyLimit  uint64
	ChannelURL    string
	DatabasePath  string
	BindAddr      string
}

func Load() Config {
	return Config{
		WebhookSecret: os.Getenv("WEBHOOK_SECRET"),
		DailyLimit:    uintEnv("DAILY_LIMIT", 10_000),
		MonthlyLimit:  uintEnv("MONTHLY_LIMIT", 100_000),
		ChannelURL:    os.Getenv("CHANNEL_URL"),
		DatabasePath:  env("DATABASE_PATH", "relay.db"),
		BindAddr:      env("BIND_ADDR", "0.0.0.0:8787"),
	}
}

func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func uintEnv(key string, fallback uint64) uint64 {
	raw := os.Getenv(key)
	if raw == "" {
		return fallback
	}
	value, err := strconv.ParseUint(raw, 10, 64)
	if err != nil {
		return fallback
	}
	return value
}
