package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"regexp"
	"path/filepath"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/jinto/gopaw/core"
)

// ---------------------------------------------------------------------------
// GET /api/bootstrap
// ---------------------------------------------------------------------------

func (s *Server) handleBootstrap(w http.ResponseWriter, r *http.Request) {
	if !isLocalhost(r) {
		writeError(w, http.StatusForbidden, "bootstrap only allowed from localhost")
		return
	}

	s.configMu.RLock()
	apiKey := s.config.Server.APIKey
	s.configMu.RUnlock()

	scheme := "ws"
	if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
		scheme = "wss"
	}
	wsURL := fmt.Sprintf("%s://%s/ws", scheme, r.Host)

	writeJSON(w, http.StatusOK, map[string]any{
		"api_key": apiKey,
		"ws_url":  wsURL,
	})
}

// ---------------------------------------------------------------------------
// GET /api/setup/status
// ---------------------------------------------------------------------------

func (s *Server) handleSetupStatus(w http.ResponseWriter, _ *http.Request) {
	completed := s.isOnboardingCompleted()

	s.configMu.RLock()
	cfg := s.config
	s.configMu.RUnlock()

	// Determine existing LLM provider from live config.
	var existingProvider *string
	if cfg.LLM.APIKey != "" && cfg.LLM.Provider != "" {
		p := cfg.LLM.Provider
		existingProvider = &p
	}
	if existingProvider == nil && cfg.LLM.BaseURL != "" {
		p := "local"
		existingProvider = &p
	}

	// Check configured channels.
	hasTelegram := false
	var telegramChatID *string
	hasKakao := false

	for _, ch := range cfg.Channels {
		switch ch.ChannelType {
		case core.ChannelTelegram:
			hasTelegram = true
		case core.ChannelKakaoTalk:
			hasKakao = true
		}
	}

	// Also check pending setup state (wizard in progress).
	if !hasTelegram {
		if v, ok, _ := s.store.GetUserContext("setup:telegram_bot_token"); ok && v != "" {
			hasTelegram = true
		}
	}
	if hasTelegram {
		if v, ok, _ := s.store.GetUserContext("setup:telegram_chat_id"); ok && v != "" {
			masked := maskValue(v)
			telegramChatID = &masked
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"completed":         completed,
		"existing_provider": existingProvider,
		"has_telegram":      hasTelegram,
		"telegram_chat_id":  telegramChatID,
		"has_kakao":         hasKakao,
		"kakao_available":   true,
	})
}

// ---------------------------------------------------------------------------
// POST /api/setup/llm
// ---------------------------------------------------------------------------

func (s *Server) handleSetupLlm(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Provider   string `json:"provider"`
		APIKey     string `json:"api_key"`
		LocalURL   string `json:"local_url"`
		LocalModel string `json:"local_model"`
	}
	if !decodeBody(w, r, &body) {
		return
	}

	switch body.Provider {
	case "claude", "anthropic":
		if body.APIKey == "" {
			writeError(w, http.StatusBadRequest, "api_key is required for Claude")
			return
		}
		s.store.SetUserContext("setup:llm_provider", "anthropic", "setup")
		s.store.SetUserContext("setup:llm_api_key", body.APIKey, "setup")
		s.store.SetUserContext("setup:llm_model", "claude-sonnet-4-20250514", "setup")
		s.store.SetUserContext("setup:llm_base_url", "", "setup")

	case "openrouter":
		if body.APIKey == "" {
			writeError(w, http.StatusBadRequest, "api_key is required for OpenRouter")
			return
		}
		s.store.SetUserContext("setup:llm_provider", "openai", "setup")
		s.store.SetUserContext("setup:llm_api_key", body.APIKey, "setup")
		s.store.SetUserContext("setup:llm_base_url", "https://openrouter.ai/api/v1/chat/completions", "setup")
		s.store.SetUserContext("setup:llm_model", "qwen/qwen3-235b-a22b:free", "setup")

	case "local":
		if body.LocalURL == "" || body.LocalModel == "" {
			writeError(w, http.StatusBadRequest, "local_url and local_model are required")
			return
		}
		baseURL := strings.TrimRight(body.LocalURL, "/") + "/chat/completions"
		s.store.SetUserContext("setup:llm_provider", "openai", "setup")
		s.store.SetUserContext("setup:llm_api_key", "", "setup")
		s.store.SetUserContext("setup:llm_base_url", baseURL, "setup")
		s.store.SetUserContext("setup:llm_model", body.LocalModel, "setup")

	default:
		writeError(w, http.StatusBadRequest, "invalid provider")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"saved": true, "provider": body.Provider})
}

