package server

import (
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"

	"github.com/jinto/kittypaw/channel"
	"github.com/jinto/kittypaw/core"
)

// ---------------------------------------------------------------------------
// GET /api/bootstrap
// ---------------------------------------------------------------------------

func (s *Server) handleBootstrap(w http.ResponseWriter, r *http.Request) {
	if !isLocalhost(r) {
		writeError(w, http.StatusForbidden, "bootstrap only allowed from localhost")
		return
	}

	apiKey, ok := s.browserAPIToken(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

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

	// Validate provider-specific requirements.
	switch body.Provider {
	case "claude", "anthropic":
		if body.APIKey == "" {
			writeError(w, http.StatusBadRequest, "api_key is required for Claude")
			return
		}
	case "openrouter":
		if body.APIKey == "" {
			writeError(w, http.StatusBadRequest, "api_key is required for OpenRouter")
			return
		}
	case "local":
		if body.LocalURL == "" || body.LocalModel == "" {
			writeError(w, http.StatusBadRequest, "local_url and local_model are required")
			return
		}
	default:
		writeError(w, http.StatusBadRequest, "invalid provider")
		return
	}

	provider, model, baseURL := core.ResolveLLMConfig(body.Provider, body.LocalURL, body.LocalModel)
	apiKey := body.APIKey
	if body.Provider == "local" {
		apiKey = ""
	}

	_ = s.store.SetUserContext("setup:llm_provider", provider, "setup")
	_ = s.store.SetUserContext("setup:llm_api_key", apiKey, "setup")
	_ = s.store.SetUserContext("setup:llm_model", model, "setup")
	_ = s.store.SetUserContext("setup:llm_base_url", baseURL, "setup")

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
	if !core.ValidateTelegramToken(body.BotToken) {
		writeError(w, http.StatusBadRequest, "invalid bot token format")
		return
	}

	_ = s.store.SetUserContext("setup:telegram_bot_token", body.BotToken, "setup")
	_ = s.store.SetUserContext("setup:telegram_chat_id", body.ChatID, "setup")

	// Immediately spawn the Telegram channel so the user gets instant
	// feedback during onboarding — no reload required (AC3).
	if s.spawner != nil {
		chCfg := core.ChannelConfig{ChannelType: core.ChannelTelegram, Token: body.BotToken}
		accountID := s.defaultAccountID()
		ch, err := channel.FromConfig(accountID, chCfg)
		if err != nil {
			slog.Warn("setup: telegram channel create failed", "error", err)
		} else if err := s.spawner.TrySpawn(accountID, ch, chCfg); err != nil {
			slog.Warn("setup: telegram channel spawn failed", "error", err)
		}
	}

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

	chatID, err := core.FetchTelegramChatID(r.Context(), body.Token)
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
	apiURL, _, _ := s.store.GetUserContext("setup:api_server_url")
	if apiURL == "" {
		apiURL = core.DefaultAPIServerURL
	}
	apiURL = strings.TrimRight(apiURL, "/")

	accountID := s.defaultAccountID()
	secrets, err := core.LoadAccountSecrets(accountID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "load secrets: "+err.Error())
		return
	}
	mgr := core.NewAPITokenManager("", secrets)

	relayURL, ok := mgr.LoadRelayURL(apiURL)
	if !ok || relayURL == "" {
		writeError(w, http.StatusServiceUnavailable, "relay URL not configured — login to the API server first")
		return
	}

	reg, err := core.RegisterRelaySession(relayURL)
	if err != nil {
		writeError(w, http.StatusBadGateway, "relay register: "+err.Error())
		return
	}

	wsURL := core.WSURLFromRelay(relayURL, reg.Token)
	if err := mgr.SaveKakaoRelayURL(apiURL, wsURL); err != nil {
		writeError(w, http.StatusInternalServerError, "save kakao ws url: "+err.Error())
		return
	}

	_ = s.store.SetUserContext("setup:kakao_relay_base", relayURL, "setup")
	_ = s.store.SetUserContext("setup:kakao_relay_token", reg.Token, "setup")
	// Persist apiURL so generateConfig writes it to the bare "kittypaw-api"
	// namespace that InjectKakaoWSURL reads at serve time — without this,
	// users who only complete the Kakao step end up with an unroutable channel.
	_ = s.store.SetUserContext("setup:api_server_url", apiURL, "setup")

	writeJSON(w, http.StatusOK, map[string]any{
		"pair_code":   reg.PairCode,
		"channel_url": reg.ChannelURL,
	})
}

// ---------------------------------------------------------------------------
// GET /api/setup/kakao/pair-status
// ---------------------------------------------------------------------------

func (s *Server) handleSetupKakaoPairStatus(w http.ResponseWriter, _ *http.Request) {
	// Already configured: no further pairing needed.
	s.configMu.RLock()
	for _, ch := range s.config.Channels {
		if ch.ChannelType == core.ChannelKakaoTalk {
			s.configMu.RUnlock()
			writeJSON(w, http.StatusOK, map[string]any{"paired": true})
			return
		}
	}
	s.configMu.RUnlock()

	// Wizard in progress: ask the relay whether the user has completed pairing.
	relayBase, _, _ := s.store.GetUserContext("setup:kakao_relay_base")
	token, _, _ := s.store.GetUserContext("setup:kakao_relay_token")
	if relayBase == "" || token == "" {
		writeJSON(w, http.StatusOK, map[string]any{"paired": false})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"paired": core.CheckRelayPairStatus(relayBase, token),
	})
}

