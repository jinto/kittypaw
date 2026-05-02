package auth

import (
	"crypto/rsa"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/golang-jwt/jwt/v5"

	"github.com/kittypaw-app/kittyapi/internal/model"
)

const (
	// DeviceAccessTokenTTL — daemon access JWT lifetime. Matches user
	// AccessTokenTTL by design (single TTL across the cutover).
	DeviceAccessTokenTTL = 15 * time.Minute

	// DeviceRefreshTokenTTL — opaque refresh token lifetime. Longer
	// than user refresh because daemons run continuously.
	DeviceRefreshTokenTTL = 30 * 24 * time.Hour

	// maxDevicePairBodyBytes caps the pair request body. capabilities
	// is a nested object (daemon_version, supported_protocols, etc.) —
	// 4 KiB is enough for legitimate payloads while still bounding abuse.
	maxDevicePairBodyBytes = 4 * 1024
)

// deviceClaimsPayload is the wire shape of a device JWT. Mirrors
// testfixture.DeviceClaims's payload struct so the cross-team
// contract test (TestSignDeviceJWT_WireFormatMatchesIssueDeviceJWT)
// pins both ends to the same structure. Drift here = silent verifier
// breakage on kittychat side.
//
// docs/specs/kittychat-credential-foundation.md D5.
type deviceClaimsPayload struct {
	UserID string   `json:"user_id"`
	Scope  []string `json:"scope"`
	V      int      `json:"v"`
	jwt.RegisteredClaims
}

// SignDeviceJWT issues an RS256 device JWT.
//
// Wire format (Plan 23 PR-D + spec D5):
//   - alg=RS256, kid in header
//   - sub=device:<deviceID>, user_id=<userID> as separate claim
//   - aud=[AudienceChat], scope=[ScopeDaemonConnect], v=ClaimsVersion
//   - iss=Issuer, iat/exp set
//
// The user_id is a separate claim (not embedded in sub) so kittychat's
// verifier can extract user scope without parsing the sub prefix —
// matches IssueDeviceJWT (testfixture/jwt.go).
func SignDeviceJWT(userID, deviceID string, key *rsa.PrivateKey, kid string, ttl time.Duration) (string, error) {
	if key == nil {
		return "", fmt.Errorf("private key is nil")
	}
	if kid == "" {
		return "", fmt.Errorf("kid is empty")
	}
	now := time.Now()
	payload := deviceClaimsPayload{
		UserID: userID,
		Scope:  []string{ScopeDaemonConnect},
		V:      ClaimsVersion,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    Issuer,
			Subject:   "device:" + deviceID,
			Audience:  jwt.ClaimStrings{AudienceChat},
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(ttl)),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, payload)
	token.Header["kid"] = kid
	return token.SignedString(key)
}

// pairRequest is the JSON body for POST /auth/devices/pair. Both
// fields are optional from the daemon's perspective: name defaults
// to a daemon-supplied label, capabilities defaults to {}.
type pairRequest struct {
	Name         string         `json:"name"`
	Capabilities map[string]any `json:"capabilities"`
}

// pairResponse is the JSON shape returned by /auth/devices/pair and
// /auth/devices/refresh. Plan 23 contract.
type pairResponse struct {
	DeviceID           string `json:"device_id"`
	DeviceAccessToken  string `json:"device_access_token"`
	DeviceRefreshToken string `json:"device_refresh_token"`
	ExpiresIn          int    `json:"expires_in"`
}

