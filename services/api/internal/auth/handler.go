package auth

import (
	"encoding/json"
	"log"
	"net/http"
	"time"

	"github.com/kittypaw-app/kittyapi/internal/model"
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
	tokens, err := h.issueTokenPair(r.Context(), user)
	if err != nil {
		log.Printf("issue tokens: %v", err)
		http.Error(w, "token generation failed", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(tokens)
}
