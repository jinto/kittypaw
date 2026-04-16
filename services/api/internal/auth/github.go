package auth

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strconv"
)

const (
	defaultGitHubTokenURL = "https://github.com/login/oauth/access_token"
	defaultGitHubUserURL  = "https://api.github.com/user"
)

type GitHubConfig struct {
	ClientID     string
	ClientSecret string
	RedirectURL  string
}

type githubUserInfo struct {
	ID        int    `json:"id"`
	Login     string `json:"login"`
	Email     string `json:"email"`
	Name      string `json:"name"`
	AvatarURL string `json:"avatar_url"`
}

func (h *OAuthHandler) gitHubTokenURL() string {
	if h.GitHubTokenURL != "" {
		return h.GitHubTokenURL
	}
	return defaultGitHubTokenURL
}

func (h *OAuthHandler) gitHubUserURL() string {
	if h.GitHubUserURL != "" {
		return h.GitHubUserURL
	}
	return defaultGitHubUserURL
}

func (h *OAuthHandler) HandleGitHubLogin(cfg GitHubConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		verifier, err := GenerateVerifier()
		if err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}

		state, err := h.StateStore.Create(verifier)
		if err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}

		params := url.Values{
			"client_id":             {cfg.ClientID},
			"redirect_uri":          {cfg.RedirectURL},
			"scope":                 {"read:user user:email"},
			"state":                 {state},
			"code_challenge":        {ChallengeS256(verifier)},
			"code_challenge_method": {"S256"},
		}

		http.Redirect(w, r, "https://github.com/login/oauth/authorize?"+params.Encode(), http.StatusFound)
	}
}

func (h *OAuthHandler) HandleGitHubCallback(cfg GitHubConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		code := r.URL.Query().Get("code")
		state := r.URL.Query().Get("state")

		if code == "" || state == "" {
			http.Error(w, "missing code or state", http.StatusBadRequest)
			return
		}

		verifier, err := h.StateStore.Consume(state)
		if err != nil {
			http.Error(w, "invalid state", http.StatusBadRequest)
			return
		}

		token, err := h.exchangeGitHubCode(cfg, code, verifier)
		if err != nil {
			log.Printf("github code exchange: %v", err)
			http.Error(w, "authentication failed", http.StatusBadGateway)
			return
		}

		info, err := h.fetchGitHubUser(token)
		if err != nil {
			log.Printf("github user: %v", err)
			http.Error(w, "authentication failed", http.StatusBadGateway)
			return
		}

		name := info.Name
		if name == "" {
			name = info.Login
		}

		user, err := h.UserStore.CreateOrUpdate(r.Context(), "github", strconv.Itoa(info.ID), info.Email, name, info.AvatarURL)
		if err != nil {
			log.Printf("user upsert: %v", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}

		h.issueTokens(w, r, user)
	}
}

func (h *OAuthHandler) exchangeGitHubCode(cfg GitHubConfig, code, verifier string) (string, error) {
	req, err := http.NewRequest(http.MethodPost, h.gitHubTokenURL(), nil)
	if err != nil {
		return "", err
	}
	req.URL.RawQuery = url.Values{
		"client_id":     {cfg.ClientID},
		"client_secret": {cfg.ClientSecret},
		"code":          {code},
		"code_verifier": {verifier},
	}.Encode()
	req.Header.Set("Accept", "application/json")

	resp, err := h.HTTPClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("token request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("token response %d: %s", resp.StatusCode, body)
	}

	var result struct {
		AccessToken string `json:"access_token"`
		Error       string `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("decode token: %w", err)
	}
	if result.Error != "" {
		return "", fmt.Errorf("github error: %s", result.Error)
	}
	return result.AccessToken, nil
}

func (h *OAuthHandler) fetchGitHubUser(accessToken string) (*githubUserInfo, error) {
	req, err := http.NewRequest(http.MethodGet, h.gitHubUserURL(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := h.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("user request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("user response %d: %s", resp.StatusCode, body)
	}

	var info githubUserInfo
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return nil, fmt.Errorf("decode user: %w", err)
	}
	return &info, nil
}
