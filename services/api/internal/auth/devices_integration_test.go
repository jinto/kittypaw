//go:build integration

package auth_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/kittypaw-app/kittyapi/internal/auth"
	"github.com/kittypaw-app/kittyapi/internal/auth/testfixture"
	"github.com/kittypaw-app/kittyapi/internal/config"
	"github.com/kittypaw-app/kittyapi/internal/model"
)

// Plan 23 PR-D — devices endpoints integration tests (real DB).
// T4 (list) + T5 (delete) are integration-only per CEO scope cut —
// handler logic is thin enough that real-DB testing covers all
// branches without the maintenance cost of mockDeviceStore plumbing.

type devicesITSetup struct {
	pool         *pgxpool.Pool
	users        model.UserStore
	devices      model.DeviceStore
	refreshStore model.RefreshTokenStore
	handler      *auth.OAuthHandler
	server       *httptest.Server
}

func setupDevicesIntegration(t *testing.T) *devicesITSetup {
	t.Helper()
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set — skipping integration test")
	}
	if !strings.Contains(dsn, "_test") {
		t.Fatalf("DATABASE_URL must point at a test DB (must contain \"_test\"); got %q", dsn)
	}

	cfg := config.LoadForTest()
	provider := auth.NewSingleKeyProvider(&cfg.JWTPrivateKey.PublicKey, cfg.JWTKID)

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}

	users := model.NewUserStore(pool)
	devices := model.NewDeviceStore(pool)
	refreshStore := model.NewRefreshTokenStore(pool)

	h := &auth.OAuthHandler{
		UserStore:         users,
		RefreshTokenStore: refreshStore,
		DeviceStore:       devices,
		StateStore:        auth.NewStateStore(),
		JWTPrivateKey:     cfg.JWTPrivateKey,
		JWTKID:            cfg.JWTKID,
	}

	// chi.URLParam에 의존하는 delete handler를 위해 chi mux로 wrap.
	// Plan 23 T6 main.go 와이어링 모방.
	r := newDeviceTestRouter(h, provider, users)
	server := httptest.NewServer(r)

	t.Cleanup(func() {
		bg := context.Background()
		_, _ = pool.Exec(bg, `DELETE FROM refresh_tokens WHERE user_id IN (SELECT id FROM users WHERE provider_id LIKE 'test-%')`)
		_, _ = pool.Exec(bg, `DELETE FROM devices WHERE user_id IN (SELECT id FROM users WHERE provider_id LIKE 'test-%')`)
		_, _ = pool.Exec(bg, "DELETE FROM users WHERE provider_id LIKE 'test-%'")
		server.Close()
		h.StateStore.Close()
		pool.Close()
	})

	return &devicesITSetup{
		pool:         pool,
		users:        users,
		devices:      devices,
		refreshStore: refreshStore,
		handler:      h,
		server:       server,
	}
}

// newDeviceTestRouter wires a chi router that mirrors the production
// shape from cmd/server/main.go — refresh outside authMW, list/delete/pair
// inside authMW. Tests hit this via httptest.Server.
func newDeviceTestRouter(h *auth.OAuthHandler, provider auth.JWKSProvider, users model.UserStore) http.Handler {
	r := chi.NewRouter()
	mw := auth.Middleware(provider, auth.AudienceAPI, users)

	// refresh — no authMW (Plan 23 결정 3)
	r.Post("/auth/devices/refresh", h.HandleDeviceRefresh())

	// pair / list / delete — authMW applied
	r.Group(func(r chi.Router) {
		r.Use(mw)
		r.Post("/auth/devices/pair", h.HandlePair())
		r.Get("/auth/devices", h.HandleDevicesList())
		r.Delete("/auth/devices/{id}", h.HandleDeviceDelete())
	})
	return r
}

