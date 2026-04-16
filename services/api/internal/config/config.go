package config

import (
	"fmt"
	"os"
	"strings"
)

type Config struct {
	Port               string
	DatabaseURL        string
	JWTSecret          string
	GoogleClientID     string
	GoogleClientSecret string
	GitHubClientID     string
	GitHubClientSecret string
	BaseURL            string
	AllowedOrigins     []string
}

func Load() (*Config, error) {
	c := &Config{
		Port:               env("PORT", "8080"),
		DatabaseURL:        os.Getenv("DATABASE_URL"),
		JWTSecret:          os.Getenv("JWT_SECRET"),
		GoogleClientID:     os.Getenv("GOOGLE_CLIENT_ID"),
		GoogleClientSecret: os.Getenv("GOOGLE_CLIENT_SECRET"),
		GitHubClientID:     os.Getenv("GITHUB_CLIENT_ID"),
		GitHubClientSecret: os.Getenv("GITHUB_CLIENT_SECRET"),
		BaseURL:            env("BASE_URL", "http://localhost:8080"),
	}

	required := map[string]string{
		"DATABASE_URL": c.DatabaseURL,
		"JWT_SECRET":   c.JWTSecret,
	}
	for name, val := range required {
		if val == "" {
			return nil, fmt.Errorf("%s is required", name)
		}
	}

	if len(c.JWTSecret) < 32 {
		return nil, fmt.Errorf("JWT_SECRET must be at least 32 characters")
	}

	if origins := os.Getenv("CORS_ORIGINS"); origins != "" {
		c.AllowedOrigins = strings.Split(origins, ",")
	} else {
		c.AllowedOrigins = []string{c.BaseURL}
	}

	return c, nil
}

// LoadForTest returns a config suitable for testing (no required fields).
func LoadForTest() *Config {
	return &Config{
		Port:           env("PORT", "8080"),
		BaseURL:        env("BASE_URL", "http://localhost:8080"),
		AllowedOrigins: []string{"http://localhost:8080"},
	}
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
