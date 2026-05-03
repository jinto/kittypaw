package server

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/jinto/kittypaw/core"
)

var fetchTelegramChatID = core.FetchTelegramChatID

type telegramPairingResult struct {
	Status  string `json:"status"`
	ChatID  string `json:"chat_id,omitempty"`
	Source  string `json:"source,omitempty"`
	Message string `json:"message,omitempty"`
}

func (s *Server) handleTelegramPairingChatID(w http.ResponseWriter, r *http.Request) {
	var body struct {
		AccountID string `json:"account_id"`
		Token     string `json:"token"`
	}
	if !decodeBody(w, r, &body) {
		return
	}

	requestedAccountID := strings.TrimSpace(body.AccountID)
	token := strings.TrimSpace(body.Token)
	if token == "" {
		writeError(w, http.StatusBadRequest, "token is required")
		return
	}
	if !core.ValidateTelegramToken(token) {
		writeError(w, http.StatusBadRequest, "invalid bot token format")
		return
	}

	if requestAuthToken(r) == "" && isLocalhost(r) {
		result, ok := s.localTelegramPairingChatIDByToken(token)
		if !ok {
			writeError(w, http.StatusNotFound, "no running Telegram channel is using this bot token")
			return
		}
		writeJSON(w, http.StatusOK, result)
		return
	}

	accountID, status, err := s.telegramPairingRequestAccountID(r, requestedAccountID)
	if err != nil {
		writeError(w, status, err.Error())
		return
	}

	result, err := s.telegramPairingChatID(r.Context(), accountID, token)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) telegramPairingRequestAccountID(r *http.Request, requestedAccountID string) (string, int, error) {
	if requestedAccountID != "" {
		if err := core.ValidateAccountID(requestedAccountID); err != nil {
			return "", http.StatusBadRequest, err
		}
	}

	if acct, err := s.requestAccount(r); err == nil && acct != nil {
		if requestedAccountID == "" || requestedAccountID == acct.ID {
			return acct.ID, http.StatusOK, nil
		}
		if token := requestAuthToken(r); token != "" && s.effectiveAPIKey() != "" && fixedLenEqual(token, s.effectiveAPIKey()) {
			return requestedAccountID, http.StatusOK, nil
		}
		return "", http.StatusForbidden, fmt.Errorf("account %q is not authorized for this request", requestedAccountID)
	}

	if requestedAccountID != "" {
		if token := requestAuthToken(r); token != "" && s.effectiveAPIKey() != "" && fixedLenEqual(token, s.effectiveAPIKey()) {
			return requestedAccountID, http.StatusOK, nil
		}
	}

	return "", http.StatusUnauthorized, fmt.Errorf("unauthorized")
}

func (s *Server) telegramPairingChatID(ctx context.Context, accountID, token string) (telegramPairingResult, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return telegramPairingResult{}, fmt.Errorf("token is required")
	}

	if chatID, ok := s.configuredTelegramChatID(accountID, token); ok {
		return telegramPairingResult{Status: "paired", ChatID: chatID, Source: "config"}, nil
	}

	if s.spawner != nil {
		if chatID, ok, active := s.spawner.TelegramLastChatID(accountID, token); active {
			if ok {
				return telegramPairingResult{Status: "paired", ChatID: chatID, Source: "active_channel"}, nil
			}
			return telegramPairingResult{
				Status:  "waiting",
				Source:  "active_channel",
				Message: "waiting for a Telegram message received by the running local server",
			}, nil
		}
	}

	chatID, err := fetchTelegramChatID(ctx, token)
	if err != nil {
		msg := err.Error()
		if strings.Contains(msg, "no messages found") ||
			strings.Contains(msg, "terminated by other getUpdates request") {
			return telegramPairingResult{Status: "waiting", Source: "telegram_api", Message: msg}, nil
		}
		return telegramPairingResult{}, fmt.Errorf("failed to fetch chat ID: %w", err)
	}
	return telegramPairingResult{Status: "paired", ChatID: chatID, Source: "telegram_api"}, nil
}

func (s *Server) localTelegramPairingChatIDByToken(token string) (telegramPairingResult, bool) {
	if s.spawner == nil {
		return telegramPairingResult{}, false
	}
	_, chatID, found, active := s.spawner.TelegramLastChatIDByToken(token)
	if !active {
		return telegramPairingResult{}, false
	}
	if found {
		return telegramPairingResult{Status: "paired", ChatID: chatID, Source: "active_channel"}, true
	}
	return telegramPairingResult{
		Status:  "waiting",
		Source:  "active_channel",
		Message: "waiting for a Telegram message received by the running local server",
	}, true
}

func (s *Server) configuredTelegramChatID(accountID, token string) (string, bool) {
	deps := s.accountDepsForID(accountID)
	if deps == nil || deps.Account == nil || deps.Account.Config == nil {
		return "", false
	}
	cfg := deps.Account.Config
	for _, ch := range cfg.Channels {
		if ch.ChannelType != core.ChannelTelegram || strings.TrimSpace(ch.Token) != token {
			continue
		}
		if id := firstNonEmpty(ch.AllowedChatIDs...); id != "" {
			return id, true
		}
		if id := firstNonEmpty(cfg.AllowedChatIDs...); id != "" {
			return id, true
		}
		return "", false
	}
	return "", false
}
