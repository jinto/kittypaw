package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"

	"github.com/kittypaw-app/kittyapi/internal/cache"
	"github.com/kittypaw-app/kittyapi/internal/config"
	"github.com/kittypaw-app/kittyapi/internal/model"
	"github.com/kittypaw-app/kittyapi/internal/proxy"
	"github.com/kittypaw-app/kittyapi/internal/ratelimit"
)

const shutdownGrace = 30 * time.Second

func main() {
	initLogging()

	if err := run(); err != nil {
		slog.Error("server exited", "err", err)
		os.Exit(1)
	}
}

func initLogging() {
	raw := strings.ToLower(os.Getenv("LOG_LEVEL"))
	level := slog.LevelInfo
	known := true
	switch raw {
	case "", "info":
	case "debug":
		level = slog.LevelDebug
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	default:
		known = false
	}
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: level})))
	if !known {
		slog.Warn("unknown LOG_LEVEL, falling back to info", "value", raw)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}

	ctx, stopSignals := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stopSignals()

	pool, err := model.NewPool(ctx, cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("db: %w", err)
	}
	defer pool.Close()

	placeStore := model.NewPlaceStore(pool)
	router, cleanup := NewRouter(cfg, placeStore)
	defer cleanup()

	srv := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           router,
		ReadHeaderTimeout: 10 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	serveErr := make(chan error, 1)
	go func() {
		slog.Info("listening", "addr", srv.Addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serveErr <- err
		}
		close(serveErr)
	}()

	select {
	case err := <-serveErr:
		if err != nil {
			return fmt.Errorf("listen: %w", err)
		}
		return nil
	case <-ctx.Done():
		slog.Info("shutdown signal received, draining", "grace", shutdownGrace)
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownGrace)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shutdown: %w", err)
	}
	return nil
}

// NewRouter builds the data API router. Identity routes, discovery, and
// JWKS publication live in apps/portal.
func NewRouter(cfg *config.Config, placeStore model.PlaceStore) (*chi.Mux, func()) {
	r := chi.NewRouter()

	r.Use(middleware.RequestID)
	r.Use(middleware.Recoverer)
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   cfg.AllowedOrigins,
		AllowedMethods:   []string{"GET", "OPTIONS"},
		AllowedHeaders:   []string{"Authorization", "Content-Type"},
		ExposedHeaders:   []string{"X-RateLimit-Limit", "X-RateLimit-Remaining", "Retry-After", "Warning"},
		AllowCredentials: false,
		MaxAge:           300,
	}))

	limiter := ratelimit.New()
	dataCache := cache.New()

	airKorea := &proxy.AirKoreaHandler{
		Cache:      dataCache,
		HTTPClient: &http.Client{Timeout: 15 * time.Second},
		APIKey:     cfg.AirKoreaAPIKey,
	}
	holiday := &proxy.HolidayHandler{
		Cache:      dataCache,
		HTTPClient: &http.Client{Timeout: 15 * time.Second},
		APIKey:     cfg.HolidayAPIKey,
	}
	weather := &proxy.WeatherHandler{
		Cache:      dataCache,
		HTTPClient: &http.Client{Timeout: 15 * time.Second},
		APIKey:     cfg.WeatherAPIKey,
	}
	almanac := &proxy.AlmanacHandler{
		Cache:      dataCache,
		HTTPClient: &http.Client{Timeout: 15 * time.Second},
		APIKey:     cfg.HolidayAPIKey,
	}
	places := &proxy.PlacesHandler{
		Store: placeStore,
	}

	r.Get("/health", handleHealth)

	r.Group(func(r chi.Router) {
		r.Use(ratelimit.Middleware(limiter))

		r.Route("/v1/air/airkorea", func(r chi.Router) {
			r.Get("/realtime/station", airKorea.RealtimeByStation())
			r.Get("/realtime/city", airKorea.RealtimeByCity())
			r.Get("/forecast", airKorea.Forecast())
			r.Get("/forecast/weekly", airKorea.WeeklyForecast())
			r.Get("/unhealthy", airKorea.UnhealthyStations())
		})

		r.Route("/v1/calendar", func(r chi.Router) {
			r.Get("/holidays", holiday.Holidays())
			r.Get("/anniversaries", holiday.Anniversaries())
			r.Get("/solar-terms", holiday.SolarTerms())
		})

		r.Route("/v1/weather/kma", func(r chi.Router) {
			r.Get("/village-fcst", weather.VillageForecast())
			r.Get("/ultra-srt-ncst", weather.UltraShortNowcast())
			r.Get("/ultra-srt-fcst", weather.UltraShortForecast())
		})

		r.Route("/v1/almanac", func(r chi.Router) {
			r.Get("/lunar-date", almanac.LunarDate())
			r.Get("/solar-date", almanac.SolarDate())
			r.Get("/sun", almanac.Sun())
		})

		r.Route("/v1/geo", func(r chi.Router) {
			r.Get("/resolve", places.Resolve())
		})
	})

	cleanup := func() {
		dataCache.Close()
		limiter.Close()
	}
	return r, cleanup
}

func handleHealth(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "healthy"})
}
