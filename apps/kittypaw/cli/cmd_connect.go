package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/jinto/kittypaw/core"
)

var (
	connectOpenBrowser = core.OpenBrowser
	connectHTTPClient  = http.DefaultClient
	connectGmailRunner = runConnectGmail
)

func newConnectCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "connect",
		Short: "Connect external service accounts",
	}
	addPersistentAccountFlag(cmd)
	cmd.AddCommand(newConnectGmailCmd())
	return cmd
}

func newConnectGmailCmd() *cobra.Command {
	var (
		flagCode   bool
		flagAPIURL string
	)
	cmd := &cobra.Command{
		Use:   "gmail",
		Short: "Connect a Gmail account",
		RunE: func(cmd *cobra.Command, args []string) error {
			apiURL := strings.TrimRight(flagAPIURL, "/")
			if apiURL == "" {
				apiURL = core.DefaultAPIServerURL
			}
			accountID, err := resolveCLIAccountWithContext(flagAccount)
			if err != nil {
				return err
			}
			useCode := flagCode || !term.IsTerminal(int(os.Stdin.Fd()))
			return connectGmailRunner(apiURL, accountID, useCode)
		},
	}
	cmd.Flags().BoolVar(&flagCode, "code", false, "use code-paste mode")
	cmd.Flags().StringVar(&flagAPIURL, "api-url", "", "API server URL (default "+core.DefaultAPIServerURL+")")
	return cmd
}

func runConnectGmail(apiURL, accountID string, useCode bool) error {
	secrets, err := core.LoadAccountSecrets(accountID)
	if err != nil {
		return fmt.Errorf("load secrets: %w", err)
	}
	apiMgr := core.NewAPITokenManager("", secrets)
	serviceMgr := core.NewServiceTokenManager(secrets)
	if useCode {
		return connectGmailCode(apiURL, apiMgr, serviceMgr)
	}
	return connectGmailHTTP(apiURL, apiMgr, serviceMgr)
}

func connectGmailCode(apiURL string, apiMgr *core.APITokenManager, serviceMgr *core.ServiceTokenManager) error {
	_ = applyDiscovery(apiURL, apiMgr)
	connectBaseURL := apiMgr.ResolveConnectBaseURL(apiURL)
	loginURL := connectGmailLoginURL(apiURL, apiMgr, "code", 0)
	fmt.Printf("Open this URL in your browser:\n\n  %s\n\n", loginURL)
	fmt.Printf("Enter the code from the browser: ")
	var code string
	if _, err := fmt.Scanln(&code); err != nil {
		return fmt.Errorf("read code: %w", err)
	}
	code = strings.TrimSpace(code)
	if code == "" {
		return fmt.Errorf("empty code")
	}
	return exchangeConnectCode(connectBaseURL, code, serviceMgr)
}

func connectGmailHTTP(apiURL string, apiMgr *core.APITokenManager, serviceMgr *core.ServiceTokenManager) error {
	_ = applyDiscovery(apiURL, apiMgr)
	connectBaseURL := apiMgr.ResolveConnectBaseURL(apiURL)

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return fmt.Errorf("start callback server: %w", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port

	codeCh := make(chan connectCodeResult, 1)
	mux := http.NewServeMux()
	mux.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		code, err := connectCallbackCode(r)
		if err != nil {
			codeCh <- connectCodeResult{err: err}
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		codeCh <- connectCodeResult{code: code}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = fmt.Fprint(w, loginSuccessHTML)
	})

	srv := &http.Server{Handler: mux}
	go func() { _ = srv.Serve(listener) }()
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	}()

	loginURL := connectGmailLoginURL(apiURL, apiMgr, "http", port)
	fmt.Printf("Opening browser for Gmail connection...\n")
	fmt.Printf("If the browser doesn't open, visit:\n  %s\n\n", loginURL)
	if err := connectOpenBrowser(loginURL); err != nil {
		fmt.Printf("Could not open browser: %v\n", err)
	}

	fmt.Printf("Waiting for authorization (5 minute timeout)...\n")
	select {
	case result := <-codeCh:
		if result.err != nil {
			return result.err
		}
		return exchangeConnectCode(connectBaseURL, result.code, serviceMgr)
	case <-time.After(5 * time.Minute):
		return fmt.Errorf("gmail connection timed out (5 minutes)")
	}
}

type connectCodeResult struct {
	code string
	err  error
}

func connectGmailLoginURL(apiURL string, mgr *core.APITokenManager, mode string, port int) string {
	base := mgr.ResolveConnectBaseURL(apiURL)
	params := "mode=" + mode
	if mode == "http" {
		params += fmt.Sprintf("&port=%d", port)
	}
	return strings.TrimRight(base, "/") + "/connect/gmail/login?" + params
}

func connectCallbackCode(r *http.Request) (string, error) {
	q := r.URL.Query()
	if q.Get("access_token") != "" || q.Get("refresh_token") != "" {
		return "", fmt.Errorf("expected one-time code callback, got token query params")
	}
	code := strings.TrimSpace(q.Get("code"))
	if code == "" {
		return "", fmt.Errorf("missing one-time code")
	}
	return code, nil
}

func exchangeConnectCode(connectBaseURL, code string, serviceMgr *core.ServiceTokenManager) error {
	payload, _ := json.Marshal(map[string]string{"code": code})
	resp, err := connectHTTPClient.Post(strings.TrimRight(connectBaseURL, "/")+"/connect/cli/exchange", "application/json", strings.NewReader(string(payload)))
	if err != nil {
		return fmt.Errorf("exchange request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("exchange failed (%d): %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var result struct {
		Provider     string `json:"provider"`
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		TokenType    string `json:"token_type"`
		ExpiresIn    int    `json:"expires_in"`
		Scope        string `json:"scope"`
		Email        string `json:"email"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	if result.Provider == "" {
		result.Provider = "gmail"
	}
	return serviceMgr.Save(result.Provider, core.ServiceTokenSet{
		Provider:       result.Provider,
		AccessToken:    result.AccessToken,
		RefreshToken:   result.RefreshToken,
		TokenType:      result.TokenType,
		ExpiresIn:      result.ExpiresIn,
		Scope:          result.Scope,
		Email:          result.Email,
		ConnectBaseURL: strings.TrimRight(connectBaseURL, "/"),
	})
}
