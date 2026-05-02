package server

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"log/slog"
	"math/big"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"
	"github.com/kittypaw-app/kittykakao/internal/relay"
)

func NewRouter(state *State) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.Recoverer)

	r.Post("/register", state.handleRegister)
	r.Get("/pair-status/{token}", state.handlePairStatus)
	r.Post("/webhook", state.handleWebhook)
	r.Get("/ws/{token}", state.handleWS)
	r.Post("/admin/killswitch", state.handleAdminKillswitch)
	r.Get("/admin/stats", state.handleAdminStats)
	r.Get("/health", state.handleHealth)
	return r
}

func (s *State) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{
		"status":  "healthy",
		"version": s.Version,
		"commit":  s.Commit,
	})
}

func (s *State) handleRegister(w http.ResponseWriter, r *http.Request) {
	token := strings.ReplaceAll(uuid.NewString(), "-", "")
	pairCode, err := newPairCode()
	if err != nil {
		slog.Warn("generate pair code failed", "err", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	if err := s.Store.PutToken(r.Context(), token); err != nil {
		slog.Warn("put token failed", "err", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	s.pairCodes.Set(pairCode, token)
	writeJSON(w, http.StatusOK, relay.RegisterResponse{
		Token:      token,
		PairCode:   pairCode,
		ChannelURL: s.Config.ChannelURL,
	})
}

func (s *State) handlePairStatus(w http.ResponseWriter, r *http.Request) {
	token := chi.URLParam(r, "token")
	paired, _ := s.pairedMarkers.Get(token)
	writeJSON(w, http.StatusOK, relay.PairStatusResponse{Paired: paired})
}

func (s *State) handleWebhook(w http.ResponseWriter, r *http.Request) {
	if !s.checkSecret(r.URL.Query().Get("secret")) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var payload relay.KakaoPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	actionID := payload.Action.ID
	utterance := payload.UserRequest.Utterance
	userID := payload.UserRequest.User.ID
	if actionID == "" || utterance == "" || userID == "" {
		http.Error(w, "missing required fields", http.StatusBadRequest)
		return
	}

	killswitch, err := s.Store.GetKillswitch(r.Context())
	if err != nil {
		slog.Warn("get killswitch failed", "err", err)
		writeJSON(w, http.StatusOK, relay.Text(relay.MsgTransientError))
		return
	}
	if killswitch {
		http.Error(w, "Service temporarily suspended", http.StatusServiceUnavailable)
		return
	}

	limit, err := s.Store.CheckRateLimit(r.Context(), s.Config.DailyLimit, s.Config.MonthlyLimit)
	if err != nil {
		slog.Warn("rate limit check failed", "err", err)
		writeJSON(w, http.StatusOK, relay.Text(relay.MsgTransientError))
		return
	}
	if !limit.OK {
		writeJSON(w, http.StatusOK, relay.Text(relay.MsgRateLimited))
		return
	}

	if isSixDigitCode(utterance) {
		s.handlePairing(w, r, utterance, userID)
		return
	}

	if payload.UserRequest.CallbackURL == nil || *payload.UserRequest.CallbackURL == "" {
		writeJSON(w, http.StatusOK, relay.Text(relay.MsgNoCallback))
		return
	}
	callbackURL := *payload.UserRequest.CallbackURL

	relayToken, ok, err := s.Store.GetUserMapping(r.Context(), userID)
	if err != nil {
		slog.Warn("user mapping lookup failed", "err", err)
		writeJSON(w, http.StatusOK, relay.Text(relay.MsgTransientError))
		return
	}
	if !ok {
		writeJSON(w, http.StatusOK, relay.Text(relay.MsgNotPaired))
		return
	}

	session, ok := s.getSession(relayToken)
	if !ok {
		writeJSON(w, http.StatusOK, relay.Text(relay.MsgOffline))
		return
	}

	if !IsAllowedCallbackHost(callbackURL) {
		slog.Warn("callback URL blocked by SSRF guard", "url", callbackURL)
		writeJSON(w, http.StatusOK, relay.Text(relay.MsgTransientError))
		return
	}

	if err := s.Store.PutPending(r.Context(), actionID, relay.PendingContext{
		CallbackURL: callbackURL,
		UserID:      userID,
		CreatedAt:   time.Now().Unix(),
	}); err != nil {
		slog.Warn("put pending callback failed", "err", err)
		writeJSON(w, http.StatusOK, relay.Text(relay.MsgTransientError))
		return
	}

	if err := session.Send(relay.WSOutgoing{ID: actionID, Text: utterance, UserID: userID}); err != nil {
		slog.Warn("websocket send failed", "token", relayToken, "err", err)
		writeJSON(w, http.StatusOK, relay.Text(relay.MsgOffline))
		return
	}
	writeJSON(w, http.StatusOK, relay.AsyncAck())
}

func (s *State) handlePairing(w http.ResponseWriter, r *http.Request, pairCode, kakaoUserID string) {
	token, ok := s.pairCodes.Get(pairCode)
	if !ok {
		writeJSON(w, http.StatusOK, relay.Text(relay.MsgInvalidPairCode))
		return
	}
	s.pairCodes.Delete(pairCode)
	if err := s.Store.PutUserMapping(r.Context(), kakaoUserID, token); err != nil {
		slog.Warn("put user mapping failed", "err", err)
		writeJSON(w, http.StatusOK, relay.Text(relay.MsgTransientError))
		return
	}
	s.pairedMarkers.Set(token, true)
	writeJSON(w, http.StatusOK, relay.Text(relay.MsgPaired))
}

func (s *State) handleAdminKillswitch(w http.ResponseWriter, r *http.Request) {
	if !s.checkSecret(r.URL.Query().Get("secret")) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	var body struct {
		Enabled bool `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	if err := s.Store.SetKillswitch(r.Context(), body.Enabled); err != nil {
		slog.Warn("set killswitch failed", "err", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, relay.KillswitchResponse{Killswitch: body.Enabled})
}

func (s *State) handleAdminStats(w http.ResponseWriter, r *http.Request) {
	if !s.checkSecret(r.URL.Query().Get("secret")) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	stats, err := s.Store.GetStats(r.Context())
	if err != nil {
		slog.Warn("get stats failed", "err", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	killswitch, err := s.Store.GetKillswitch(r.Context())
	if err != nil {
		slog.Warn("get killswitch failed", "err", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, relay.AdminStatsResponse{
		Daily: relay.LimitInfo{
			Current: stats.Daily,
			Limit:   s.Config.DailyLimit,
		},
		Monthly: relay.LimitInfo{
			Current: stats.Monthly,
			Limit:   s.Config.MonthlyLimit,
		},
		Killswitch: killswitch,
		WSSessions: s.sessionCount(),
		RSSBytes:   getRSSBytes(),
		FDCount:    getFDCount(),
	})
}

func (s *State) checkSecret(got string) bool {
	return s.Config.WebhookSecret != "" && got == s.Config.WebhookSecret
}

func IsAllowedCallbackHost(raw string) bool {
	parsed, err := url.Parse(raw)
	if err != nil {
		return false
	}
	if parsed.Scheme != "https" {
		return false
	}
	host := strings.ToLower(parsed.Hostname())
	if host == "" {
		return false
	}
	return host == "kakao.com" ||
		strings.HasSuffix(host, ".kakao.com") ||
		host == "kakaoenterprise.com" ||
		strings.HasSuffix(host, ".kakaoenterprise.com")
}

func isPublicHTTPSImageURL(raw string) bool {
	parsed, err := url.Parse(raw)
	return err == nil && parsed.Scheme == "https" && parsed.Hostname() != ""
}

func isSixDigitCode(text string) bool {
	if len(text) != 6 {
		return false
	}
	for _, r := range text {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func newPairCode() (string, error) {
	n, err := rand.Int(rand.Reader, big.NewInt(900_000))
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%06d", n.Int64()+100_000), nil
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
