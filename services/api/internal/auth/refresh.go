package auth

import (
	"encoding/json"
	"log"
	"net/http"
	"time"
)

type refreshRequest struct {
	RefreshToken string `json:"refresh_token"`
}

func (h *OAuthHandler) HandleTokenRefresh() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req refreshRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.RefreshToken == "" {
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}

		hash := HashRefreshToken(req.RefreshToken)

		rt, err := h.RefreshTokenStore.FindByHash(r.Context(), hash)
		if err != nil {
			http.Error(w, "invalid refresh token", http.StatusUnauthorized)
			return
		}

		// Reuse detection: revoked token used → compromise, revoke all.
		if rt.RevokedAt != nil {
			log.Printf("refresh token reuse detected for user %s", rt.UserID)
			_ = h.RefreshTokenStore.RevokeAllForUser(r.Context(), rt.UserID)
			http.Error(w, "token reuse detected", http.StatusUnauthorized)
			return
		}

		// Absolute expiry check.
		if time.Now().After(rt.ExpiresAt) {
			http.Error(w, "refresh token expired", http.StatusUnauthorized)
			return
		}

		// Atomically revoke old token — prevents concurrent rotation race.
		revoked, err := h.RefreshTokenStore.RevokeIfActive(r.Context(), rt.ID)
		if err != nil {
			log.Printf("revoke old refresh: %v", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		if !revoked {
			log.Printf("refresh token reuse detected (concurrent) for user %s", rt.UserID)
			_ = h.RefreshTokenStore.RevokeAllForUser(r.Context(), rt.UserID)
			http.Error(w, "token reuse detected", http.StatusUnauthorized)
			return
		}

		// Find user to issue new tokens.
		user, err := h.UserStore.FindByID(r.Context(), rt.UserID)
		if err != nil {
			log.Printf("find user: %v", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}

		h.issueTokens(w, r, user)
	}
}
