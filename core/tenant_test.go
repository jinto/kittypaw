package core

import (
	"strings"
	"testing"
)

// TestChatBelongsToTenant_StrictMatch pins the ownership check: when a
// tenant has AdminChatIDs configured, only chat_ids in that list are
// accepted. A mismatch is the AC-T7 attack surface — a bot-token leak
// must not let a foreign chat_id leak into this tenant's session state.
func TestChatBelongsToTenant_StrictMatch(t *testing.T) {
	cfg := &Config{AdminChatIDs: []string{"111", "222"}}
	if !ChatBelongsToTenant(cfg, "111") {
		t.Error("chat_id 111 should match AdminChatIDs")
	}
	if !ChatBelongsToTenant(cfg, "222") {
		t.Error("chat_id 222 should match AdminChatIDs")
	}
	if ChatBelongsToTenant(cfg, "999") {
		t.Error("chat_id 999 must NOT match; AC-T7 violation")
	}
	if ChatBelongsToTenant(cfg, "") {
		t.Error("empty chat_id must NOT match a configured tenant")
	}
}

// TestChatBelongsToTenant_PermissiveUnconfigured locks in the back-compat
// path: a tenant with no AdminChatIDs (fresh install, WebChat-only tenant)
// accepts every chat_id. Without this the migration would silently drop
// every inbound message on existing installs.
func TestChatBelongsToTenant_PermissiveUnconfigured(t *testing.T) {
	cases := []*Config{
		nil,
		{AdminChatIDs: nil},
		{AdminChatIDs: []string{}},
	}
	for i, cfg := range cases {
		if !ChatBelongsToTenant(cfg, "anything") {
			t.Errorf("case %d: unconfigured tenant must be permissive", i)
		}
	}
}

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

// TestValidateTenantChannels_TelegramDuplicate locks in that two tenants
// declaring the same Telegram bot token surface as a startup error rather
// than silently racing on getUpdates.
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

// TestValidateTenantChannels_KakaoDuplicate locks in the same rule for Kakao
// relay pairings — identical WS URL across tenants would dual-bind a single
// Kakao account.
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

// TestValidateFamilyTenants_RejectsChannels locks in the rule that a tenant
// marked IsFamily cannot own a chat channel. If the family bot kept a
// [telegram] block, it would swallow updates meant for whichever personal
// tenant shares the real bot_token, producing a silent delivery blackhole.
// Fail-fast at startup.
func TestValidateFamilyTenants_RejectsChannels(t *testing.T) {
	tenants := []*Tenant{
		{ID: "alice", Config: &Config{}},
		{ID: "family", Config: &Config{
			IsFamily: true,
			Channels: []ChannelConfig{{ChannelType: ChannelTelegram, Token: "x"}},
		}},
	}
	err := ValidateFamilyTenants(tenants)
	if err == nil {
		t.Fatal("expected family-with-channels to error")
	}
	if !strings.Contains(err.Error(), "family") || !strings.Contains(err.Error(), "telegram") {
		t.Errorf("error should cite tenant id and channel type: %q", err.Error())
	}
}

// TestValidateFamilyTenants_PersonalWithChannelsOK confirms the check is
// scoped to the family flag — personal tenants declaring channels are
// the normal case and must pass.
func TestValidateFamilyTenants_PersonalWithChannelsOK(t *testing.T) {
	tenants := []*Tenant{
		{ID: "alice", Config: &Config{
			Channels: []ChannelConfig{{ChannelType: ChannelTelegram, Token: "x"}},
		}},
		{ID: "family", Config: &Config{IsFamily: true}},
	}
	if err := ValidateFamilyTenants(tenants); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

// TestValidateFamilyTenants_NilConfigSkipped guards against a half-loaded
// tenant (Config == nil) panicking the startup path. Better to skip than
// to crash when a config file failed to parse earlier.
func TestValidateFamilyTenants_NilConfigSkipped(t *testing.T) {
	tenants := []*Tenant{{ID: "ghost", Config: nil}}
	if err := ValidateFamilyTenants(tenants); err != nil {
		t.Errorf("nil config should be skipped, got %v", err)
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