func authedRequest(t *testing.T, server *httptest.Server, method, path string, body io.Reader, userID string) (*http.Response, []byte) {
	t.Helper()
	cfg := config.LoadForTest()
	token := testfixture.IssueTestJWT(t, cfg.JWTPrivateKey, cfg.JWTKID, userID, 15*time.Minute)
	req, err := http.NewRequest(method, server.URL+path, body)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	return resp, respBody
}

func anonRequest(t *testing.T, server *httptest.Server, method, path string, body io.Reader) (*http.Response, []byte) {
	t.Helper()
	req, err := http.NewRequest(method, server.URL+path, body)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	return resp, respBody
}

func seedTestUserDevices(t *testing.T, s *devicesITSetup, providerSuffix string) *model.User {
	t.Helper()
	user, err := s.users.CreateOrUpdate(context.Background(), "google", "test-"+providerSuffix, providerSuffix+"@test.com", providerSuffix, "")
	if err != nil {
		t.Fatalf("seed user: %v", err)
	}
	return user
}

// --- T4: List handler ---

func TestHandleDevicesList_Happy_2Devices(t *testing.T) {
	s := setupDevicesIntegration(t)
	user := seedTestUserDevices(t, s, "list1")

	first, _ := s.devices.Create(context.Background(), user.ID, "first", nil)
	time.Sleep(10 * time.Millisecond) // ensure paired_at differs
	second, _ := s.devices.Create(context.Background(), user.ID, "second", nil)

	resp, body := authedRequest(t, s.server, http.MethodGet, "/auth/devices", nil, user.ID)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", resp.StatusCode, string(body))
	}
	var got []model.Device
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("decode: %v (body=%s)", err, string(body))
	}
	if len(got) != 2 {
		t.Fatalf("got %d devices, want 2", len(got))
	}
	// paired_at DESC — second (newer) first.
	if got[0].ID != second.ID {
		t.Fatalf("first listed = %q, want %q (paired_at DESC)", got[0].ID, second.ID)
	}
	if got[1].ID != first.ID {
		t.Fatalf("second listed = %q, want %q", got[1].ID, first.ID)
	}
}

func TestHandleDevicesList_RevokedFiltered(t *testing.T) {
	s := setupDevicesIntegration(t)
	user := seedTestUserDevices(t, s, "list2")
	active, _ := s.devices.Create(context.Background(), user.ID, "active", nil)
	revoked, _ := s.devices.Create(context.Background(), user.ID, "revoked", nil)
	_ = s.devices.Revoke(context.Background(), revoked.ID)

	resp, body := authedRequest(t, s.server, http.MethodGet, "/auth/devices", nil, user.ID)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var got []model.Device
	_ = json.Unmarshal(body, &got)
	if len(got) != 1 || got[0].ID != active.ID {
		t.Fatalf("got %v, want only active device %q", got, active.ID)
	}
}

func TestHandleDevicesList_Anonymous_401(t *testing.T) {
	s := setupDevicesIntegration(t)
	resp, _ := anonRequest(t, s.server, http.MethodGet, "/auth/devices", nil)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}
}

func TestHandleDevicesList_OtherUserDevicesHidden(t *testing.T) {
	s := setupDevicesIntegration(t)
	userA := seedTestUserDevices(t, s, "owner-a")
	userB := seedTestUserDevices(t, s, "owner-b")
	_, _ = s.devices.Create(context.Background(), userB.ID, "bee-device", nil)

	resp, body := authedRequest(t, s.server, http.MethodGet, "/auth/devices", nil, userA.ID)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var got []model.Device
	_ = json.Unmarshal(body, &got)
	if len(got) != 0 {
		t.Fatalf("user A sees %d devices, want 0 (user B's devices must be hidden)", len(got))
	}
}

// User with 0 devices — response body MUST be `[]`, not `null`. Go's
// nil slice marshals to `null` by default; handler must coerce.
func TestHandleDevicesList_ZeroDevices_EmptyArray(t *testing.T) {
	s := setupDevicesIntegration(t)
	user := seedTestUserDevices(t, s, "zero")

	resp, body := authedRequest(t, s.server, http.MethodGet, "/auth/devices", nil, user.ID)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	got := strings.TrimSpace(string(body))
	if got != "[]" {
		t.Fatalf("body = %q, want %q (empty array, not null)", got, "[]")
	}
}

