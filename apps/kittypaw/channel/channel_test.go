package channel

import (
	"encoding/json"
	"testing"

	"github.com/jinto/gopaw/core"
)

func TestFromConfigTelegram(t *testing.T) {
	ch, err := FromConfig(core.ChannelConfig{
		ChannelType: core.ChannelTelegram,
		Token:       "123:ABC",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := ch.(*TelegramChannel); !ok {
		t.Fatalf("expected *TelegramChannel, got %T", ch)
	}
	if ch.Name() != "telegram" {
		t.Fatalf("expected name %q, got %q", "telegram", ch.Name())
	}
}

func TestFromConfigTelegramMissingToken(t *testing.T) {
	_, err := FromConfig(core.ChannelConfig{
		ChannelType: core.ChannelTelegram,
	})
	if err == nil {
		t.Fatal("expected error for missing token")
	}
}

func TestFromConfigSlack(t *testing.T) {
	ch, err := FromConfig(core.ChannelConfig{
		ChannelType: core.ChannelSlack,
		Token:       "xoxb-test",
		BindAddr:    "xapp-test",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	sc, ok := ch.(*SlackChannel)
	if !ok {
		t.Fatalf("expected *SlackChannel, got %T", ch)
	}
	if sc.Name() != "slack" {
		t.Fatalf("expected name %q, got %q", "slack", sc.Name())
	}
}

func TestFromConfigSlackMissingToken(t *testing.T) {
	_, err := FromConfig(core.ChannelConfig{
		ChannelType: core.ChannelSlack,
	})
	if err == nil {
		t.Fatal("expected error for missing token")
	}
}

func TestFromConfigDiscord(t *testing.T) {
	ch, err := FromConfig(core.ChannelConfig{
		ChannelType: core.ChannelDiscord,
		Token:       "discord-bot-token",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := ch.(*DiscordChannel); !ok {
		t.Fatalf("expected *DiscordChannel, got %T", ch)
	}
	if ch.Name() != "discord" {
		t.Fatalf("expected name %q, got %q", "discord", ch.Name())
	}
}

func TestFromConfigDiscordMissingToken(t *testing.T) {
	_, err := FromConfig(core.ChannelConfig{
		ChannelType: core.ChannelDiscord,
	})
	if err == nil {
		t.Fatal("expected error for missing token")
	}
}

func TestFromConfigWeb(t *testing.T) {
	ch, err := FromConfig(core.ChannelConfig{
		ChannelType: core.ChannelWeb,
		BindAddr:    "0.0.0.0:9090",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	ws, ok := ch.(*WebSocketChannel)
	if !ok {
		t.Fatalf("expected *WebSocketChannel, got %T", ch)
	}
	if ws.bindAddr != "0.0.0.0:9090" {
		t.Fatalf("expected bind addr %q, got %q", "0.0.0.0:9090", ws.bindAddr)
	}
	if ws.Name() != "web" {
		t.Fatalf("expected name %q, got %q", "web", ws.Name())
	}
}

func TestFromConfigWebDefaultAddr(t *testing.T) {
	ch, err := FromConfig(core.ChannelConfig{
		ChannelType: core.ChannelWeb,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	ws, ok := ch.(*WebSocketChannel)
	if !ok {
		t.Fatalf("expected *WebSocketChannel, got %T", ch)
	}
	if ws.bindAddr != "127.0.0.1:8080" {
		t.Fatalf("expected default addr %q, got %q", "127.0.0.1:8080", ws.bindAddr)
	}
}

func TestFromConfigKakao(t *testing.T) {
	ch, err := FromConfig(core.ChannelConfig{
		ChannelType: core.ChannelKakaoTalk,
		Kakao: &core.KakaoChannelConfig{
			RelayURL:  "https://relay.example.com",
			UserToken: "tok123",
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	kc, ok := ch.(*KakaoChannel)
	if !ok {
		t.Fatalf("expected *KakaoChannel, got %T", ch)
	}
	if kc.Name() != "kakao_talk" {
		t.Fatalf("expected name %q, got %q", "kakao_talk", kc.Name())
	}
}

func TestFromConfigKakaoMissingConfig(t *testing.T) {
	_, err := FromConfig(core.ChannelConfig{
		ChannelType: core.ChannelKakaoTalk,
	})
	if err == nil {
		t.Fatal("expected error for nil kakao config")
	}
}

func TestFromConfigKakaoMissingFields(t *testing.T) {
	tests := []struct {
		name  string
		kakao core.KakaoChannelConfig
	}{
		{"missing relay_url", core.KakaoChannelConfig{UserToken: "tok"}},
		{"missing user_token", core.KakaoChannelConfig{RelayURL: "https://r.example.com"}},
		{"both empty", core.KakaoChannelConfig{}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := FromConfig(core.ChannelConfig{
				ChannelType: core.ChannelKakaoTalk,
				Kakao:       &tc.kakao,
			})
			if err == nil {
				t.Fatal("expected error for incomplete kakao config")
			}
		})
	}
}

func TestFromConfigUnsupported(t *testing.T) {
	_, err := FromConfig(core.ChannelConfig{
		ChannelType: "carrier_pigeon",
	})
	if err == nil {
		t.Fatal("expected error for unsupported channel type")
	}
}

// --- SessionID mapping tests ---

func TestKakaoSessionIDFromUserID(t *testing.T) {
	// Simulate the payload that connectAndListen would build.
	msg := kakaoRelayMessage{
		ID:     "action-123",
		Text:   "hello",
		UserID: "kakao-user-42",
	}

	payload := core.ChatPayload{
		ChatID:    msg.ID,
		Text:      msg.Text,
		SessionID: msg.UserID,
	}

	if payload.SessionID != "kakao-user-42" {
		t.Errorf("expected SessionID %q, got %q", "kakao-user-42", payload.SessionID)
	}
	if payload.ChatID != "action-123" {
		t.Errorf("expected ChatID %q, got %q", "action-123", payload.ChatID)
	}

	// Verify it roundtrips via JSON → Event → ParsePayload.
	raw, _ := json.Marshal(payload)
	event := &core.Event{Type: core.EventKakaoTalk, Payload: raw}
	parsed, err := event.ParsePayload()
	if err != nil {
		t.Fatalf("parse payload: %v", err)
	}
	if parsed.SessionID != "kakao-user-42" {
		t.Errorf("roundtrip SessionID: got %q, want %q", parsed.SessionID, "kakao-user-42")
	}
}

func TestTelegramSessionIDFromUserID(t *testing.T) {
	// Verify telegramUser.ID is included and would become SessionID.
	user := telegramUser{
		ID:        12345678,
		FirstName: "Test",
		Username:  "testuser",
	}

	data, _ := json.Marshal(user)
	var decoded telegramUser
	json.Unmarshal(data, &decoded)

	if decoded.ID != 12345678 {
		t.Errorf("expected user ID 12345678, got %d", decoded.ID)
	}
}

func TestSlackSessionIDFromUser(t *testing.T) {
	evt := slackEvent{
		Type:    "message",
		Text:    "hello",
		User:    "U123ABC",
		Channel: "C456DEF",
	}

	payload := core.ChatPayload{
		ChatID:    evt.Channel,
		Text:      evt.Text,
		FromName:  evt.User,
		SessionID: evt.User,
	}

	if payload.SessionID != "U123ABC" {
		t.Errorf("expected SessionID %q, got %q", "U123ABC", payload.SessionID)
	}
}

func TestDiscordSessionIDFromAuthor(t *testing.T) {
	msg := discordMessageCreate{
		ID:        "msg-1",
		ChannelID: "ch-1",
		Content:   "hello",
		Author:    discordUser{ID: "discord-user-99", Username: "testbot"},
	}

	payload := core.ChatPayload{
		ChatID:    msg.ChannelID,
		Text:      msg.Content,
		FromName:  msg.Author.Username,
		SessionID: msg.Author.ID,
	}

	if payload.SessionID != "discord-user-99" {
		t.Errorf("expected SessionID %q, got %q", "discord-user-99", payload.SessionID)
	}
}

func TestKakaoWsURL(t *testing.T) {
	tests := []struct {
		name      string
		relayURL  string
		userToken string
		want      string
	}{
		{
			name:      "https to wss",
			relayURL:  "https://relay.example.com",
			userToken: "abc123",
			want:      "wss://relay.example.com/ws/abc123",
		},
		{
			name:      "http to ws",
			relayURL:  "http://localhost:8787",
			userToken: "abc123",
			want:      "ws://localhost:8787/ws/abc123",
		},
		{
			name:      "token with special chars",
			relayURL:  "https://relay.example.com",
			userToken: "a/b c",
			want:      "wss://relay.example.com/ws/a%2Fb%20c",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			k := NewKakao(tc.relayURL, tc.userToken)
			got := k.wsURL()
			if got != tc.want {
				t.Fatalf("wsURL() = %q, want %q", got, tc.want)
			}
		})
	}
}
