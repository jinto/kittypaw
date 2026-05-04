package connect

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/kittypaw-app/kittyportal/internal/auth"
)

const (
	GmailProviderID    = "gmail"
	GmailReadOnlyScope = "openid email profile https://www.googleapis.com/auth/gmail.readonly"

	defaultGmailAuthURL     = "https://accounts.google.com/o/oauth2/v2/auth"
	defaultGmailTokenURL    = "https://oauth2.googleapis.com/token"
	defaultGmailUserInfoURL = "https://www.googleapis.com/oauth2/v2/userinfo"
)

type GmailConfig struct {
	ClientID     string
	ClientSecret string
	BaseURL      string
	AuthURL      string
	TokenURL     string
	UserInfoURL  string
}

type GmailProvider struct {
	cfg    GmailConfig
	client *http.Client
}

func NewGmailProvider(cfg GmailConfig, client *http.Client) *GmailProvider {
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	cfg.BaseURL = strings.TrimRight(cfg.BaseURL, "/")
	return &GmailProvider{cfg: cfg, client: client}
}

func (p *GmailProvider) AuthURL(state, verifier string) string {
	params := url.Values{
		"client_id":              {p.cfg.ClientID},
		"redirect_uri":           {p.redirectURL()},
		"response_type":          {"code"},
		"scope":                  {GmailReadOnlyScope},
		"state":                  {state},
		"code_challenge":         {auth.ChallengeS256(verifier)},
		"code_challenge_method":  {"S256"},
		"access_type":            {"offline"},
		"include_granted_scopes": {"true"},
	}
	return p.authURL() + "?" + params.Encode()
}

func (p *GmailProvider) ExchangeCode(ctx context.Context, code, verifier string) (TokenSet, error) {
	values := url.Values{
		"code":          {code},
		"client_id":     {p.cfg.ClientID},
		"client_secret": {p.cfg.ClientSecret},
		"redirect_uri":  {p.redirectURL()},
		"grant_type":    {"authorization_code"},
		"code_verifier": {verifier},
	}
	tokens, err := p.postToken(ctx, values)
	if err != nil {
		return TokenSet{}, err
	}
	email, err := p.fetchEmail(ctx, tokens.AccessToken)
	if err != nil {
		return TokenSet{}, err
	}
	tokens.Email = email
	return tokens, nil
}

func (p *GmailProvider) Refresh(ctx context.Context, refreshToken string) (TokenSet, error) {
	values := url.Values{
		"client_id":     {p.cfg.ClientID},
		"client_secret": {p.cfg.ClientSecret},
		"refresh_token": {refreshToken},
		"grant_type":    {"refresh_token"},
	}
	return p.postToken(ctx, values)
}

func (p *GmailProvider) postToken(ctx context.Context, values url.Values) (TokenSet, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.tokenURL(), strings.NewReader(values.Encode()))
	if err != nil {
		return TokenSet{}, fmt.Errorf("build token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := p.client.Do(req)
	if err != nil {
		return TokenSet{}, fmt.Errorf("token request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return TokenSet{}, fmt.Errorf("token response %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var body struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		TokenType    string `json:"token_type"`
		ExpiresIn    int    `json:"expires_in"`
		Scope        string `json:"scope"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return TokenSet{}, fmt.Errorf("decode token: %w", err)
	}
	if body.TokenType == "" {
		body.TokenType = "Bearer"
	}
	return TokenSet{
		Provider:     GmailProviderID,
		AccessToken:  body.AccessToken,
		RefreshToken: body.RefreshToken,
		TokenType:    body.TokenType,
		ExpiresIn:    body.ExpiresIn,
		Scope:        body.Scope,
		IssuedAt:     time.Now().UTC(),
	}, nil
}

func (p *GmailProvider) fetchEmail(ctx context.Context, accessToken string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.userInfoURL(), nil)
	if err != nil {
		return "", fmt.Errorf("build userinfo request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	resp, err := p.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("userinfo request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("userinfo response %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var body struct {
		Email string `json:"email"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return "", fmt.Errorf("decode userinfo: %w", err)
	}
	return body.Email, nil
}

func (p *GmailProvider) redirectURL() string {
	return strings.TrimRight(p.cfg.BaseURL, "/") + "/connect/gmail/callback"
}

func (p *GmailProvider) authURL() string {
	if p.cfg.AuthURL != "" {
		return p.cfg.AuthURL
	}
	return defaultGmailAuthURL
}

func (p *GmailProvider) tokenURL() string {
	if p.cfg.TokenURL != "" {
		return p.cfg.TokenURL
	}
	return defaultGmailTokenURL
}

func (p *GmailProvider) userInfoURL() string {
	if p.cfg.UserInfoURL != "" {
		return p.cfg.UserInfoURL
	}
	return defaultGmailUserInfoURL
}
