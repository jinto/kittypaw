package config

import (
	"fmt"
	"os"
)

type Config struct {
	BindAddr       string
	APIToken       string
	DeviceToken    string
	UserID         string
	DeviceID       string
	LocalAccountID string
	Version        string
}

func Load() (Config, error) {
	cfg := Config{
		BindAddr:       bindAddr(),
		APIToken:       os.Getenv("KITTYRELAY_API_TOKEN"),
		DeviceToken:    os.Getenv("KITTYRELAY_DEVICE_TOKEN"),
		UserID:         os.Getenv("KITTYRELAY_USER_ID"),
		DeviceID:       os.Getenv("KITTYRELAY_DEVICE_ID"),
		LocalAccountID: os.Getenv("KITTYRELAY_LOCAL_ACCOUNT_ID"),
		Version:        env("KITTYRELAY_VERSION", "dev"),
	}

	required := map[string]string{
		"KITTYRELAY_API_TOKEN":        cfg.APIToken,
		"KITTYRELAY_DEVICE_TOKEN":     cfg.DeviceToken,
		"KITTYRELAY_USER_ID":          cfg.UserID,
		"KITTYRELAY_DEVICE_ID":        cfg.DeviceID,
		"KITTYRELAY_LOCAL_ACCOUNT_ID": cfg.LocalAccountID,
	}
	for name, value := range required {
		if value == "" {
			return Config{}, fmt.Errorf("%s is required", name)
		}
	}
	return cfg, nil
}

func bindAddr() string {
	if value := os.Getenv("KITTYRELAY_BIND_ADDR"); value != "" {
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
