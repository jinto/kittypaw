package main

import (
	"testing"

	"github.com/jinto/kittypaw/core"
	"github.com/jinto/kittypaw/server"
)

func TestChatRelayConnectorConfigsRequiresCompleteAccountSecrets(t *testing.T) {
	secrets := testSecretsStore(t)
	mgr := core.NewAPITokenManager("", secrets)
	apiURL := core.DefaultAPIServerURL
	if err := mgr.SaveChatRelayURL(apiURL, "https://chat.kittypaw.app"); err != nil {
		t.Fatal(err)
	}
	if err := mgr.SaveChatRelayDeviceID(apiURL, "dev_123"); err != nil {
		t.Fatal(err)
	}
	if err := mgr.SaveChatDaemonCredential(apiURL, "device-token-1"); err != nil {
		t.Fatal(err)
	}

	got := chatRelayConnectorConfigs([]*server.AccountDeps{
		{
			Account:     &core.Account{ID: "alice"},
			Secrets:     secrets,
			APITokenMgr: mgr,
		},
	}, "0.1.5", false)

	if len(got) != 1 {
		t.Fatalf("connector configs = %d, want 1", len(got))
	}
	cfg := got[0]
	if cfg.RelayURL != "https://chat.kittypaw.app" {
		t.Fatalf("RelayURL = %q", cfg.RelayURL)
	}
	if cfg.DeviceID != "dev_123" {
		t.Fatalf("DeviceID = %q", cfg.DeviceID)
	}
	if cfg.Credential != "device-token-1" {
		t.Fatalf("Credential = %q", cfg.Credential)
	}
	if len(cfg.LocalAccounts) != 1 || cfg.LocalAccounts[0] != "alice" {
		t.Fatalf("LocalAccounts = %#v", cfg.LocalAccounts)
	}
	if cfg.DaemonVersion != "0.1.5" {
		t.Fatalf("DaemonVersion = %q", cfg.DaemonVersion)
	}
	if len(cfg.Capabilities) != 0 {
		t.Fatalf("Capabilities = %#v, want none until operation dispatch is wired", cfg.Capabilities)
	}
}

func TestChatRelayConnectorConfigsAdvertisesDefaultCapabilitiesWhenDispatchIsReady(t *testing.T) {
	secrets := testSecretsStore(t)
	mgr := core.NewAPITokenManager("", secrets)
	apiURL := core.DefaultAPIServerURL
	if err := mgr.SaveChatRelayURL(apiURL, "https://chat.kittypaw.app"); err != nil {
		t.Fatal(err)
	}
	if err := mgr.SaveChatRelayDeviceID(apiURL, "dev_123"); err != nil {
		t.Fatal(err)
	}
	if err := mgr.SaveChatDaemonCredential(apiURL, "device-token-1"); err != nil {
		t.Fatal(err)
	}

	got := chatRelayConnectorConfigs([]*server.AccountDeps{
		{
			Account:     &core.Account{ID: "alice"},
			Secrets:     secrets,
			APITokenMgr: mgr,
		},
	}, "0.1.5", true)

	if len(got) != 1 {
		t.Fatalf("connector configs = %d, want 1", len(got))
	}
	if len(got[0].Capabilities) != 0 {
		t.Fatalf("Capabilities = %#v, want nil/default capabilities", got[0].Capabilities)
	}
	if got[0].Capabilities != nil {
		t.Fatalf("Capabilities = %#v, want nil so protocol defaults are advertised", got[0].Capabilities)
	}
}

