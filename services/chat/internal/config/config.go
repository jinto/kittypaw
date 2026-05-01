package config

import (
	"fmt"
	"os"
)

type Config struct {
	BindAddr       string
	APIToken       string
	DeviceToken    string
	JWTSecret      string
	UserID         string
	DeviceID       string
	LocalAccountID string
	Version        string
}

func Load() (Config, error) {
	cfg := Config{
		BindAddr:       bindAddr(),
		APIToken:       os.Getenv("KITTYCHAT_API_TOKEN"),
		DeviceToken:    os.Getenv("KITTYCHAT_DEVICE_TOKEN"),
		JWTSecret:      env("KITTYCHAT_JWT_SECRET", os.Getenv("JWT_SECRET")),
		UserID:         os.Getenv("KITTYCHAT_USER_ID"),
		DeviceID:       os.Getenv("KITTYCHAT_DEVICE_ID"),
		LocalAccountID: os.Getenv("KITTYCHAT_LOCAL_ACCOUNT_ID"),
		Version:        env("KITTYCHAT_VERSION", "dev"),
	}

	required := map[string]string{
		"KITTYCHAT_DEVICE_TOKEN":     cfg.DeviceToken,
		"KITTYCHAT_USER_ID":          cfg.UserID,
		"KITTYCHAT_DEVICE_ID":        cfg.DeviceID,
		"KITTYCHAT_LOCAL_ACCOUNT_ID": cfg.LocalAccountID,
	}
	if cfg.JWTSecret == "" {
		required["KITTYCHAT_API_TOKEN"] = cfg.APIToken
	}
	for name, value := range required {
		if value == "" {
			return Config{}, fmt.Errorf("%s is required", name)
		}
	}
	if cfg.JWTSecret != "" && len(cfg.JWTSecret) < 32 {
		return Config{}, fmt.Errorf("KITTYCHAT_JWT_SECRET must be at least 32 characters")
	}
	return cfg, nil
}

func bindAddr() string {
	if value := os.Getenv("KITTYCHAT_BIND_ADDR"); value != "" {
		return value
	}
	if port := os.Getenv("PORT"); port != "" {
		return ":" + port
	}
	return ":8080"
}

func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