// ---------------------------------------------------------------------------
// POST /api/setup/telegram
// ---------------------------------------------------------------------------

func (s *Server) handleSetupTelegram(w http.ResponseWriter, r *http.Request) {
	var body struct {
		BotToken string `json:"bot_token"`
		ChatID   string `json:"chat_id"`
	}
	if !decodeBody(w, r, &body) {
		return
	}
	if body.BotToken == "" || body.ChatID == "" {
		writeError(w, http.StatusBadRequest, "bot_token and chat_id are required")
		return
	}

	s.store.SetUserContext("setup:telegram_bot_token", body.BotToken, "setup")
	s.store.SetUserContext("setup:telegram_chat_id", body.ChatID, "setup")

	writeJSON(w, http.StatusOK, map[string]any{"saved": true})
}

// ---------------------------------------------------------------------------
// POST /api/setup/telegram/chat-id
// ---------------------------------------------------------------------------

func (s *Server) handleSetupTelegramChatID(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Token string `json:"token"`
	}
	if !decodeBody(w, r, &body) {
		return
	}
	if body.Token == "" {
		writeError(w, http.StatusBadRequest, "token is required")
		return
	}

	chatID, err := fetchTelegramChatID(r.Context(), body.Token)
	if err != nil {
		writeError(w, http.StatusBadRequest, "failed to fetch chat ID: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"chat_id": chatID})
}

// ---------------------------------------------------------------------------
// POST /api/setup/kakao/register
// ---------------------------------------------------------------------------

func (s *Server) handleSetupKakaoRegister(w http.ResponseWriter, _ *http.Request) {
	writeError(w, http.StatusServiceUnavailable, "kakao registration requires the relay server")
}

// ---------------------------------------------------------------------------
// GET /api/setup/kakao/pair-status
// ---------------------------------------------------------------------------

func (s *Server) handleSetupKakaoPairStatus(w http.ResponseWriter, _ *http.Request) {
	hasKakao := false
	s.configMu.RLock()
	for _, ch := range s.config.Channels {
		if ch.ChannelType == core.ChannelKakaoTalk {
			hasKakao = true
			break
		}
	}
	s.configMu.RUnlock()

	writeJSON(w, http.StatusOK, map[string]any{"paired": hasKakao})
}

// ---------------------------------------------------------------------------
// POST /api/setup/workspace
// ---------------------------------------------------------------------------

func (s *Server) handleSetupWorkspace(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Path string `json:"path"`
	}
	if !decodeBody(w, r, &body) {
		return
	}
	if body.Path == "" || !filepath.IsAbs(body.Path) {
		writeError(w, http.StatusBadRequest, "absolute path is required")
		return
	}

	info, err := os.Stat(body.Path)
	if err != nil || !info.IsDir() {
		writeError(w, http.StatusBadRequest, "path does not exist or is not a directory")
		return
	}

	canonical, err := filepath.EvalSymlinks(body.Path)
	if err != nil {
		canonical = body.Path
	}

	s.store.SetUserContext("setup:workspace_path", canonical, "setup")
	writeJSON(w, http.StatusOK, map[string]any{"saved": true, "path": canonical})
}

// ---------------------------------------------------------------------------
// POST /api/setup/http-access
// ---------------------------------------------------------------------------