func TestChatRelayConnectorConfigsGroupsAccountsForSameDeviceCredential(t *testing.T) {
	aliceSecrets := testSecretsStore(t)
	aliceMgr := core.NewAPITokenManager("", aliceSecrets)
	bobSecrets := testSecretsStore(t)
	bobMgr := core.NewAPITokenManager("", bobSecrets)
	for _, mgr := range []*core.APITokenManager{aliceMgr, bobMgr} {
		if err := mgr.SaveChatRelayURL(core.DefaultAPIServerURL, "https://chat.kittypaw.app"); err != nil {
			t.Fatal(err)
		}
		if err := mgr.SaveChatRelayDeviceID(core.DefaultAPIServerURL, "dev_123"); err != nil {
			t.Fatal(err)
		}
		if err := mgr.SaveChatDaemonCredential(core.DefaultAPIServerURL, "device-token-1"); err != nil {
			t.Fatal(err)
		}
	}

	got := chatRelayConnectorConfigs([]*server.AccountDeps{
		{Account: &core.Account{ID: "alice"}, Secrets: aliceSecrets, APITokenMgr: aliceMgr},
		{Account: &core.Account{ID: "bob"}, Secrets: bobSecrets, APITokenMgr: bobMgr},
	}, "0.1.5", false)

	if len(got) != 1 {
		t.Fatalf("connector configs = %d, want one grouped device connector", len(got))
	}
	if want := []string{"alice", "bob"}; !equalStringSlices(got[0].LocalAccounts, want) {
		t.Fatalf("LocalAccounts = %#v, want %#v", got[0].LocalAccounts, want)
	}
}

func TestChatRelayConnectorConfigsSeparatesSameDeviceWithDifferentCredential(t *testing.T) {
	aliceSecrets := testSecretsStore(t)
	aliceMgr := core.NewAPITokenManager("", aliceSecrets)
	bobSecrets := testSecretsStore(t)
	bobMgr := core.NewAPITokenManager("", bobSecrets)
	for _, setup := range []struct {
		mgr        *core.APITokenManager
		credential string
	}{
		{aliceMgr, "device-token-user-1"},
		{bobMgr, "device-token-user-2"},
	} {
		if err := setup.mgr.SaveChatRelayURL(core.DefaultAPIServerURL, "https://chat.kittypaw.app"); err != nil {
			t.Fatal(err)
		}
		if err := setup.mgr.SaveChatRelayDeviceID(core.DefaultAPIServerURL, "dev_123"); err != nil {
			t.Fatal(err)
		}
		if err := setup.mgr.SaveChatDaemonCredential(core.DefaultAPIServerURL, setup.credential); err != nil {
			t.Fatal(err)
		}
	}

	got := chatRelayConnectorConfigs([]*server.AccountDeps{
		{Account: &core.Account{ID: "alice"}, Secrets: aliceSecrets, APITokenMgr: aliceMgr},
		{Account: &core.Account{ID: "bob"}, Secrets: bobSecrets, APITokenMgr: bobMgr},
	}, "0.1.5", false)

	if len(got) != 2 {
		t.Fatalf("connector configs = %d, want two credentials kept separate", len(got))
	}
}

func TestChatRelayConnectorConfigsSkipsPartialSecrets(t *testing.T) {
	secrets := testSecretsStore(t)
	mgr := core.NewAPITokenManager("", secrets)
	if err := mgr.SaveChatRelayURL(core.DefaultAPIServerURL, "https://chat.kittypaw.app"); err != nil {
		t.Fatal(err)
	}
	if err := mgr.SaveChatRelayDeviceID(core.DefaultAPIServerURL, "dev_123"); err != nil {
		t.Fatal(err)
	}

	got := chatRelayConnectorConfigs([]*server.AccountDeps{
		{
			Account:     &core.Account{ID: "alice"},
			Secrets:     secrets,
			APITokenMgr: mgr,
		},
	}, "0.1.5", false)

	if len(got) != 0 {
		t.Fatalf("connector configs = %#v, want none without credential", got)
	}
}

func testSecretsStore(t *testing.T) *core.SecretsStore {
	t.Helper()
	secrets, err := core.LoadSecretsFrom(t.TempDir() + "/secrets.json")
	if err != nil {
		t.Fatal(err)
	}
	return secrets
}

func equalStringSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
