package protocol

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestFrameRoundTripRequest(t *testing.T) {
	body := json.RawMessage(`{"model":"kittypaw","stream":true}`)
	in := Frame{
		Type:      FrameRequest,
		ID:        "req_123",
		AccountID: "alice",
		Method:    "POST",
		Path:      "/v1/chat/completions",
		Body:      body,
	}

	raw, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var out Frame
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if out.Type != FrameRequest || out.ID != "req_123" || out.AccountID != "alice" {
		t.Fatalf("round trip frame = %+v", out)
	}
	if string(out.Body) != string(body) {
		t.Fatalf("body = %s, want %s", out.Body, body)
	}
}

func TestValidateHelloRequiresDeviceAndAccount(t *testing.T) {
	tests := []struct {
		name  string
		frame Frame
		want  string
	}{
		{
			name:  "missing device",
			frame: Frame{Type: FrameHello, LocalAccounts: []string{"alice"}},
			want:  "device_id is required",
		},
		{
			name:  "missing account",
			frame: Frame{Type: FrameHello, DeviceID: "dev_123"},
			want:  "at least one local account is required",
		},
		{
			name:  "valid hello",
			frame: Frame{Type: FrameHello, DeviceID: "dev_123", LocalAccounts: []string{"alice"}},
			want:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.frame.Validate()
			if tt.want == "" {
				if err != nil {
					t.Fatalf("Validate() error = %v, want nil", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Validate() error = %v, want containing %q", err, tt.want)
			}
		})
	}
}

func TestValidateRequestRestrictsPaths(t *testing.T) {
	allowed := []string{"/health", "/v1/models", "/v1/chat/completions"}
	for _, path := range allowed {
		frame := Frame{
			Type:      FrameRequest,
			ID:        "req_allowed",
			AccountID: "alice",
			Method:    "GET",
			Path:      path,
		}
		if err := frame.Validate(); err != nil {
			t.Fatalf("Validate(%s) error = %v, want nil", path, err)
		}
	}

	frame := Frame{
		Type:      FrameRequest,
		ID:        "req_forbidden",
		AccountID: "alice",
		Method:    "GET",
		Path:      "/api/v1/skills",
	}
	if err := frame.Validate(); err == nil || !strings.Contains(err.Error(), "path is not relayable") {
		t.Fatalf("Validate() error = %v, want forbidden path error", err)
	}
}

func TestValidateFrameRejectsOversizedID(t *testing.T) {
	frame := Frame{
		Type:      FrameRequest,
		ID:        strings.Repeat("x", MaxIDLength+1),
		AccountID: "alice",
		Method:    "GET",
		Path:      "/health",
	}
	if err := frame.Validate(); err == nil || !strings.Contains(err.Error(), "id exceeds maximum length") {
		t.Fatalf("Validate() error = %v, want oversized id error", err)
	}
}
