package protocol

import (
	"encoding/json"
	"fmt"
	"net/http"
)

const MaxIDLength = 128

type FrameType string

const (
	FrameHello           FrameType = "hello"
	FrameRequest         FrameType = "request"
	FrameResponseHeaders FrameType = "response_headers"
	FrameResponseChunk   FrameType = "response_chunk"
	FrameResponseEnd     FrameType = "response_end"
	FrameError           FrameType = "error"
	FramePing            FrameType = "ping"
	FramePong            FrameType = "pong"
)

type Frame struct {
	Type          FrameType         `json:"type"`
	ID            string            `json:"id,omitempty"`
	DeviceID      string            `json:"device_id,omitempty"`
	AccountID     string            `json:"account_id,omitempty"`
	LocalAccounts []string          `json:"local_accounts,omitempty"`
	Version       string            `json:"version,omitempty"`
	Method        string            `json:"method,omitempty"`
	Path          string            `json:"path,omitempty"`
	Status        int               `json:"status,omitempty"`
	Headers       map[string]string `json:"headers,omitempty"`
	Body          json.RawMessage   `json:"body,omitempty"`
	Data          string            `json:"data,omitempty"`
	Code          string            `json:"code,omitempty"`
	Message       string            `json:"message,omitempty"`
}

func (f Frame) Validate() error {
	if f.Type == "" {
		return fmt.Errorf("type is required")
	}
	if len(f.ID) > MaxIDLength {
		return fmt.Errorf("id exceeds maximum length")
	}

	switch f.Type {
	case FrameHello:
		if f.DeviceID == "" {
			return fmt.Errorf("device_id is required")
		}
		if len(f.LocalAccounts) == 0 {
			return fmt.Errorf("at least one local account is required")
		}
		for _, accountID := range f.LocalAccounts {
			if accountID == "" {
				return fmt.Errorf("local account id is required")
			}
		}
	case FrameRequest:
		if f.ID == "" {
			return fmt.Errorf("id is required")
		}
		if f.AccountID == "" {
			return fmt.Errorf("account_id is required")
		}
		if f.Method != http.MethodGet && f.Method != http.MethodPost {
			return fmt.Errorf("method is not relayable")
		}
		if !AllowedRelayPath(f.Path) {
			return fmt.Errorf("path is not relayable")
		}
	case FrameResponseHeaders:
		if f.ID == "" {
			return fmt.Errorf("id is required")
		}
		if f.Status < 100 || f.Status > 599 {
			return fmt.Errorf("status is invalid")
		}
	case FrameResponseChunk, FrameResponseEnd, FrameError:
		if f.ID == "" {
			return fmt.Errorf("id is required")
		}
	case FramePing, FramePong:
	default:
		return fmt.Errorf("unknown frame type %q", f.Type)
	}

	return nil
}

func AllowedRelayPath(path string) bool {
	switch path {
	case "/health", "/v1/models", "/v1/chat/completions":
		return true
	default:
		return false
	}
}
