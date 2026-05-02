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
	JWKSURL        string
	UserID         string
	DeviceID       string
	LocalAccountID string
	PublicBaseURL  string
	APIAuthBaseURL string
	Version        string
}

func Load() (Config, error) {
	cfg := Config{
		BindAddr:       bindAddr(),
		APIToken:       os.Getenv("KITTYCHAT_API_TOKEN"),
		DeviceToken:    os.Getenv("KITTYCHAT_DEVICE_TOKEN"),
		JWTSecret:      env("KITTYCHAT_JWT_SECRET", os.Getenv("JWT_SECRET")),
		JWKSURL:        os.Getenv("KITTYCHAT_JWKS_URL"),
		UserID:         os.Getenv("KITTYCHAT_USER_ID"),
		DeviceID:       os.Getenv("KITTYCHAT_DEVICE_ID"),
		LocalAccountID: os.Getenv("KITTYCHAT_LOCAL_ACCOUNT_ID"),
		PublicBaseURL:  env("KITTYCHAT_PUBLIC_BASE_URL", "https://chat.kittypaw.app"),
		APIAuthBaseURL: env("KITTYCHAT_API_AUTH_BASE_URL", "https://api.kittypaw.app/auth"),
		Version:        env("KITTYCHAT_VERSION", "dev"),
	}

	hasJWTVerifier := cfg.JWTSecret != "" || cfg.JWKSURL != ""
	required := map[string]string{}
	if !hasJWTVerifier {
		required["KITTYCHAT_API_TOKEN"] = cfg.APIToken
		required["KITTYCHAT_DEVICE_TOKEN"] = cfg.DeviceToken
	}
	if !hasJWTVerifier || cfg.APIToken != "" || cfg.DeviceToken != "" {
		required["KITTYCHAT_USER_ID"] = cfg.UserID
		required["KITTYCHAT_DEVICE_ID"] = cfg.DeviceID
		required["KITTYCHAT_LOCAL_ACCOUNT_ID"] = cfg.LocalAccountID
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
