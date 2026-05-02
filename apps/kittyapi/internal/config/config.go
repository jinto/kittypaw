package config

import (
	"fmt"
	"os"
	"strings"
)

type Config struct {
	Port           string
	UnixSocket     string
	DatabaseURL    string
	BaseURL        string
	AllowedOrigins []string
	AirKoreaAPIKey string
	HolidayAPIKey  string
	WeatherAPIKey  string
}

func Load() (*Config, error) {
	c := &Config{
		Port:           env("PORT", "8080"),
		UnixSocket:     os.Getenv("UNIX_SOCKET"),
		DatabaseURL:    os.Getenv("DATABASE_URL"),
		BaseURL:        env("BASE_URL", "http://localhost:8080"),
		AirKoreaAPIKey: os.Getenv("AIRKOREA_API_KEY"),
		HolidayAPIKey:  os.Getenv("HOLIDAY_API_KEY"),
		WeatherAPIKey:  os.Getenv("WEATHER_API_KEY"),
	}

	if c.DatabaseURL == "" {
		return nil, fmt.Errorf("DATABASE_URL is required")
	}

	if origins := os.Getenv("CORS_ORIGINS"); origins != "" {
		c.AllowedOrigins = splitCSV(origins)
	} else {
		c.AllowedOrigins = []string{c.BaseURL}
	}

	return c, nil
}

func LoadForTest() *Config {
	return &Config{
		Port:           env("PORT", "8080"),
		UnixSocket:     os.Getenv("UNIX_SOCKET"),
		BaseURL:        env("BASE_URL", "http://localhost:8080"),
		AllowedOrigins: []string{"http://localhost:8080"},
	}
}

func splitCSV(raw string) []string {
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
