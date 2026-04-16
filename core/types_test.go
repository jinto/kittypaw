package core

import (
	"encoding/json"
	"testing"
)

func TestValidateSkillName(t *testing.T) {
	tests := []struct {
		name    string
		wantErr bool
	}{
		{"my-skill", false},
		{"my_skill", false},
		{"Skill123", false},
		{"a", false},
		{"", true},              // empty
		{"..", true},            // path traversal
		{"../etc/passwd", true}, // path traversal
		{"skill/../bad", true},  // embedded traversal
		{"skill/bad", true},     // slash
		{"skill\\bad", true},    // backslash
		{"skill name", true},    // space
		{"skill!name", true},    // special char
		{"skill@name", true},    // at sign
		{"good-skill-name", false},
		{"x-y_z-123", false},
	}
	for _, tt := range tests {
		err := ValidateSkillName(tt.name)
		if (err != nil) != tt.wantErr {
			t.Errorf("ValidateSkillName(%q) error = %v, wantErr %v", tt.name, err, tt.wantErr)
		}
	}
}

func TestIsSecretEnvVar(t *testing.T) {
	tests := []struct {
		name string
		want bool
	}{
		{"API_KEY", true},
		{"SECRET_VALUE", true},
		{"AUTH_TOKEN", true},
		{"DB_PASSWORD", true},
		{"AWS_CREDENTIAL", true},
		{"HOME", false},
		{"PATH", false},
		{"LANG", false},
		{"GOPAW_PORT", false},
		{"my_secret", true}, // lowercase "secret"
		{"tokenizer", true}, // contains "token"
		{"AUTHOR", true},    // contains "AUTH"
	}
	for _, tt := range tests {
		got := IsSecretEnvVar(tt.name)
		if got != tt.want {
			t.Errorf("IsSecretEnvVar(%q) = %v, want %v", tt.name, got, tt.want)
		}
	}
}

func TestIsPrivateIP(t *testing.T) {
	tests := []struct {
		host string
		want bool
	}{
		{"localhost", true},
		{"127.0.0.1", true},
		{"127.0.0.2", true},
		{"::1", true},
		{"10.0.0.1", true},
		{"10.255.255.255", true},
		{"172.16.0.1", true},
		{"172.31.255.255", true},
		{"192.168.0.1", true},
		{"192.168.1.100", true},
		{"169.254.1.1", true},
		{"0.0.0.0", true},
		// Public addresses
		{"8.8.8.8", false},
		{"1.1.1.1", false},
		{"203.0.113.1", false},
		{"example.com", false},
		{"172.32.0.1", false}, // just outside 172.16-31 range
		{"11.0.0.1", false},
	}
	for _, tt := range tests {
		got := IsPrivateIP(tt.host)
		if got != tt.want {
			t.Errorf("IsPrivateIP(%q) = %v, want %v", tt.host, got, tt.want)
		}
	}
}

func TestSplitChunks(t *testing.T) {
	tests := []struct {
		text   string
		maxLen int
		want   int // expected number of chunks
	}{
		{"short", 100, 1},
		{"", 10, 1},
		{"hello\nworld\nfoo\nbar", 11, 2},
		// Force hard split when no newline found in first half
		{"abcdefghij", 5, 2},
	}
	for _, tt := range tests {
		chunks := SplitChunks(tt.text, tt.maxLen)
		if len(chunks) != tt.want {
			t.Errorf("SplitChunks(%q, %d) = %d chunks, want %d", tt.text, tt.maxLen, len(chunks), tt.want)
		}
		// Verify all chunks are within maxLen
		for i, c := range chunks {
			if len(c) > tt.maxLen {
				t.Errorf("SplitChunks chunk %d len %d > maxLen %d", i, len(c), tt.maxLen)
			}
		}
		// Verify reassembly
		reassembled := ""
		for _, c := range chunks {
			reassembled += c
		}
		if reassembled != tt.text {
			t.Errorf("SplitChunks reassembly mismatch: got %q, want %q", reassembled, tt.text)
		}
	}
}

func TestParsePayload(t *testing.T) {
	payload := ChatPayload{
		ChatID:    "chat123",
		Text:      "hello",
		FromName:  "alice",
		SessionID: "sess1",
	}
	raw, _ := json.Marshal(payload)
	event := &Event{Type: EventWebChat, Payload: raw}

	got, err := event.ParsePayload()
	if err != nil {
		t.Fatalf("ParsePayload() error: %v", err)
	}
	if got.ChatID != "chat123" || got.Text != "hello" || got.FromName != "alice" {
		t.Errorf("ParsePayload() = %+v, want matching fields", got)
	}
}

func TestParsePayloadInvalid(t *testing.T) {
	event := &Event{Type: EventWebChat, Payload: json.RawMessage(`{invalid`)}
	_, err := event.ParsePayload()
	if err == nil {
		t.Error("ParsePayload() expected error for invalid JSON")
	}
}

func TestChannelTypeToEventType(t *testing.T) {
	tests := []struct {
		ct   ChannelType
		want EventType
	}{
		{ChannelTelegram, EventTelegram},
		{ChannelSlack, EventSlack},
		{ChannelDiscord, EventDiscord},
		{ChannelWeb, EventWebChat},
		{ChannelDesktop, EventDesktop},
		{ChannelKakaoTalk, EventKakaoTalk},
	}
	for _, tt := range tests {
		got := tt.ct.ToEventType()
		if got != tt.want {
			t.Errorf("ChannelType(%q).ToEventType() = %q, want %q", tt.ct, got, tt.want)
		}
	}
}

func TestSkillRegistryCompleteness(t *testing.T) {
	if len(SkillRegistry) == 0 {
		t.Fatal("SkillRegistry is empty")
	}
	seen := make(map[string]bool)
	for _, skill := range SkillRegistry {
		if skill.Name == "" {
			t.Error("SkillRegistry contains entry with empty name")
		}
		if seen[skill.Name] {
			t.Errorf("SkillRegistry has duplicate: %s", skill.Name)
		}
		seen[skill.Name] = true
		if len(skill.Methods) == 0 {
			t.Errorf("SkillRegistry[%s] has no methods", skill.Name)
		}
		for _, m := range skill.Methods {
			if m.Name == "" {
				t.Errorf("SkillRegistry[%s] has method with empty name", skill.Name)
			}
			if m.Signature == "" {
				t.Errorf("SkillRegistry[%s].%s has empty signature", skill.Name, m.Name)
			}
		}
	}
}
