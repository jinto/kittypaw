package core

import (
	"strings"
	"testing"
)

// TestChatBelongsToAccount_StrictMatch pins the ownership check: when a
// account has AllowedChatIDs configured, only chat_ids in that list are
// accepted. A mismatch is the AC-T7 attack surface — a bot-token leak
// must not let a foreign chat_id leak into this account's session state.
func TestChatBelongsToAccount_StrictMatch(t *testing.T) {
	cfg := &Config{AllowedChatIDs: []string{"111", "222"}}
	if !ChatBelongsToAccount(cfg, "111") {
		t.Error("chat_id 111 should match AllowedChatIDs")
	}
	if !ChatBelongsToAccount(cfg, "222") {
		t.Error("chat_id 222 should match AllowedChatIDs")
	}
	if ChatBelongsToAccount(cfg, "999") {
		t.Error("chat_id 999 must NOT match; AC-T7 violation")
	}
	if ChatBelongsToAccount(cfg, "") {
		t.Error("empty chat_id must NOT match a configured account")
	}
}

// TestChatBelongsToAccount_PermissiveUnconfigured locks in the back-compat
// path: an account with no AllowedChatIDs (fresh install, WebChat-only account)
// accepts every chat_id. Without this the migration would silently drop
// every inbound message on existing installs.
func TestChatBelongsToAccount_PermissiveUnconfigured(t *testing.T) {
	cases := []*Config{
		nil,
		{AllowedChatIDs: nil},
		{AllowedChatIDs: []string{}},
	}
	for i, cfg := range cases {
		if !ChatBelongsToAccount(cfg, "anything") {
			t.Errorf("case %d: unconfigured account must be permissive", i)
		}
	}
}

// TestValidateAccountChannels_NoDuplicates confirms the happy path —
// distinct tokens across accounts return nil.
func TestValidateAccountChannels_NoDuplicates(t *testing.T) {
	tc := map[string][]ChannelConfig{
		"alice": {{ChannelType: ChannelTelegram, Token: "alice-token"}},
		"bob":   {{ChannelType: ChannelTelegram, Token: "bob-token"}},
	}
	if err := ValidateAccountChannels(tc); err != nil {
		t.Errorf("unexpected error for distinct tokens: %v", err)
	}
}

// TestValidateAccountChannels_TelegramDuplicate locks in that two accounts
// declaring the same Telegram bot token surface as a startup error rather
// than silently racing on getUpdates.
func TestValidateAccountChannels_TelegramDuplicate(t *testing.T) {
	tc := map[string][]ChannelConfig{
		"alice": {{ChannelType: ChannelTelegram, Token: "shared"}},
		"bob":   {{ChannelType: ChannelTelegram, Token: "shared"}},
	}
	err := ValidateAccountChannels(tc)
	if err == nil {
		t.Fatal("expected duplicate bot_token error, got nil")
	}
	msg := err.Error()
	if !strings.Contains(msg, "telegram bot_token") {
		t.Errorf("error should mention telegram bot_token: %q", msg)
	}
	if !strings.Contains(msg, "alice") || !strings.Contains(msg, "bob") {
		t.Errorf("error should name both accounts: %q", msg)
	}
}

// TestValidateAccountChannels_KakaoDuplicate locks in the same rule for Kakao
// relay pairings — identical WS URL across accounts would dual-bind a single
// Kakao account.
func TestValidateAccountChannels_KakaoDuplicate(t *testing.T) {
	tc := map[string][]ChannelConfig{
		"alice":  {{ChannelType: ChannelKakaoTalk, KakaoWSURL: "wss://relay/ws/shared"}},
		"family": {{ChannelType: ChannelKakaoTalk, KakaoWSURL: "wss://relay/ws/shared"}},
	}
	err := ValidateAccountChannels(tc)
	if err == nil {
		t.Fatal("expected duplicate kakao URL error, got nil")
	}
	if !strings.Contains(err.Error(), "kakao relay URL") {
		t.Errorf("error should mention kakao relay URL: %q", err.Error())
	}
}

// TestValidateAccountChannels_EmptyTokensIgnored ensures that accounts with
// unset/empty tokens do not falsely collide (multiple accounts may legitimately
// have "" during half-completed setup).
func TestValidateAccountChannels_EmptyTokensIgnored(t *testing.T) {
	tc := map[string][]ChannelConfig{
		"alice": {{ChannelType: ChannelTelegram, Token: ""}},
		"bob":   {{ChannelType: ChannelTelegram, Token: ""}},
	}
	if err := ValidateAccountChannels(tc); err != nil {
		t.Errorf("empty tokens should not collide: %v", err)
	}
}

// TestValidateFamilyAccounts_RejectsChannels locks in the rule that an account
// marked IsFamily cannot own a chat channel. If the family bot kept a
// [telegram] block, it would swallow updates meant for whichever personal
// account shares the real bot_token, producing a silent delivery blackhole.
// Fail-fast at startup.
func TestValidateFamilyAccounts_RejectsChannels(t *testing.T) {
	accounts := []*Account{
		{ID: "alice", Config: &Config{}},
		{ID: "family", Config: &Config{
			IsFamily: true,
			Channels: []ChannelConfig{{ChannelType: ChannelTelegram, Token: "x"}},
		}},
	}
	err := ValidateFamilyAccounts(accounts)
	if err == nil {
		t.Fatal("expected family-with-channels to error")
	}
	if !strings.Contains(err.Error(), "family") || !strings.Contains(err.Error(), "telegram") {
		t.Errorf("error should cite account id and channel type: %q", err.Error())
	}
}

// TestValidateFamilyAccounts_PersonalWithChannelsOK confirms the check is
// scoped to the family flag — personal accounts declaring channels are
// the normal case and must pass.
func TestValidateFamilyAccounts_PersonalWithChannelsOK(t *testing.T) {
	accounts := []*Account{
		{ID: "alice", Config: &Config{
			Channels: []ChannelConfig{{ChannelType: ChannelTelegram, Token: "x"}},
		}},
		{ID: "family", Config: &Config{IsFamily: true}},
	}
	if err := ValidateFamilyAccounts(accounts); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

// TestValidateFamilyAccounts_NilConfigSkipped guards against a half-loaded
// account (Config == nil) panicking the startup path. Better to skip than
// to crash when a config file failed to parse earlier.
func TestValidateFamilyAccounts_NilConfigSkipped(t *testing.T) {
	accounts := []*Account{{ID: "ghost", Config: nil}}
	if err := ValidateFamilyAccounts(accounts); err != nil {
		t.Errorf("nil config should be skipped, got %v", err)
	}
}

// TestValidateAccountChannels_CrossChannelOK ensures the check only scopes
// within a single channel type — a Telegram token equal to a random string
// used elsewhere should not collide with Kakao URLs, etc.
func TestValidateAccountChannels_CrossChannelOK(t *testing.T) {
	tc := map[string][]ChannelConfig{
		"alice": {{ChannelType: ChannelTelegram, Token: "value"}},
		"bob":   {{ChannelType: ChannelKakaoTalk, KakaoWSURL: "value"}},
	}
	if err := ValidateAccountChannels(tc); err != nil {
		t.Errorf("cross-channel value reuse should not collide: %v", err)
	}
}