// HandlePair issues a fresh device JWT + refresh token after the
// authenticated user pairs a daemon. The handler uses sequential
// explicit revoke (Plan 23 결정 2) — if any post-Create step fails,
// we revoke the just-created device row to prevent ghost listings.
//
// Rationale: pgx transaction wrapping would be cleaner but DeviceStore
// + RefreshTokenStore (PR-C) don't expose tx semantics; the soft-delete
// (revoked_at) column makes compensating-revoke a first-class option.
func (h *OAuthHandler) HandlePair() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := UserFromContext(r.Context())
		if user == nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		r.Body = http.MaxBytesReader(w, r.Body, maxDevicePairBodyBytes)
		// DisallowUnknownFields intentionally NOT used — spec D4
		// contract is forward-compat (unknown fields are ignored).
		// Daemons may add new pair-request fields ahead of server bumps.
		var req pairRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}
		// `name` is optional per spec (silly-wiggling-balloon.md L222).
		// An unnamed device is valid — daemon may label later via a
		// future PATCH endpoint.

		ctx := r.Context()
		dev, err := h.DeviceStore.Create(ctx, user.ID, req.Name, req.Capabilities)
		if err != nil {
			slog.Error("device create failed", "user_id", user.ID, "err", err)
			http.Error(w, "failed to pair device", http.StatusInternalServerError)
			return
		}

		// From here on, any failure must compensate — sequential explicit
		// revoke (Plan 23 결정 2). defer+bool would be footgun-prone.
		accessToken, err := SignDeviceJWT(user.ID, dev.ID, h.JWTPrivateKey, h.JWTKID, DeviceAccessTokenTTL)
		if err != nil {
			_ = h.DeviceStore.Revoke(ctx, dev.ID)
			slog.Error("device JWT sign failed", "user_id", user.ID, "device_id", dev.ID, "err", err)
			http.Error(w, "failed to issue token", http.StatusInternalServerError)
			return
		}

		rawRefresh, err := GenerateRefreshToken()
		if err != nil {
			_ = h.DeviceStore.Revoke(ctx, dev.ID)
			slog.Error("refresh token generate failed", "user_id", user.ID, "device_id", dev.ID, "err", err)
			http.Error(w, "failed to issue token", http.StatusInternalServerError)
			return
		}
		hash := HashRefreshToken(rawRefresh)
		if err := h.RefreshTokenStore.CreateForDevice(ctx, user.ID, dev.ID, hash, time.Now().Add(DeviceRefreshTokenTTL)); err != nil {
			_ = h.DeviceStore.Revoke(ctx, dev.ID)
			slog.Error("refresh token store failed", "user_id", user.ID, "device_id", dev.ID, "err", err)
			http.Error(w, "failed to issue token", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(pairResponse{
			DeviceID:           dev.ID,
			DeviceAccessToken:  accessToken,
			DeviceRefreshToken: rawRefresh,
			ExpiresIn:          int(DeviceAccessTokenTTL.Seconds()),
		})
	}
}

