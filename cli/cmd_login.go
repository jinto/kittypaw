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

func newLoginCmd() *cobra.Command {
	var (
		flagCode   bool
		flagAPIURL string
	)

	cmd := &cobra.Command{
		Use:   "login",
		Short: "Authenticate with kittypaw API server",
		RunE: func(cmd *cobra.Command, args []string) error {
			apiURL := flagAPIURL
			if apiURL == "" {
				apiURL = core.DefaultAPIServerURL
			}
			apiURL = strings.TrimRight(apiURL, "/")

			secrets, err := core.LoadSecrets()
			if err != nil {
				return fmt.Errorf("load secrets: %w", err)
			}
			mgr := core.NewAPITokenManager("", secrets)

			// Auto-detect: use code mode if no TTY or flag set.
			useCode := flagCode || !term.IsTerminal(int(os.Stdin.Fd()))

			if useCode {
				_, err = loginCode(apiURL, mgr)
			} else {
				_, err = loginHTTP(apiURL, mgr)
			}
			return err
		},
	}

	cmd.Flags().BoolVar(&flagCode, "code", false, "use code-paste mode (for SSH/remote)")
	cmd.Flags().StringVar(&flagAPIURL, "api-url", "", "API server URL (default "+core.DefaultAPIServerURL+")")
	return cmd
}

func loginHTTP(apiURL string, mgr *core.APITokenManager) (*loginResult, error) {
	// 1. Start local callback server on OS-assigned port.
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("start callback server: %w", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port

	tokenCh := make(chan *tokenResult, 1)

	mux := http.NewServeMux()
	mux.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		accessToken := r.URL.Query().Get("access_token")
		refreshToken := r.URL.Query().Get("refresh_token")
		relayURL := r.URL.Query().Get("relay_url")

		if accessToken == "" {
			tokenCh <- &tokenResult{err: fmt.Errorf("no access_token in callback")}
			http.Error(w, "missing token", http.StatusBadRequest)
			return
		}

		tokenCh <- &tokenResult{
			accessToken:  accessToken,
			refreshToken: refreshToken,
			relayURL:     relayURL,
		}

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

	// 2. Open browser.
	loginURL := fmt.Sprintf("%s/auth/cli/google?mode=http&port=%d", apiURL, port)
	fmt.Printf("Opening browser for login...\n")
	fmt.Printf("If the browser doesn't open, visit:\n  %s\n\n", loginURL)

	if err := core.OpenBrowser(loginURL); err != nil {
		fmt.Printf("Could not open browser: %v\n", err)
	}

	// 3. Wait for callback with timeout.
	fmt.Printf("Waiting for authentication (5 minute timeout)...\n")
	select {
	case result := <-tokenCh:
		if result.err != nil {
			return nil, result.err
		}
		if err := mgr.SaveTokens(apiURL, result.accessToken, result.refreshToken); err != nil {
			return nil, fmt.Errorf("save tokens: %w", err)
		}
		if result.relayURL != "" {
			if err := mgr.SaveRelayURL(apiURL, result.relayURL); err != nil {
				return nil, fmt.Errorf("save relay URL: %w", err)
			}
		}
		if err := verifyAndPrint(apiURL, result.accessToken); err != nil {
			return nil, err
		}
		return &loginResult{RelayURL: result.relayURL}, nil

	case <-time.After(5 * time.Minute):
		return nil, fmt.Errorf("login timed out (5 minutes)")
	}
}

func loginCode(apiURL string, mgr *core.APITokenManager) (*loginResult, error) {
	loginURL := fmt.Sprintf("%s/auth/cli/google?mode=code", apiURL)
	fmt.Printf("Open this URL in your browser:\n\n  %s\n\n", loginURL)
	fmt.Printf("Enter the code from the browser: ")

	var code string
	if _, err := fmt.Scanln(&code); err != nil {
		return nil, fmt.Errorf("read code: %w", err)
	}
	code = strings.TrimSpace(code)
	if code == "" {
		return nil, fmt.Errorf("empty code")
	}

	// Exchange code for tokens.
	payload, _ := json.Marshal(map[string]string{"code": code})
	resp, err := http.Post(
		apiURL+"/auth/cli/exchange",
		"application/json",
		strings.NewReader(string(payload)),
	)
	if err != nil {
		return nil, fmt.Errorf("exchange request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("exchange failed (%d): %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}

	var result struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		RelayURL     string `json:"relay_url"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	if err := mgr.SaveTokens(apiURL, result.AccessToken, result.RefreshToken); err != nil {
		return nil, fmt.Errorf("save tokens: %w", err)
	}
	if result.RelayURL != "" {
		if err := mgr.SaveRelayURL(apiURL, result.RelayURL); err != nil {
			return nil, fmt.Errorf("save relay URL: %w", err)
		}
	}
	if err := verifyAndPrint(apiURL, result.AccessToken); err != nil {
		return nil, err
	}
	return &loginResult{RelayURL: result.RelayURL}, nil
}

func verifyAndPrint(apiURL, accessToken string) error {
	req, err := http.NewRequest("GET", apiURL+"/auth/me", nil)
	if err != nil {
		fmt.Printf("Login successful.\n")
		return nil
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		fmt.Printf("Login successful (could not verify: %v)\n", err)
		return nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		fmt.Printf("Login successful.\n")
		return nil
	}

	var user struct {
		Email string `json:"email"`
		Name  string `json:"name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&user); err != nil {
		fmt.Printf("Login successful.\n")
		return nil
	}

	fmt.Printf("Logged in as %s (%s)\n", user.Name, user.Email)
	return nil
}

type tokenResult struct {
	accessToken  string
	refreshToken string
	relayURL     string
	err          error
}

// loginResult is returned to callers (e.g. the setup wizard) so they can
// run follow-up flows that need the relay server's base URL.
type loginResult struct {
	RelayURL string
}

const loginSuccessHTML = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8"><title>KittyPaw Login</title>
<style>
  body { font-family: -apple-system, sans-serif; display: flex; justify-content: center; align-items: center; min-height: 100vh; margin: 0; background: #f5f5f7; }
  .card { background: white; border-radius: 12px; padding: 48px; text-align: center; box-shadow: 0 4px 24px rgba(0,0,0,0.1); }
</style>
</head>
<body>
<div class="card">
  <h2>Login complete</h2>
  <p>You can close this tab and return to the terminal.</p>
</div>
</body>
</html>`
