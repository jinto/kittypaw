//go:build integration

package testfixture_test

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/kittypaw-app/kittyapi/internal/auth/testfixture"
	"github.com/kittypaw-app/kittyapi/internal/model"
)

func TestSeedTestUser_LiveDB(t *testing.T) {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL not set — skipping integration test")
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	defer pool.Close()

	store := model.NewUserStore(pool)

	u := testfixture.SeedTestUser(t, store)
	if u == nil {
		t.Fatal("expected non-nil user")
	}
	if u.ID == "" {
		t.Fatal("expected non-empty user ID")
	}
	if u.Email == "" {
		t.Fatal("expected non-empty email")
	}
	if u.Provider != "google" {
		t.Fatalf("expected provider=google, got %q", u.Provider)
	}
}

func TestSeedTestUser_Idempotent(t *testing.T) {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL not set")
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	defer pool.Close()

	store := model.NewUserStore(pool)

	u1 := testfixture.SeedTestUser(t, store)
	u2 := testfixture.SeedTestUser(t, store)
	if u1.ID == u2.ID {
		t.Fatal("expected unique users for separate seed calls (UnixNano provider_id)")
	}
}
