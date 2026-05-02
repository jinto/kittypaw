package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/jinto/kittypaw/core"
)

type chatRelayFlags struct {
	account string
	apiURL  string
	name    string
}

func newChatRelayCmd() *cobra.Command {
	flags := &chatRelayFlags{}
	cmd := &cobra.Command{
		Use:   "chat-relay",
		Short: "Manage hosted chat relay device credentials",
	}
	cmd.PersistentFlags().StringVar(&flags.account, "account", "", "use this local account")
	cmd.PersistentFlags().StringVar(&flags.apiURL, "api-url", "", "API server URL (default "+core.DefaultAPIServerURL+")")

	pairCmd := &cobra.Command{
		Use:   "pair",
		Short: "Pair this daemon with the hosted chat relay",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runChatRelayPair(flags)
		},
	}
	pairCmd.Flags().StringVar(&flags.name, "name", "", "device display name")

	statusCmd := &cobra.Command{
		Use:   "status",
		Short: "Show local chat relay credential status",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runChatRelayStatus(flags)
		},
	}

	disconnectCmd := &cobra.Command{
		Use:   "disconnect",
		Short: "Remove local chat relay device credentials",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runChatRelayDisconnect(flags)
		},
	}

	cmd.AddCommand(pairCmd, statusCmd, disconnectCmd)
	return cmd
}

func runChatRelayPair(flags *chatRelayFlags) error {
	accountID, mgr, apiURL, err := chatRelayAccountManager(flags)
	if err != nil {
		return err
	}
	_ = applyDiscovery(apiURL, mgr)

	accessToken, err := mgr.LoadAccessToken(apiURL)
	if err != nil {
		return err
	}
	if accessToken == "" {
		return fmt.Errorf("not logged in to %s for account %q; run `kittypaw setup` or `kittypaw login` first", apiURL, accountID)
	}

	name := strings.TrimSpace(flags.name)
	if name == "" {
		if host, err := os.Hostname(); err == nil {
			name = host
		}
	}
	tokens, err := mgr.PairChatRelayDevice(mgr.ResolveAuthBaseURL(apiURL), apiURL, accessToken, core.ChatRelayDevicePairRequest{Name: name})
	if err != nil {
		return err
	}
	fmt.Printf("paired chat relay device for account %s\n", accountID)
	fmt.Printf("device_id: %s\n", tokens.DeviceID)
	if relayURL, ok := mgr.LoadChatRelayURL(apiURL); ok && relayURL != "" {
		fmt.Printf("chat_relay_url: %s\n", relayURL)
	}
	return nil
}

func runChatRelayStatus(flags *chatRelayFlags) error {
	accountID, mgr, apiURL, err := chatRelayAccountManager(flags)
	if err != nil {
		return err
	}
	authBaseURL := mgr.ResolveAuthBaseURL(apiURL)
	relayURL, relayOK := mgr.LoadChatRelayURL(apiURL)
	tokens, tokenOK := mgr.LoadChatRelayDeviceTokens(apiURL)

	fmt.Printf("account: %s\n", accountID)
	fmt.Printf("api_url: %s\n", apiURL)
	fmt.Printf("auth_base_url: %s\n", authBaseURL)
	if relayOK && relayURL != "" {
		fmt.Printf("chat_relay_url: %s\n", relayURL)
	} else {
		fmt.Println("chat_relay_url: not configured")
	}
	if tokenOK {
		fmt.Printf("device_id: %s\n", tokens.DeviceID)
		if expired, ok := mgr.ChatRelayDeviceAccessTokenExpired(apiURL); ok && expired {
			fmt.Println("access_token: stored (refresh needed)")
		} else {
			fmt.Println("access_token: stored")
		}
		fmt.Println("refresh_token: stored")
	} else {
		fmt.Println("device: not paired")
	}
	return nil
}

func runChatRelayDisconnect(flags *chatRelayFlags) error {
	accountID, mgr, apiURL, err := chatRelayAccountManager(flags)
	if err != nil {
		return err
	}
	if err := mgr.ClearChatRelayDeviceTokens(apiURL); err != nil {
		return err
	}
	fmt.Printf("removed local chat relay device tokens for account %s\n", accountID)
	return nil
}

func chatRelayAccountManager(flags *chatRelayFlags) (string, *core.APITokenManager, string, error) {
	accountID, err := resolveCLIAccount(flags.account)
	if err != nil {
		return "", nil, "", err
	}
	secrets, err := core.LoadAccountSecrets(accountID)
	if err != nil {
		return "", nil, "", fmt.Errorf("load account secrets: %w", err)
	}
	apiURL := strings.TrimRight(flags.apiURL, "/")
	if apiURL == "" {
		apiURL = accountAPIURL(secrets)
	}
	return accountID, core.NewAPITokenManager("", secrets), apiURL, nil
}