// HandleDeviceRefresh rotates a device-scoped opaque refresh token.
//
// Authentication: opaque token in body is the only credential. This
// route is wired OUTSIDE the user-aud middleware (Plan 23 결정 3) so a
// daemon's stale Authorization header can't trip the user-aud check
// before this handler runs.
//
// Reuse detection: presenting an already-revoked refresh token revokes
// every active refresh for the same device (Plan 23 결정 3 — RevokeAllForDevice).
// User-scoped refresh (device_id NULL) is rejected — that's /auth/token/refresh's
// job, not this device-only endpoint.
func (h *OAuthHandler) HandleDeviceRefresh() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, maxAuthBodyBytes)
		var req struct {
			RefreshToken string `json:"refresh_token"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.RefreshToken == "" {
			http.Error(w, "refresh_token required", http.StatusBadRequest)
			return
		}

		ctx := r.Context()
		hash := HashRefreshToken(req.RefreshToken)
		rt, err := h.RefreshTokenStore.FindByHash(ctx, hash)
		if err != nil {
			// Unknown hash → silent 401 (don't disclose existence).
			http.Error(w, "invalid refresh token", http.StatusUnauthorized)
			return
		}
		// Device-only endpoint guard.
		if rt.DeviceID == nil {
			http.Error(w, "invalid refresh token", http.StatusUnauthorized)
			return
		}
		// Reuse detection — already-revoked refresh signals a leaked token.
		// Revoke every active device refresh on the same device.
		if rt.RevokedAt != nil {
			if rerr := h.RefreshTokenStore.RevokeAllForDevice(ctx, *rt.DeviceID); rerr != nil {
				slog.Error("RevokeAllForDevice failed during reuse-detect", "device_id", *rt.DeviceID, "err", rerr)
			}
			http.Error(w, "invalid refresh token", http.StatusUnauthorized)
			return
		}
		if rt.ExpiresAt.Before(time.Now()) {
			http.Error(w, "invalid refresh token", http.StatusUnauthorized)
			return
		}

		// Race-aware revoke: returns false if a concurrent request beat us.
		revoked, err := h.RefreshTokenStore.RevokeIfActive(ctx, rt.ID)
		if err != nil {
			slog.Error("RevokeIfActive failed", "id", rt.ID, "err", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		if !revoked {
			// Race-loser — another request just rotated this row.
			http.Error(w, "invalid refresh token", http.StatusUnauthorized)
			return
		}

		// Verify device is still active (not deleted).
		dev, err := h.DeviceStore.FindByID(ctx, *rt.DeviceID)
		if err != nil {
			http.Error(w, "invalid refresh token", http.StatusUnauthorized)
			return
		}
		if dev.RevokedAt != nil {
			http.Error(w, "invalid refresh token", http.StatusUnauthorized)
			return
		}

		// Issue new pair.
		accessToken, err := SignDeviceJWT(rt.UserID, dev.ID, h.JWTPrivateKey, h.JWTKID, DeviceAccessTokenTTL)
		if err != nil {
			slog.Error("SignDeviceJWT failed", "device_id", dev.ID, "err", err)
			http.Error(w, "failed to issue token", http.StatusInternalServerError)
			return
		}
		rawRefresh, err := GenerateRefreshToken()
		if err != nil {
			slog.Error("GenerateRefreshToken failed", "device_id", dev.ID, "err", err)
			http.Error(w, "failed to issue token", http.StatusInternalServerError)
			return
		}
		newHash := HashRefreshToken(rawRefresh)
		if err := h.RefreshTokenStore.CreateForDevice(ctx, rt.UserID, dev.ID, newHash, time.Now().Add(DeviceRefreshTokenTTL)); err != nil {
			slog.Error("CreateForDevice failed", "device_id", dev.ID, "err", err)
			http.Error(w, "failed to issue token", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(pairResponse{
			DeviceID:           dev.ID,
			DeviceAccessToken:  accessToken,
			DeviceRefreshToken: rawRefresh,
			ExpiresIn:          int(DeviceAccessTokenTTL.Seconds()),
		})
	}
}

// HandleDevicesList returns the authenticated user's active devices,
// sorted by paired_at DESC (PR-C ListActiveForUser contract).
//
// Empty result MUST encode as `[]`, not `null` — Go's nil slice
// marshaling default would surface as null and break clients that
// type the field as `Array<Device>`.
func (h *OAuthHandler) HandleDevicesList() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := UserFromContext(r.Context())
		if user == nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		list, err := h.DeviceStore.ListActiveForUser(r.Context(), user.ID)
		if err != nil {
			slog.Error("ListActiveForUser failed", "user_id", user.ID, "err", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		if list == nil {
			list = []*model.Device{}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(list)
	}
}

// HandleDeviceDelete soft-deletes a device the authenticated user owns,
// then revokes every active device-scoped refresh token for that device.
//
// All "not your device" cases (missing, invalid UUID, owned by another
// user, already revoked) collapse to 404 — non-disclosure (Plan 23 결정 5).
// Refresh revoke runs first; a failure there leaves the device active
// rather than orphaning live tokens after a half-completed delete.
func (h *OAuthHandler) HandleDeviceDelete() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := UserFromContext(r.Context())
		if user == nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		deviceID := chi.URLParam(r, "id")
		if deviceID == "" {
			http.Error(w, "device not found", http.StatusNotFound)
			return
		}

		ctx := r.Context()
		dev, err := h.DeviceStore.FindByID(ctx, deviceID)
		if err != nil {
			// ErrNotFound, invalid UUID (pgx 22P02), or any other lookup
			// error — collapse to 404 for non-disclosure consistency.
			http.Error(w, "device not found", http.StatusNotFound)
			return
		}
		if dev.UserID != user.ID || dev.RevokedAt != nil {
			http.Error(w, "device not found", http.StatusNotFound)
			return
		}

		// Refresh revoke first — if it fails, device stays alive (no
		// half-deleted state with orphan refresh).
		if err := h.RefreshTokenStore.RevokeAllForDevice(ctx, deviceID); err != nil {
			slog.Error("RevokeAllForDevice failed", "device_id", deviceID, "err", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		if err := h.DeviceStore.Revoke(ctx, deviceID); err != nil {
			slog.Error("DeviceStore.Revoke failed", "device_id", deviceID, "err", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("{}"))
	}
}
