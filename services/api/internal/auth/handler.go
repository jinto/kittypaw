package auth

import (
	"encoding/json"
	"log"
	"net/http"
	"time"

	"github.com/jinto/kittypaw-api/internal/model"
)

const (
	AccessTokenTTL  = 15 * time.Minute
	RefreshTokenTTL = 7 * 24 * time.Hour
)

type TokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
}

type OAuthHandler struct {
	UserStore         model.UserStore
	RefreshTokenStore model.RefreshTokenStore
	StateStore        *StateStore
	JWTSecret         string
	HTTPClient        *http.Client

	// Overridable for testing.
	GoogleTokenURL    string
	GoogleUserInfoURL string
	GitHubTokenURL    string
	GitHubUserURL     string
}

func (h *OAuthHandler) issueTokens(w http.ResponseWriter, r *http.Request, user *model.User) {
	accessToken, err := Sign(user.ID, h.JWTSecret, AccessTokenTTL)
	if err != nil {
		log.Printf("jwt sign: %v", err)
		http.Error(w, "token generation failed", http.StatusInternalServerError)
		return
	}

	rawRefresh, err := GenerateRefreshToken()
	if err != nil {
		log.Printf("refresh token generate: %v", err)
		http.Error(w, "token generation failed", http.StatusInternalServerError)
		return
	}

	hash := HashRefreshToken(rawRefresh)
	if err := h.RefreshTokenStore.Create(r.Context(), user.ID, hash, time.Now().Add(RefreshTokenTTL)); err != nil {
		log.Printf("refresh token store: %v", err)
		http.Error(w, "token storage failed", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(TokenResponse{
		AccessToken:  accessToken,
		RefreshToken: rawRefresh,
		TokenType:    "Bearer",
		ExpiresIn:    int(AccessTokenTTL.Seconds()),
	})
}
