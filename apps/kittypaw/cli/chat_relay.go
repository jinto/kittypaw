package main

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/jinto/kittypaw/core"
	"github.com/jinto/kittypaw/remote/chatrelay"
	"github.com/jinto/kittypaw/server"
)

func chatRelayConnectorConfigs(deps []*server.AccountDeps, daemonVersion string) []chatrelay.ConnectorConfig {
	configs := make([]chatrelay.ConnectorConfig, 0, len(deps))
	groupIndex := make(map[chatRelayConnectorKey]int)
	for _, dep := range deps {
		cfg, ok := chatRelayConnectorConfig(dep, daemonVersion)
		if !ok {
			continue
		}
		key := chatRelayConnectorKey{
			RelayURL:   cfg.RelayURL,
			Credential: cfg.Credential,
			DeviceID:   cfg.DeviceID,
		}
		if idx, exists := groupIndex[key]; exists {
			for _, account := range cfg.LocalAccounts {
				if !containsString(configs[idx].LocalAccounts, account) {
					configs[idx].LocalAccounts = append(configs[idx].LocalAccounts, account)
				}
			}
		} else {
			groupIndex[key] = len(configs)
			configs = append(configs, cfg)
		}
	}
	return configs
}

type chatRelayConnectorKey struct {
	RelayURL   string
	Credential string
	DeviceID   string
}

func chatRelayConnectorConfig(dep *server.AccountDeps, daemonVersion string) (chatrelay.ConnectorConfig, bool) {
	if dep == nil || dep.Account == nil || dep.Account.ID == "" || dep.Secrets == nil || dep.APITokenMgr == nil {
		return chatrelay.ConnectorConfig{}, false
	}
	apiURL := accountAPIURL(dep.Secrets)
	relayURL, ok := dep.APITokenMgr.LoadChatRelayURL(apiURL)
	if !ok || relayURL == "" {
		return chatrelay.ConnectorConfig{}, false
	}
	deviceID, ok := dep.APITokenMgr.LoadChatRelayDeviceID(apiURL)
	if !ok || deviceID == "" {
		return chatrelay.ConnectorConfig{}, false
	}
	credential, ok := dep.APITokenMgr.LoadChatDaemonCredential(apiURL)
	if !ok || credential == "" {
		return chatrelay.ConnectorConfig{}, false
	}
	return chatrelay.ConnectorConfig{
		RelayURL:      relayURL,
		Credential:    credential,
		DeviceID:      deviceID,
		LocalAccounts: []string{dep.Account.ID},
		DaemonVersion: daemonVersion,
		Capabilities:  []string{},
	}, true
}

func startChatRelayConnectors(ctx context.Context, deps []*server.AccountDeps, daemonVersion string) {
	for _, cfg := range chatRelayConnectorConfigs(deps, daemonVersion) {
		connector := &chatrelay.Connector{Config: cfg}
		go connector.Run(ctx, chatrelay.RunOptions{
			Logf: func(format string, args ...any) {
				slog.Debug("chat relay connector", "message", formatLog(format, args...))
			},
		})
	}
}

func accountAPIURL(secrets *core.SecretsStore) string {
	if secrets != nil {
		if apiURL, ok := secrets.Get("kittypaw-api", "api_url"); ok && apiURL != "" {
			return apiURL
		}
	}
	return core.DefaultAPIServerURL
}

func containsString(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}

func formatLog(format string, args ...any) string {
	if len(args) == 0 {
		return format
	}
	return fmt.Sprintf(format, args...)
}
