package channel

import (
	"fmt"

	"github.com/jinto/gopaw/core"
)

// FromConfig creates a Channel from a ChannelConfig.
func FromConfig(cfg core.ChannelConfig) (Channel, error) {
	switch cfg.ChannelType {
	case core.ChannelTelegram:
		if cfg.Token == "" {
			return nil, fmt.Errorf("telegram channel requires a bot token")
		}
		return NewTelegram(cfg.Token), nil

	case core.ChannelSlack:
		if cfg.Token == "" {
			return nil, fmt.Errorf("slack channel requires a bot token")
		}
		// Slack needs both a bot token and an app-level token for socket mode.
		// The bot token is in cfg.Token; the app token is expected in BindAddr
		// as a secondary field (kept flat to match the Rust config shape).
		return NewSlack(cfg.Token, cfg.BindAddr), nil

	case core.ChannelDiscord:
		if cfg.Token == "" {
			return nil, fmt.Errorf("discord channel requires a bot token")
		}
		return NewDiscord(cfg.Token), nil

	case core.ChannelWeb:
		addr := cfg.BindAddr
		if addr == "" {
			addr = "127.0.0.1:8080"
		}
		return NewWebSocket(addr), nil

	case core.ChannelKakaoTalk:
		if cfg.Kakao == nil {
			return nil, fmt.Errorf("kakao channel requires kakao config section")
		}
		if cfg.Kakao.RelayURL == "" || cfg.Kakao.UserToken == "" {
			return nil, fmt.Errorf("kakao channel requires relay_url and user_token")
		}
		return NewKakao(cfg.Kakao.RelayURL, cfg.Kakao.UserToken), nil

	default:
		return nil, fmt.Errorf("unsupported channel type: %q", cfg.ChannelType)
	}
}