// ---------------------------------------------------------------------------
// POST /api/setup/api-server
// ---------------------------------------------------------------------------

func (s *Server) handleSetupAPIServer(w http.ResponseWriter, r *http.Request) {
	var body struct {
		URL string `json:"url"`
	}
	if !decodeBody(w, r, &body) {
		return
	}
	body.URL = strings.TrimRight(body.URL, "/")
	if body.URL == "" {
		writeError(w, http.StatusBadRequest, "url is required")
		return
	}

	_ = s.store.SetUserContext("setup:api_server_url", body.URL, "setup")
	writeJSON(w, http.StatusOK, map[string]any{"saved": true, "url": body.URL})
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

	_ = s.store.SetUserContext("setup:workspace_path", canonical, "setup")
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

	_ = s.store.SetUserContext("onboarding_completed", "true", "system")

	// Hot-reload the config into the running server.
	accountID := s.defaultAccountID()
	cfgPath, _ := core.ConfigPathForAccount(accountID)
	if cfg, err := core.LoadConfig(cfgPath); err == nil {
		s.configMu.Lock()
		*s.config = *cfg
		s.configMu.Unlock()
		slog.Info("setup: config reloaded after onboarding")

		// Reconcile channels with the generated config. TrySpawn is idempotent,
		// so channels already started by handleSetupTelegram are safely skipped.
		if s.spawner != nil {
			if rErr := s.spawner.Reconcile(accountID, cfg.Channels); rErr != nil {
				slog.Warn("setup: channel reconcile partial failure", "error", rErr)
			}
		}
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

	_ = s.store.SetUserContext("onboarding_completed", "false", "system")
	writeJSON(w, http.StatusOK, map[string]any{"reset": true})
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func (s *Server) isOnboardingCompleted() bool {
	v, ok, _ := s.store.GetUserContext("onboarding_completed")
	if ok && v == "true" {
		return true
	}
	// CLI `kittypaw setup` writes config.toml but doesn't set the DB flag.
	// Treat a configured LLM as onboarding complete.
	s.configMu.RLock()
	configured := s.config.LLM.APIKey != "" || s.config.LLM.BaseURL != ""
	s.configMu.RUnlock()
	return configured
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

// wizardResultFromStore reads setup:* keys from the store into a WizardResult.
func (s *Server) wizardResultFromStore() core.WizardResult {
	var w core.WizardResult
	if v, ok, _ := s.store.GetUserContext("setup:llm_provider"); ok {
		w.LLMProvider = v
	}
	if v, ok, _ := s.store.GetUserContext("setup:llm_api_key"); ok {
		w.LLMAPIKey = v
	}
	if v, ok, _ := s.store.GetUserContext("setup:llm_model"); ok {
		w.LLMModel = v
	}
	if v, ok, _ := s.store.GetUserContext("setup:llm_base_url"); ok {
		w.LLMBaseURL = v
	}
	if v, ok, _ := s.store.GetUserContext("setup:telegram_bot_token"); ok {
		w.TelegramBotToken = v
	}
	if v, ok, _ := s.store.GetUserContext("setup:telegram_chat_id"); ok {
		w.TelegramChatID = v
	}
	if v, ok, _ := s.store.GetUserContext("setup:workspace_path"); ok {
		w.WorkspacePath = v
	}
	if v, ok, _ := s.store.GetUserContext("setup:api_server_url"); ok {
		w.APIServerURL = v
	}
	// Kakao has no toggle field in the web wizard: a successful /setup/kakao/register
	// leaves a relay token in the store. Treat that as the "enabled" signal so
	// MergeWizardSettings includes the channel in the final config.
	if v, ok, _ := s.store.GetUserContext("setup:kakao_relay_token"); ok && v != "" {
		w.KakaoEnabled = true
	}
	return w
}

// generateConfig merges wizard settings into the existing config.toml.
func (s *Server) generateConfig() error {
	accountID := s.defaultAccountID()
	cfgPath, err := core.ConfigPathForAccount(accountID)
	if err != nil {
		return err
	}

	cfg := core.DefaultConfig()
	if data, readErr := os.ReadFile(cfgPath); readErr == nil {
		if err := toml.Unmarshal(data, &cfg); err != nil {
			return fmt.Errorf("existing config.toml has syntax errors: %w", err)
		}
	}

	w := s.wizardResultFromStore()
	merged := core.MergeWizardSettings(&cfg, w)
	if err := core.WriteConfigAtomic(merged, cfgPath); err != nil {
		return err
	}

	// Save API server URL to secrets for package source bindings.
	// Open the per-account store fresh on every call: a long-lived
	// reference would carry stale in-memory state between web setup
	// steps (e.g. /kakao/register followed by /complete) and the
	// second Set's persist would overwrite the first step's writes.
	if w.APIServerURL != "" {
		if secrets, err := core.LoadAccountSecrets(accountID); err == nil {
			_ = secrets.Set("kittypaw-api", "api_url", w.APIServerURL)
		}
	}

	return nil
}
