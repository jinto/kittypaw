package chatrelay

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"nhooyr.io/websocket"
)

type ConnectorConfig struct {
	RelayURL      string
	Credential    string
	DeviceID      string
	LocalAccounts []string
	DaemonVersion string
	Capabilities  []string
}

type Connector struct {
	Config ConnectorConfig
}

type RunOptions struct {
	RetryInitialDelay time.Duration
	RetryMaxDelay     time.Duration
	Logf              func(format string, args ...any)
}

func BuildDaemonConnectURL(base string) (string, error) {
	base = strings.TrimSpace(base)
	if base == "" {
		return "", fmt.Errorf("chat relay url is required")
	}
	u, err := url.Parse(base)
	if err != nil {
		return "", fmt.Errorf("parse chat relay url: %w", err)
	}
	if u.Scheme == "" || u.Host == "" {
		return "", fmt.Errorf("chat relay url must include scheme and host")
	}
	switch u.Scheme {
	case "https":
		u.Scheme = "wss"
	case "http":
		u.Scheme = "ws"
	case "ws", "wss":
	default:
		return "", fmt.Errorf("unsupported chat relay url scheme %q", u.Scheme)
	}
	basePath := strings.TrimRight(u.Path, "/")
	if basePath == "" {
		u.Path = "/daemon/connect"
	} else {
		u.Path = basePath + "/daemon/connect"
	}
	u.RawQuery = ""
	u.Fragment = ""
	return u.String(), nil
}

func (c *Connector) DialAndSendHello(ctx context.Context) (*websocket.Conn, error) {
	cfg := c.Config
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	endpoint, err := BuildDaemonConnectURL(cfg.RelayURL)
	if err != nil {
		return nil, err
	}

	headers := http.Header{}
	headers.Set("Authorization", "Bearer "+cfg.Credential)
	conn, resp, err := websocket.Dial(ctx, endpoint, &websocket.DialOptions{
		HTTPHeader: headers,
	})
	if err != nil {
		if resp != nil {
			return nil, fmt.Errorf("chat relay dial failed (%s): %w", resp.Status, err)
		}
		return nil, fmt.Errorf("chat relay dial: %w", err)
	}

	hello := NewHelloFrame(cfg.DeviceID, cfg.LocalAccounts, cfg.DaemonVersion, cfg.Capabilities)
	raw, err := json.Marshal(hello)
	if err != nil {
		conn.CloseNow()
		return nil, fmt.Errorf("marshal hello: %w", err)
	}
	if err := conn.Write(ctx, websocket.MessageText, raw); err != nil {
		conn.CloseNow()
		return nil, fmt.Errorf("write hello: %w", err)
	}
	return conn, nil
}

func (c *Connector) Run(ctx context.Context, opts RunOptions) {
	delay := opts.RetryInitialDelay
	if delay <= 0 {
		delay = time.Second
	}
	maxDelay := opts.RetryMaxDelay
	if maxDelay <= 0 {
		maxDelay = 30 * time.Second
	}
	for {
		if ctx.Err() != nil {
			return
		}

		conn, err := c.DialAndSendHello(ctx)
		if err == nil {
			delay = opts.RetryInitialDelay
			if delay <= 0 {
				delay = time.Second
			}
			readErr := c.readLoop(ctx, conn)
			conn.CloseNow()
			if ctx.Err() != nil {
				return
			}
			if readErr != nil && opts.Logf != nil {
				opts.Logf("chat relay disconnected: %v", readErr)
			}
		} else if opts.Logf != nil {
			opts.Logf("chat relay connect failed: %v", err)
		}

		if !sleepContext(ctx, delay) {
			return
		}
		delay *= 2
		if delay > maxDelay {
			delay = maxDelay
		}
	}
}

func (c *Connector) readLoop(ctx context.Context, conn *websocket.Conn) error {
	for {
		typ, data, err := conn.Read(ctx)
		if err != nil {
			return err
		}
		if typ != websocket.MessageText {
			if err := writeError(ctx, conn, "", "unsupported_frame", "chat relay frames must be text JSON"); err != nil {
				return err
			}
			continue
		}
		if err := c.handleFrame(ctx, conn, data); err != nil {
			return err
		}
	}
}

func (c *Connector) handleFrame(ctx context.Context, conn *websocket.Conn, data []byte) error {
	var envelope struct {
		Type      string `json:"type"`
		ID        string `json:"id"`
		Operation string `json:"operation"`
		AccountID string `json:"account_id"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		return writeError(ctx, conn, "", "bad_frame", "invalid chat relay frame JSON")
	}
	if envelope.Type != FrameRequest {
		return writeError(ctx, conn, envelope.ID, "unsupported_frame", "unsupported chat relay frame type")
	}
	if !containsString(c.Config.LocalAccounts, envelope.AccountID) {
		return writeError(ctx, conn, envelope.ID, "unknown_account", "account is not active on this daemon connection")
	}
	if !SupportedOperation(envelope.Operation) {
		return writeError(ctx, conn, envelope.ID, "unsupported_operation", "unsupported chat relay operation")
	}
	if !containsString(EffectiveCapabilities(c.Config.Capabilities), envelope.Operation) {
		return writeError(ctx, conn, envelope.ID, "unsupported_capability", "operation was not advertised by this daemon connection")
	}
	return writeError(ctx, conn, envelope.ID, "not_implemented", "chat relay operation dispatch is not implemented")
}

func writeError(ctx context.Context, conn *websocket.Conn, id, code, message string) error {
	raw, err := json.Marshal(ErrorFrame{
		Type:    FrameError,
		ID:      id,
		Code:    code,
		Message: message,
	})
	if err != nil {
		return err
	}
	return conn.Write(ctx, websocket.MessageText, raw)
}

func (cfg ConnectorConfig) validate() error {
	if strings.TrimSpace(cfg.RelayURL) == "" {
		return fmt.Errorf("chat relay url is required")
	}
	if strings.TrimSpace(cfg.Credential) == "" {
		return fmt.Errorf("chat relay credential is required")
	}
	if strings.TrimSpace(cfg.DeviceID) == "" {
		return fmt.Errorf("chat relay device id is required")
	}
	if len(cfg.LocalAccounts) == 0 {
		return fmt.Errorf("chat relay local accounts are required")
	}
	return nil
}

func containsString(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}

func sleepContext(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