// --- T5: Delete handler ---

func TestHandleDeviceDelete_Happy_200(t *testing.T) {
	s := setupDevicesIntegration(t)
	user := seedTestUserDevices(t, s, "del1")
	dev, _ := s.devices.Create(context.Background(), user.ID, "to-delete", nil)
	// Seed a refresh — should be revoked by delete.
	_ = s.refreshStore.CreateForDevice(context.Background(), user.ID, dev.ID, "del-hash", time.Now().Add(time.Hour))

	resp, body := authedRequest(t, s.server, http.MethodDelete, "/auth/devices/"+dev.ID, nil, user.ID)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", resp.StatusCode, string(body))
	}

	// Device should be revoked.
	got, _ := s.devices.FindByID(context.Background(), dev.ID)
	if got.RevokedAt == nil {
		t.Fatal("device.revoked_at must be set")
	}
	// Refresh should be revoked.
	rt, _ := s.refreshStore.FindByHash(context.Background(), "del-hash")
	if rt.RevokedAt == nil {
		t.Fatal("device refresh must be revoked")
	}
}

func TestHandleDeviceDelete_NotFound_404(t *testing.T) {
	s := setupDevicesIntegration(t)
	user := seedTestUserDevices(t, s, "del2")

	resp, _ := authedRequest(t, s.server, http.MethodDelete, "/auth/devices/00000000-0000-0000-0000-000000000000", nil, user.ID)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
}

// IDOR guard — A authenticated as user X must not be able to delete user Y's device.
func TestHandleDeviceDelete_DifferentUser_404(t *testing.T) {
	s := setupDevicesIntegration(t)
	owner := seedTestUserDevices(t, s, "owner")
	attacker := seedTestUserDevices(t, s, "attacker")
	dev, _ := s.devices.Create(context.Background(), owner.ID, "owner-device", nil)

	resp, _ := authedRequest(t, s.server, http.MethodDelete, "/auth/devices/"+dev.ID, nil, attacker.ID)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 (IDOR — non-disclosure)", resp.StatusCode)
	}
	// Device must remain active for the rightful owner.
	got, _ := s.devices.FindByID(context.Background(), dev.ID)
	if got.RevokedAt != nil {
		t.Fatal("attacker must not be able to revoke owner's device")
	}
}

func TestHandleDeviceDelete_AlreadyRevoked_404(t *testing.T) {
	s := setupDevicesIntegration(t)
	user := seedTestUserDevices(t, s, "del3")
	dev, _ := s.devices.Create(context.Background(), user.ID, "already", nil)
	_ = s.devices.Revoke(context.Background(), dev.ID)

	resp, _ := authedRequest(t, s.server, http.MethodDelete, "/auth/devices/"+dev.ID, nil, user.ID)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 (already revoked)", resp.StatusCode)
	}
}

func TestHandleDeviceDelete_Anonymous_401(t *testing.T) {
	s := setupDevicesIntegration(t)
	resp, _ := anonRequest(t, s.server, http.MethodDelete, "/auth/devices/00000000-0000-0000-0000-000000000000", nil)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}
}

// Invalid UUID format hits pgx 22P02 (invalid_text_representation).
// Handler must surface 404 for non-disclosure consistency with NotFound.
func TestHandleDeviceDelete_InvalidUUID_404(t *testing.T) {
	s := setupDevicesIntegration(t)
	user := seedTestUserDevices(t, s, "del4")

	resp, _ := authedRequest(t, s.server, http.MethodDelete, "/auth/devices/not-a-uuid", nil, user.ID)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 (invalid UUID → consistent non-disclosure)", resp.StatusCode)
	}
}
