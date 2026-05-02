package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/kittypaw-app/kittykakao/internal/config"
	"github.com/kittypaw-app/kittykakao/internal/server"
	"github.com/kittypaw-app/kittykakao/internal/store"
)

const shutdownGrace = 30 * time.Second

var (
	version = "dev"
	commit  = "unknown"
)

func main() {
	initLogging()
	if err := run(); err != nil {
		slog.Error("server exited", "err", err)
		os.Exit(1)
	}
}

func run() error {
	cfg := config.Load()
	if cfg.WebhookSecret == "" {
		slog.Warn("WEBHOOK_SECRET is empty; webhook and admin requests will be rejected")
	}

	ctx, stopSignals := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stopSignals()

	st, err := store.Open(cfg.DatabasePath)
	if err != nil {
		return fmt.Errorf("store: %w", err)
	}
	defer st.Close()

	state := server.NewState(cfg, st, buildValue("", version), buildValue("", commit))
	go state.StartPendingSweeper(ctx, 60*time.Second, 600)

	srv := &http.Server{
		Addr:              cfg.BindAddr,
		Handler:           server.NewRouter(state),
		ReadHeaderTimeout: 10 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	serveErr := make(chan error, 1)
	go func() {
		slog.Info("listening", "addr", listenAddr(cfg.BindAddr))
		if err := serveHTTP(srv, cfg.BindAddr); err != nil && !errors.Is(err, http.ErrServerClosed) {
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

func initLogging() {
	raw := strings.ToLower(os.Getenv("LOG_LEVEL"))
	if raw == "" {
		raw = strings.ToLower(os.Getenv("RUST_LOG"))
	}
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
		slog.Warn("unknown log level, falling back to info", "value", raw)
	}
}

func buildValue(configured, built string) string {
	if built != "" && built != "dev" && built != "unknown" {
		return built
	}
	if configured != "" {
		return configured
	}
	if built != "" {
		return built
	}
	return configured
}

func listenAddr(bindAddr string) string {
	if path, ok := unixSocketPath(bindAddr); ok {
		return "unix:" + path
	}
	return bindAddr
}

func serveHTTP(srv *http.Server, bindAddr string) error {
	socketPath, ok := unixSocketPath(bindAddr)
	if !ok {
		return srv.ListenAndServe()
	}
	if err := os.MkdirAll(filepath.Dir(socketPath), 0o755); err != nil {
		return err
	}
	if err := os.Remove(socketPath); err != nil && !os.IsNotExist(err) {
		return err
	}
	ln, err := net.Listen("unix", socketPath)
	if err != nil {
		return err
	}
	defer os.Remove(socketPath)
	if err := os.Chmod(socketPath, 0o660); err != nil {
		ln.Close()
		return err
	}
	return srv.Serve(ln)
}

func unixSocketPath(bindAddr string) (string, bool) {
	if strings.HasPrefix(bindAddr, "unix:") {
		return strings.TrimPrefix(bindAddr, "unix:"), true
	}
	if strings.HasPrefix(bindAddr, "/") {
		return bindAddr, true
	}
	return "", false
}
