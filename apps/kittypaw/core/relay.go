package core

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// RelayRegistration is the POST {relay_url}/register response from the
// KakaoTalk relay server.
type RelayRegistration struct {
	Token      string `json:"token"`
	PairCode   string `json:"pair_code"`
	ChannelURL string `json:"channel_url"`
}

// RegisterRelaySession creates a new KakaoTalk relay session for this client
// and returns the relay-issued token, pair code, and Kakao channel URL.
func RegisterRelaySession(relayBase string) (*RelayRegistration, error) {
	endpoint := strings.TrimRight(relayBase, "/") + "/register"
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Post(endpoint, "application/json", nil)
	if err != nil {
		return nil, fmt.Errorf("register request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("register returned %d", resp.StatusCode)
	}

	var reg RelayRegistration
	if err := json.NewDecoder(resp.Body).Decode(&reg); err != nil {
		return nil, fmt.Errorf("decode register response: %w", err)
	}
	if reg.Token == "" || reg.PairCode == "" {
		return nil, fmt.Errorf("register response missing token or pair_code")
	}
	return &reg, nil
}

// WSURLFromRelay builds the WebSocket URL for a given relay base + token.
// Converts https→wss and http→ws.
func WSURLFromRelay(relayBase, token string) string {
	u := strings.TrimRight(relayBase, "/")
	switch {
	case strings.HasPrefix(u, "https://"):
		u = "wss://" + strings.TrimPrefix(u, "https://")
	case strings.HasPrefix(u, "http://"):
		u = "ws://" + strings.TrimPrefix(u, "http://")
	}
	return u + "/ws/" + token
}

// CheckRelayPairStatus polls GET {relay_base}/pair-status/{token} and returns
// true once the pairing has been completed on the KakaoTalk side.
func CheckRelayPairStatus(relayBase, token string) bool {
	url := strings.TrimRight(relayBase, "/") + "/pair-status/" + token
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	var result struct {
		Paired bool `json:"paired"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return false
	}
	return result.Paired
}