func (s *Server) handleSetupHttpAccess(w http.ResponseWriter, _ *http.Request) {
	if err := s.store.GrantCapability("http"); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"granted": true})
}

// ---------------------------------------------------------------------------
// POST /api/setup/complete
// ---------------------------------------------------------------------------

func (s *Server) handleSetupComplete(w http.ResponseWriter, _ *http.Request) {
	if s.isOnboardingCompleted() {
		writeError(w, http.StatusConflict, "already completed")
		return
	}

	if err := s.generateConfig(); err != nil {
		slog.Error("setup: generate config failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to generate config: "+err.Error())
		return
	}

	s.store.SetUserContext("onboarding_completed", "true", "system")

	// Hot-reload the config into the running server.
	cfgPath, _ := core.ConfigPath()
	if cfg, err := core.LoadConfig(cfgPath); err == nil {
		s.configMu.Lock()
		*s.config = *cfg
		s.configMu.Unlock()
		slog.Info("setup: config reloaded after onboarding")
	}

	writeJSON(w, http.StatusOK, map[string]any{"completed": true})
}

// ---------------------------------------------------------------------------
// POST /api/setup/reset
// ---------------------------------------------------------------------------

func (s *Server) handleSetupReset(w http.ResponseWriter, r *http.Request) {
	if !isLocalhost(r) {
		writeError(w, http.StatusForbidden, "reset only allowed from localhost")
		return
	}
	if origin := r.Header.Get("Origin"); origin != "" && !isLocalhostOrigin(origin) {
		writeError(w, http.StatusForbidden, "cross-origin reset not allowed")
		return
	}

	s.store.SetUserContext("onboarding_completed", "false", "system")
	writeJSON(w, http.StatusOK, map[string]any{"reset": true})
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func (s *Server) isOnboardingCompleted() bool {
	v, ok, _ := s.store.GetUserContext("onboarding_completed")
	return ok && v == "true"
}

// requireOnboardingIncomplete blocks mutating setup endpoints after
// onboarding is complete.
func (s *Server) requireOnboardingIncomplete(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.isOnboardingCompleted() {
			writeError(w, http.StatusForbidden, "setup already completed")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func isLocalhost(r *http.Request) bool {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func isLocalhostOrigin(origin string) bool {
	return strings.HasPrefix(origin, "http://localhost") ||
		strings.HasPrefix(origin, "http://127.0.0.1") ||
		strings.HasPrefix(origin, "https://localhost") ||
		strings.HasPrefix(origin, "https://127.0.0.1")
}

// requireLocalhost blocks requests that don't originate from loopback.
func (s *Server) requireLocalhost(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !isLocalhost(r) {
			writeError(w, http.StatusForbidden, "access restricted to localhost")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func maskValue(v string) string {
	if len(v) <= 4 {
		return "***"
	}
	return "***" + v[len(v)-4:]
}

// generateConfig merges wizard settings into the existing config.toml.
func (s *Server) generateConfig() error {
	cfgPath, err := core.ConfigPath()
	if err != nil {
		return err
	}

	// Load existing config or start from defaults.
	cfg := core.DefaultConfig()
	if data, readErr := os.ReadFile(cfgPath); readErr == nil {
		if err := toml.Unmarshal(data, &cfg); err != nil {
			return fmt.Errorf("existing config.toml has syntax errors: %w", err)
		}
	}

	cfg.FreeformFallback = true

	// LLM
	if v, ok, _ := s.store.GetUserContext("setup:llm_provider"); ok && v != "" {
		cfg.LLM.Provider = v
	}
	if v, ok, _ := s.store.GetUserContext("setup:llm_api_key"); ok {
		cfg.LLM.APIKey = v
	}
	if v, ok, _ := s.store.GetUserContext("setup:llm_model"); ok && v != "" {
		cfg.LLM.Model = v
	}
	if v, ok, _ := s.store.GetUserContext("setup:llm_base_url"); ok {
		cfg.LLM.BaseURL = v
	}
	if cfg.LLM.MaxTokens == 0 {
		cfg.LLM.MaxTokens = 4096
	}

	// Channels — only replace wizard-managed types when setup values exist.
	hasTelegramSetup := false
	if v, ok, _ := s.store.GetUserContext("setup:telegram_bot_token"); ok && v != "" {
		hasTelegramSetup = true
	}
	hasKakaoSetup := false
	if v, ok, _ := s.store.GetUserContext("setup:kakao_relay_url"); ok && v != "" {
		hasKakaoSetup = true
	}

	var kept []core.ChannelConfig
	for _, ch := range cfg.Channels {
		if ch.ChannelType == core.ChannelTelegram && hasTelegramSetup {
			continue // will be replaced below
		}
		if ch.ChannelType == core.ChannelKakaoTalk && hasKakaoSetup {
			continue // will be replaced below
		}
		kept = append(kept, ch)
	}

	// Telegram — add from wizard only if user configured it.
	if hasTelegramSetup {
		tok, _, _ := s.store.GetUserContext("setup:telegram_bot_token")
		kept = append(kept, core.ChannelConfig{
			ChannelType: core.ChannelTelegram,
			Token:       tok,
		})
		if cid, ok2, _ := s.store.GetUserContext("setup:telegram_chat_id"); ok2 && cid != "" {
			cfg.AdminChatIDs = []string{cid}
		}
	}

	// KakaoTalk — add from wizard only if user configured it.
	if hasKakaoSetup {
		relay, _, _ := s.store.GetUserContext("setup:kakao_relay_url")
		userToken, _, _ := s.store.GetUserContext("setup:kakao_user_token")
		kept = append(kept, core.ChannelConfig{
			ChannelType: core.ChannelKakaoTalk,
			Kakao: &core.KakaoChannelConfig{
				RelayURL:  relay,
				UserToken: userToken,
			},
		})
	}

	cfg.Channels = kept

	// Sandbox defaults
	if cfg.Sandbox.AllowedHosts == nil {
		cfg.Sandbox.AllowedHosts = []string{}
	}

	// Workspace → sandbox allowed paths
	if ws, ok, _ := s.store.GetUserContext("setup:workspace_path"); ok && ws != "" {
		cfg.Sandbox.AllowedPaths = []string{ws}
	}

	// Write config atomically: tmp file + rename.
	var buf bytes.Buffer
	enc := toml.NewEncoder(&buf)
	if err := enc.Encode(cfg); err != nil {
		return fmt.Errorf("encode config: %w", err)
	}

	tmpPath := cfgPath + ".tmp"
	if err := os.WriteFile(tmpPath, buf.Bytes(), 0o600); err != nil {
		return fmt.Errorf("write tmp config: %w", err)
	}
	if err := os.Rename(tmpPath, cfgPath); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("rename config: %w", err)
	}

	return nil
}

var telegramTokenRe = regexp.MustCompile(`^\d+:[A-Za-z0-9_-]{30,50}$`)

// fetchTelegramChatID calls the Telegram Bot API getUpdates to discover the
// chat ID from the most recent message.
func fetchTelegramChatID(ctx context.Context, token string) (string, error) {
	if !telegramTokenRe.MatchString(token) {
		return "", fmt.Errorf("invalid token format")
	}
	url := "https://api.telegram.org/bot" + token + "/getUpdates?limit=1&timeout=0"

	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", err
	}

	var result struct {
		OK     bool `json:"ok"`
		Result []struct {
			Message struct {
				Chat struct {
					ID int64 `json:"id"`
				} `json:"chat"`
			} `json:"message"`
		} `json:"result"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("decode response: %w", err)
	}
	if !result.OK {
		return "", fmt.Errorf("telegram API returned an error")
	}
	if len(result.Result) == 0 {
		return "", fmt.Errorf("no messages found — send a message to the bot first")
	}

	chatID := result.Result[0].Message.Chat.ID
	return fmt.Sprintf("%d", chatID), nil
}
