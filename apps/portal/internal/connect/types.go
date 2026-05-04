package connect

import "time"

type TokenSet struct {
	Provider     string    `json:"provider"`
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token,omitempty"`
	TokenType    string    `json:"token_type"`
	ExpiresIn    int       `json:"expires_in,omitempty"`
	Scope        string    `json:"scope,omitempty"`
	Email        string    `json:"email,omitempty"`
	IssuedAt     time.Time `json:"issued_at"`
}
