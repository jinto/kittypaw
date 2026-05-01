package openai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/kittypaw-app/kittyrelay/internal/broker"
	"github.com/kittypaw-app/kittyrelay/internal/protocol"
)

const maxRequestBodyBytes = 1 << 20

var ErrUnauthorized = errors.New("unauthorized")

type Principal struct {
	UserID    string
	DeviceID  string
	AccountID string
}

type Authenticator interface {
	Authenticate(r *http.Request) (Principal, error)
}

type Broker interface {
	Request(ctx context.Context, req broker.Request) (<-chan protocol.Frame, error)
}

type Handler struct {
	auth   Authenticator
	broker Broker
}

func NewHandler(auth Authenticator, b Broker) *Handler {
	return &Handler{auth: auth, broker: b}
}

func (h *Handler) Routes() http.Handler {
	r := chi.NewRouter()
	r.Get("/nodes/{device_id}/v1/models", h.handleModels)
	r.Post("/nodes/{device_id}/v1/chat/completions", h.handleChatCompletions)
	return r
}

func (h *Handler) handleModels(w http.ResponseWriter, r *http.Request) {
	h.relay(w, r, http.MethodGet, "/v1/models", nil)
}

func (h *Handler) handleChatCompletions(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxRequestBodyBytes))
	if err != nil {
		writeJSONError(w, http.StatusRequestEntityTooLarge, "request body too large")
		return
	}
	h.relay(w, r, http.MethodPost, "/v1/chat/completions", body)
}

func (h *Handler) relay(w http.ResponseWriter, r *http.Request, method, path string, body []byte) {
	if h.auth == nil {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	principal, err := h.auth.Authenticate(r)
	if err != nil {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	deviceID := chi.URLParam(r, "device_id")
	if principal.DeviceID != deviceID {
		writeJSONError(w, http.StatusForbidden, "forbidden")
		return
	}
	if h.broker == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "relay broker unavailable")
		return
	}

	stream, err := h.broker.Request(r.Context(), broker.Request{
		UserID:    principal.UserID,
		DeviceID:  deviceID,
		AccountID: principal.AccountID,
		Method:    method,
		Path:      path,
		Body:      body,
	})
	if err != nil {
		switch {
		case errors.Is(err, broker.ErrDeviceOffline):
			writeJSONError(w, http.StatusServiceUnavailable, "device offline")
		case errors.Is(err, broker.ErrForbidden):
			writeJSONError(w, http.StatusForbidden, "forbidden")
		case errors.Is(err, broker.ErrBackpressure):
			writeJSONError(w, http.StatusTooManyRequests, "too many in-flight requests")
		default:
			writeJSONError(w, http.StatusBadGateway, err.Error())
		}
		return
	}

	h.writeRelayStream(w, stream)
}

func (h *Handler) writeRelayStream(w http.ResponseWriter, stream <-chan protocol.Frame) {
	headerWritten := false
	writeHeaders := func(status int, headers map[string]string) {
		if headerWritten {
			return
		}
		if status == 0 {
			status = http.StatusOK
		}
		for key, value := range headers {
			if strings.EqualFold(key, "content-type") {
				w.Header().Set("Content-Type", value)
				continue
			}
			w.Header().Set(key, value)
		}
		if w.Header().Get("Content-Type") == "" {
			w.Header().Set("Content-Type", "application/json")
		}
		w.WriteHeader(status)
		headerWritten = true
	}

	for frame := range stream {
		switch frame.Type {
		case protocol.FrameResponseHeaders:
			writeHeaders(frame.Status, frame.Headers)
		case protocol.FrameResponseChunk:
			writeHeaders(http.StatusOK, nil)
			if _, err := io.WriteString(w, frame.Data); err != nil {
				return
			}
			if flusher, ok := w.(http.Flusher); ok {
				flusher.Flush()
			}
		case protocol.FrameResponseEnd:
			writeHeaders(http.StatusOK, nil)
			return
		case protocol.FrameError:
			if !headerWritten {
				writeJSONError(w, http.StatusBadGateway, frame.Message)
			}
			return
		}
	}
	if !headerWritten {
		writeJSONError(w, http.StatusBadGateway, "relay stream ended without response")
	}
}

type StaticTokenAuthenticator struct {
	Token     string
	Principal Principal
}

func (a StaticTokenAuthenticator) Authenticate(r *http.Request) (Principal, error) {
	token := bearerToken(r)
	if token == "" || a.Token == "" || token != a.Token {
		return Principal{}, ErrUnauthorized
	}
	return a.Principal, nil
}

func bearerToken(r *http.Request) string {
	auth := r.Header.Get("Authorization")
	if strings.HasPrefix(auth, "Bearer ") {
		return strings.TrimPrefix(auth, "Bearer ")
	}
	if key := r.Header.Get("x-api-key"); key != "" {
		return key
	}
	return ""
}

func writeJSONError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": message})
}

func (p Principal) Validate() error {
	if p.UserID == "" {
		return fmt.Errorf("user_id is required")
	}
	if p.DeviceID == "" {
		return fmt.Errorf("device_id is required")
	}
	if p.AccountID == "" {
		return fmt.Errorf("account_id is required")
	}
	return nil
}
