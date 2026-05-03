package main

import (
	"errors"
	"testing"

	"github.com/jinto/kittypaw/core"
)

func TestPrepareKakaoPairingBuildsWizardResult(t *testing.T) {
	var gotAPIURL, gotRelayBase string
	prepared, err := prepareKakaoPairing("https://portal.example", kakaoPairingDeps{
		fetchDiscovery: func(apiURL string) (*core.DiscoveryResponse, error) {
			gotAPIURL = apiURL
			return &core.DiscoveryResponse{
				APIBaseURL:    "https://api.example",
				KakaoRelayURL: "https://kakao.example",
			}, nil
		},
		registerRelaySession: func(relayBase string) (*core.RelayRegistration, error) {
			gotRelayBase = relayBase
			return &core.RelayRegistration{
				Token:      "relay-token",
				PairCode:   "123456",
				ChannelURL: "https://pf.kakao.com/example",
			}, nil
		},
	})
	if err != nil {
		t.Fatalf("prepareKakaoPairing: %v", err)
	}

	if gotAPIURL != "https://portal.example" {
		t.Fatalf("fetchDiscovery apiURL = %q, want portal URL", gotAPIURL)
	}
	if gotRelayBase != "https://kakao.example" {
		t.Fatalf("registerRelaySession base = %q, want discovery relay URL", gotRelayBase)
	}
	if prepared.Wizard.APIServerURL != "https://portal.example" {
		t.Fatalf("APIServerURL = %q, want portal URL", prepared.Wizard.APIServerURL)
	}
	if !prepared.Wizard.KakaoEnabled {
		t.Fatal("KakaoEnabled = false, want true")
	}
	if prepared.Wizard.KakaoRelayWSURL != "wss://kakao.example/ws/relay-token" {
		t.Fatalf("KakaoRelayWSURL = %q", prepared.Wizard.KakaoRelayWSURL)
	}
	if prepared.RelayBase != "https://kakao.example" {
		t.Fatalf("RelayBase = %q", prepared.RelayBase)
	}
	if prepared.Registration.PairCode != "123456" {
		t.Fatalf("PairCode = %q, want 123456", prepared.Registration.PairCode)
	}
}

func TestPrepareKakaoPairingRequiresDiscoveryRelayURL(t *testing.T) {
	_, err := prepareKakaoPairing(core.DefaultAPIServerURL, kakaoPairingDeps{
		fetchDiscovery: func(string) (*core.DiscoveryResponse, error) {
			return &core.DiscoveryResponse{APIBaseURL: "https://api.example"}, nil
		},
		registerRelaySession: func(string) (*core.RelayRegistration, error) {
			t.Fatal("registerRelaySession should not run without kakao_relay_url")
			return nil, nil
		},
	})
	if err == nil {
		t.Fatal("expected missing relay URL error, got nil")
	}
	if !errors.Is(err, errKakaoRelayUnavailable) {
		t.Fatalf("err = %v, want errKakaoRelayUnavailable", err)
	}
}
