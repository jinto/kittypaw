//go:build integration

package model_test

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/pgx/v5"
	_ "github.com/golang-migrate/migrate/v4/source/file"

	"github.com/jinto/kittypaw-api/internal/model"
)

func setupTestDB(t *testing.T) *model.PostgresUserStore {
	t.Helper()

	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://kittypaw:kittypaw@localhost:5432/kittypaw_api_test?sslmode=disable"
	}

	m, err := migrate.New("file://../../migrations", "pgx5://"+stripScheme(dbURL))
	if err != nil {
		t.Fatalf("migrate new: %v", err)
	}
	if err := m.Down(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		// ignore — tables might not exist yet
	}
	if err := m.Up(); err != nil {
		t.Fatalf("migrate up: %v", err)
	}

	ctx := context.Background()
	pool, err := model.NewPool(ctx, dbURL)
	if err != nil {
		t.Fatalf("new pool: %v", err)
	}
	t.Cleanup(func() { pool.Close() })

	// Clean tables for test isolation.
	_, _ = pool.Exec(ctx, "DELETE FROM refresh_tokens")
	_, _ = pool.Exec(ctx, "DELETE FROM users")

	return model.NewUserStore(pool)
}

func stripScheme(url string) string {
	for i, c := range url {
		if c == ':' && i > 0 {
			return url[i+3:] // skip "://"
		}
	}
	return url
}

func TestCreateOrUpdateAndFindByID(t *testing.T) {
	store := setupTestDB(t)
	ctx := context.Background()

	user, err := store.CreateOrUpdate(ctx, "google", "123", "test@test.com", "Test User", "https://avatar.example.com/1")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if user.ID == "" {
		t.Fatal("expected non-empty ID")
	}
	if user.Provider != "google" {
		t.Fatalf("expected provider=google, got %q", user.Provider)
	}
	if user.Email != "test@test.com" {
		t.Fatalf("expected email=test@test.com, got %q", user.Email)
	}

	found, err := store.FindByID(ctx, user.ID)
	if err != nil {
		t.Fatalf("find: %v", err)
	}
	if found.ID != user.ID {
		t.Fatalf("expected ID=%s, got %s", user.ID, found.ID)
	}
	if found.Name != "Test User" {
		t.Fatalf("expected name=Test User, got %q", found.Name)
	}
}

func TestCreateOrUpdateUpsert(t *testing.T) {
	store := setupTestDB(t)
	ctx := context.Background()

	first, err := store.CreateOrUpdate(ctx, "github", "456", "old@test.com", "Old Name", "")
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	second, err := store.CreateOrUpdate(ctx, "github", "456", "new@test.com", "New Name", "https://new-avatar.example.com")
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}

	if second.ID != first.ID {
		t.Fatalf("upsert should preserve ID: %s != %s", first.ID, second.ID)
	}
	if second.Email != "new@test.com" {
		t.Fatalf("expected updated email, got %q", second.Email)
	}
	if second.Name != "New Name" {
		t.Fatalf("expected updated name, got %q", second.Name)
	}
	if second.AvatarURL != "https://new-avatar.example.com" {
		t.Fatalf("expected updated avatar, got %q", second.AvatarURL)
	}
}

func TestFindByIDNotFound(t *testing.T) {
	store := setupTestDB(t)
	ctx := context.Background()

	_, err := store.FindByID(ctx, "00000000-0000-0000-0000-000000000000")
	if !errors.Is(err, model.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}
