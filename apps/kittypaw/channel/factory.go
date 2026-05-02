package channel

import (
	"fmt"

	"github.com/jinto/kittypaw/core"
)

// FromConfig creates a Channel from a ChannelConfig, tagging the resulting
// channel with accountID so every emitted Event can be routed to the right
// account's engine session by the AccountRouter.
func FromConfig(accountID string, cfg core.ChannelConfig) (Channel, error) {
	switch cfg.ChannelType {
	case core.ChannelTelegram:
		if cfg.Token == "" {
			return nil, fmt.Errorf("telegram channel requires a bot token")
		}
		return NewTelegram(accountID, cfg.Token), nil

	case core.ChannelSlack:
		if cfg.Token == "" {
			return nil, fmt.Errorf("slack channel requires a bot token")
		}
		// Slack needs both a bot token and an app-level token for socket mode.
		// The bot token is in cfg.Token; the app token is expected in BindAddr
		// as a secondary field (kept flat to match the Rust config shape).
		return NewSlack(accountID, cfg.Token, cfg.BindAddr), nil

	case core.ChannelDiscord:
		if cfg.Token == "" {
			return nil, fmt.Errorf("discord channel requires a bot token")
		}
		return NewDiscord(accountID, cfg.Token), nil

	case core.ChannelWeb:
		addr := cfg.BindAddr
		if addr == "" {
			addr = "127.0.0.1:8080"
		}
		return NewWebSocket(accountID, addr), nil

	case core.ChannelKakaoTalk:
		if cfg.KakaoWSURL == "" {
			return nil, fmt.Errorf("kakao channel requires login (run: kittypaw login)")
		}
		return NewKakao(accountID, cfg.KakaoWSURL), nil

	default:
		return nil, fmt.Errorf("unsupported channel type: %q", cfg.ChannelType)
	}
}
