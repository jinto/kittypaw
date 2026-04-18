package core

import (
	"strings"
	"testing"
)

// TestValidateTenantChannels_NoDuplicates confirms the happy path —
// distinct tokens across tenants return nil.
func TestValidateTenantChannels_NoDuplicates(t *testing.T) {
	tc := map[string][]ChannelConfig{
		"alice": {{ChannelType: ChannelTelegram, Token: "alice-token"}},
		"bob":   {{ChannelType: ChannelTelegram, Token: "bob-token"}},
	}
	if err := ValidateTenantChannels(tc); err != nil {
		t.Errorf("unexpected error for distinct tokens: %v", err)
	}
}

// TestValidateTenantChannels_TelegramDuplicate enforces C3: two tenants
// declaring the same Telegram bot token must surface as a startup error,
// not silently race on getUpdates.
func TestValidateTenantChannels_TelegramDuplicate(t *testing.T) {
	tc := map[string][]ChannelConfig{
		"alice": {{ChannelType: ChannelTelegram, Token: "shared"}},
		"bob":   {{ChannelType: ChannelTelegram, Token: "shared"}},
	}
	err := ValidateTenantChannels(tc)
	if err == nil {
		t.Fatal("expected duplicate bot_token error, got nil")
	}
	msg := err.Error()
	if !strings.Contains(msg, "telegram bot_token") {
		t.Errorf("error should mention telegram bot_token: %q", msg)
	}
	if !strings.Contains(msg, "alice") || !strings.Contains(msg, "bob") {
		t.Errorf("error should name both tenants: %q", msg)
	}
}

// TestValidateTenantChannels_KakaoDuplicate enforces C3 for Kakao relay
// pairings — the same WS URL across tenants would dual-bind one Kakao
// account.
func TestValidateTenantChannels_KakaoDuplicate(t *testing.T) {
	tc := map[string][]ChannelConfig{
		"alice":  {{ChannelType: ChannelKakaoTalk, KakaoWSURL: "wss://relay/ws/shared"}},
		"family": {{ChannelType: ChannelKakaoTalk, KakaoWSURL: "wss://relay/ws/shared"}},
	}
	err := ValidateTenantChannels(tc)
	if err == nil {
		t.Fatal("expected duplicate kakao URL error, got nil")
	}
	if !strings.Contains(err.Error(), "kakao relay URL") {
		t.Errorf("error should mention kakao relay URL: %q", err.Error())
	}
}

// TestValidateTenantChannels_EmptyTokensIgnored ensures that tenants with
// unset/empty tokens do not falsely collide (multiple tenants may legitimately
// have "" during half-completed setup).
func TestValidateTenantChannels_EmptyTokensIgnored(t *testing.T) {
	tc := map[string][]ChannelConfig{
		"alice": {{ChannelType: ChannelTelegram, Token: ""}},
		"bob":   {{ChannelType: ChannelTelegram, Token: ""}},
	}
	if err := ValidateTenantChannels(tc); err != nil {
		t.Errorf("empty tokens should not collide: %v", err)
	}
}

// TestValidateTenantChannels_CrossChannelOK ensures the check only scopes
// within a single channel type — a Telegram token equal to a random string
// used elsewhere should not collide with Kakao URLs, etc.
func TestValidateTenantChannels_CrossChannelOK(t *testing.T) {
	tc := map[string][]ChannelConfig{
		"alice": {{ChannelType: ChannelTelegram, Token: "value"}},
		"bob":   {{ChannelType: ChannelKakaoTalk, KakaoWSURL: "value"}},
	}
	if err := ValidateTenantChannels(tc); err != nil {
		t.Errorf("cross-channel value reuse should not collide: %v", err)
	}
}
