package browser

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"github.com/jinto/kittypaw/core"
	"nhooyr.io/websocket"
)

type StatusResult struct {
	Enabled        bool     `json:"enabled"`
	Running        bool     `json:"running"`
	Managed        bool     `json:"managed"`
	ChromePath     string   `json:"chrome_path,omitempty"`
	CandidatePaths []string `json:"candidate_paths,omitempty"`
	Browser        string   `json:"browser,omitempty"`
	ActiveTargetID string   `json:"active_target_id,omitempty"`
	LastError      string   `json:"last_error,omitempty"`
}

type tabInfo struct {
	TargetID string `json:"target_id"`
	URL      string `json:"url"`
	Title    string `json:"title"`
	Active   bool   `json:"active"`
}

type backend interface {
	status(context.Context) (StatusResult, error)
	open(context.Context, string) (tabInfo, error)
	tabs(context.Context) ([]tabInfo, error)
	use(context.Context, string) (tabInfo, error)
	navigate(context.Context, string) (map[string]any, error)
	close(context.Context, string) error
}

type Controller struct {
	cfg       core.BrowserConfig
	baseDir   string
	dataDir   string
	backend   backend
	lastError string
	startMu   sync.Mutex
	mu        sync.Mutex

	chromePath     string
	candidatePaths []string
	proc           *chromeProcess
	client         *cdpClient
	browserVersion string
	activeTargetID string
	targets        map[string]string
}

func NewController(opts ControllerOptions) *Controller {
	c := &Controller{
		cfg:     opts.Config,
		baseDir: opts.BaseDir,
		dataDir: filepath.Join(opts.BaseDir, "data", "browser"),
	}
	c.backend = c
	return c
}

func newControllerWithBackend(cfg core.BrowserConfig, baseDir string, b backend) *Controller {
	return &Controller{
		cfg:     cfg,
		baseDir: baseDir,
		dataDir: filepath.Join(baseDir, "data", "browser"),
		backend: b,
	}
}

func (c *Controller) Execute(ctx context.Context, call core.SkillCall) (string, error) {
	if call.SkillName != "Browser" {
		return errorResult("invalid browser skill")
	}
	if call.Method == "status" {
		return c.executeStatus(ctx)
	}
	if !c.cfg.Enabled {
		return errorResult("browser disabled")
	}
	timeout := time.Duration(c.cfg.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = defaultStartupTimeout
	}
	callCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	switch call.Method {
	case "open":
		rawURL, err := optionalStringArg(call.Args, 0)
		if err != nil {
			return errorResult(err.Error())
		}
		if rawURL != "" {
			var validErr error
			rawURL, validErr = validateNavigationURL(rawURL, c.cfg.AllowedHosts)
			if validErr != nil {
				return errorResult(validErr.Error())
			}
		}
		tab, err := c.backend.open(callCtx, rawURL)
		return c.resultOrError(tab, err)
	case "tabs":
		tabs, err := c.backend.tabs(callCtx)
		return c.resultOrError(map[string]any{"tabs": tabs}, err)
	case "use":
		targetID, err := requiredStringArg(call.Args, 0, "targetId argument required")
		if err != nil {
			return errorResult(err.Error())
		}
		tab, err := c.backend.use(callCtx, targetID)
		return c.resultOrError(tab, err)
	case "navigate":
		rawURL, err := requiredStringArg(call.Args, 0, "url argument required")
		if err != nil {
			return errorResult(err.Error())
		}
		rawURL, err = validateNavigationURL(rawURL, c.cfg.AllowedHosts)
		if err != nil {
			return errorResult(err.Error())
		}
		out, err := c.backend.navigate(callCtx, rawURL)
		return c.resultOrError(out, err)
	case "close":
		targetID, err := optionalStringArg(call.Args, 0)
		if err != nil {
			return errorResult(err.Error())
		}
		err = c.backend.close(callCtx, targetID)
		return c.resultOrError(map[string]any{"success": err == nil}, err)
	default:
		return errorResult(fmt.Sprintf("unknown Browser method: %s", call.Method))
	}
}

func (c *Controller) executeStatus(ctx context.Context) (string, error) {
	if !c.cfg.Enabled {
		return jsonResult(StatusResult{Enabled: false, LastError: c.lastError})
	}
	status, err := c.backend.status(ctx)
	if err != nil {
		c.recordError(err)
		status = StatusResult{Enabled: true, LastError: err.Error()}
	}
	return jsonResult(status)
}

func (c *Controller) resultOrError(v any, err error) (string, error) {
	if err != nil {
		c.recordError(err)
		return errorResult(err.Error())
	}
	return jsonResult(v)
}

func (c *Controller) recordError(err error) {
	if err == nil {
		return
	}
	c.mu.Lock()
	c.lastError = err.Error()
	c.mu.Unlock()
}

func optionalStringArg(args []json.RawMessage, idx int) (string, error) {
	if len(args) <= idx {
		return "", nil
	}
	var out string
	if err := json.Unmarshal(args[idx], &out); err != nil {
		return "", fmt.Errorf("invalid string argument")
	}
	return out, nil
}

func requiredStringArg(args []json.RawMessage, idx int, msg string) (string, error) {
	out, err := optionalStringArg(args, idx)
	if err != nil {
		return "", err
	}
	if out == "" {
		return "", fmt.Errorf("%s", msg)
	}
	return out, nil
}

func (c *Controller) status(context.Context) (StatusResult, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return StatusResult{
		Enabled:        c.cfg.Enabled,
		Running:        c.client != nil,
		Managed:        true,
		ChromePath:     c.chromePath,
		CandidatePaths: append([]string(nil), c.candidatePaths...),
		Browser:        c.browserVersion,
		ActiveTargetID: c.activeTargetID,
		LastError:      c.lastError,
	}, nil
}

func (c *Controller) open(ctx context.Context, rawURL string) (tabInfo, error) {
	if err := c.ensureStarted(ctx); err != nil {
		return tabInfo{}, err
	}
	targetURL := rawURL
	if targetURL == "" {
		targetURL = "about:blank"
	}
	client := c.getClient()
	var created struct {
		TargetID string `json:"targetId"`
	}
	if err := client.Call(ctx, "Target.createTarget", map[string]any{"url": targetURL}, &created); err != nil {
		return tabInfo{}, err
	}
	var attached struct {
		SessionID string `json:"sessionId"`
	}
	if err := client.Call(ctx, "Target.attachToTarget", map[string]any{
		"targetId": created.TargetID,
		"flatten":  true,
	}, &attached); err != nil {
		return tabInfo{}, err
	}
	if err := client.Call(ctx, "Target.activateTarget", map[string]any{"targetId": created.TargetID}, nil); err != nil {
		return tabInfo{}, err
	}
	c.mu.Lock()
	if c.targets == nil {
		c.targets = make(map[string]string)
	}
	c.targets[created.TargetID] = attached.SessionID
	c.activeTargetID = created.TargetID
	c.mu.Unlock()
	return c.currentTabInfo(ctx, created.TargetID)
}

func (c *Controller) tabs(ctx context.Context) ([]tabInfo, error) {
	if err := c.ensureStarted(ctx); err != nil {
		return nil, err
	}
	infos, err := c.targetInfos(ctx)
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	active := c.activeTargetID
	c.mu.Unlock()
	tabs := make([]tabInfo, 0, len(infos))
	for _, info := range infos {
		if info.Type != "page" {
			continue
		}
		tabs = append(tabs, tabInfo{
			TargetID: info.TargetID,
			URL:      info.URL,
			Title:    info.Title,
			Active:   info.TargetID == active,
		})
	}
	return tabs, nil
}

func (c *Controller) use(ctx context.Context, targetID string) (tabInfo, error) {
	if err := c.ensureStarted(ctx); err != nil {
		return tabInfo{}, err
	}
	client := c.getClient()
	if err := client.Call(ctx, "Target.activateTarget", map[string]any{"targetId": targetID}, nil); err != nil {
		return tabInfo{}, err
	}
	if _, err := c.sessionForTarget(ctx, targetID); err != nil {
		return tabInfo{}, err
	}
	c.mu.Lock()
	c.activeTargetID = targetID
	c.mu.Unlock()
	return c.currentTabInfo(ctx, targetID)
}

func (c *Controller) navigate(ctx context.Context, rawURL string) (map[string]any, error) {
	if err := c.ensureStarted(ctx); err != nil {
		return nil, err
	}
	sessionID, err := c.sessionForTarget(ctx, "")
	if err != nil {
		return nil, err
	}
	out := map[string]any{}
	if err := c.getClient().CallSession(ctx, sessionID, "Page.navigate", map[string]any{"url": rawURL}, &out); err != nil {
		return nil, err
	}
	out["url"] = rawURL
	return out, nil
}

func (c *Controller) close(ctx context.Context, targetID string) error {
	if err := c.ensureStarted(ctx); err != nil {
		return err
	}
	if targetID == "" {
		c.mu.Lock()
		targetID = c.activeTargetID
		c.mu.Unlock()
	}
	if targetID == "" {
		return fmt.Errorf("no active tab")
	}
	if err := c.getClient().Call(ctx, "Target.closeTarget", map[string]any{"targetId": targetID}, nil); err != nil {
		return err
	}
	c.mu.Lock()
	delete(c.targets, targetID)
	if c.activeTargetID == targetID {
		c.activeTargetID = ""
	}
	c.mu.Unlock()
	return nil
}

func (c *Controller) Close() error {
	c.mu.Lock()
	client := c.client
	proc := c.proc
	c.client = nil
	c.proc = nil
	c.activeTargetID = ""
	c.targets = nil
	c.mu.Unlock()

	var firstErr error
	if client != nil {
		if err := client.Close(); err != nil {
			firstErr = err
		}
	}
	if proc != nil {
		if err := proc.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func (c *Controller) ensureStarted(ctx context.Context) error {
	c.startMu.Lock()
	defer c.startMu.Unlock()

	c.mu.Lock()
	if c.client != nil {
		c.mu.Unlock()
		return nil
	}
	c.mu.Unlock()

	profileDir := filepath.Join(c.dataDir, "profile")
	if err := os.MkdirAll(profileDir, 0o700); err != nil {
		return err
	}
	chromePath, candidates, err := findChrome(c.cfg.ChromePath)
	c.mu.Lock()
	c.chromePath = chromePath
	c.candidatePaths = append([]string(nil), candidates...)
	c.mu.Unlock()
	if err != nil {
		return err
	}

	cmd := exec.Command(chromePath, buildChromeArgs(profileDir, c.cfg.Headless)...)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("browser launch failed: %w", err)
	}
	proc := &chromeProcess{cmd: cmd}
	port, browserPath, err := waitForDevToolsActivePort(ctx, filepath.Join(profileDir, "DevToolsActivePort"))
	if err != nil {
		_ = proc.Close()
		return err
	}

	version, err := fetchBrowserVersion(ctx, port)
	if err != nil {
		_ = proc.Close()
		return err
	}
	if err := validateBrowserWebSocketURL(version.WebSocketDebuggerURL, port, browserPath); err != nil {
		_ = proc.Close()
		return err
	}
	ws, _, err := websocket.Dial(ctx, version.WebSocketDebuggerURL, nil)
	if err != nil {
		_ = proc.Close()
		return err
	}

	c.mu.Lock()
	c.proc = proc
	c.client = newCDPClient(&websocketConn{conn: ws})
	c.browserVersion = version.Browser
	c.targets = make(map[string]string)
	c.mu.Unlock()
	return nil
}

type browserVersionResponse struct {
	Browser              string `json:"Browser"`
	WebSocketDebuggerURL string `json:"webSocketDebuggerUrl"`
}

func fetchBrowserVersion(ctx context.Context, port string) (browserVersionResponse, error) {
	versionURL := "http://127.0.0.1:" + port + "/json/version"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, versionURL, nil)
	if err != nil {
		return browserVersionResponse{}, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return browserVersionResponse{}, err
	}
	defer resp.Body.Close()
	var version browserVersionResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 64*1024)).Decode(&version); err != nil {
		return browserVersionResponse{}, err
	}
	if version.WebSocketDebuggerURL == "" {
		return browserVersionResponse{}, fmt.Errorf("browser websocket URL missing")
	}
	return version, nil
}

func validateBrowserWebSocketURL(rawURL, port, browserPath string) error {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("browser websocket URL invalid: %w", err)
	}
	if parsed.Scheme != "ws" {
		return fmt.Errorf("browser websocket URL must use ws")
	}
	if parsed.Host != "127.0.0.1:"+port {
		return fmt.Errorf("browser websocket URL must use managed loopback address")
	}
	if parsed.Path != browserPath {
		return fmt.Errorf("browser websocket URL path mismatch")
	}
	return nil
}

func (c *Controller) getClient() *cdpClient {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.client
}

type targetInfo struct {
	TargetID string `json:"targetId"`
	Type     string `json:"type"`
	URL      string `json:"url"`
	Title    string `json:"title"`
}

func (c *Controller) targetInfos(ctx context.Context) ([]targetInfo, error) {
	var out struct {
		TargetInfos []targetInfo `json:"targetInfos"`
	}
	if err := c.getClient().Call(ctx, "Target.getTargets", nil, &out); err != nil {
		return nil, err
	}
	return out.TargetInfos, nil
}

func (c *Controller) currentTabInfo(ctx context.Context, targetID string) (tabInfo, error) {
	infos, err := c.targetInfos(ctx)
	if err != nil {
		return tabInfo{}, err
	}
	c.mu.Lock()
	active := c.activeTargetID
	c.mu.Unlock()
	for _, info := range infos {
		if info.TargetID == targetID {
			return tabInfo{
				TargetID: info.TargetID,
				URL:      info.URL,
				Title:    info.Title,
				Active:   info.TargetID == active,
			}, nil
		}
	}
	return tabInfo{}, fmt.Errorf("target not found")
}

func (c *Controller) sessionForTarget(ctx context.Context, targetID string) (string, error) {
	c.mu.Lock()
	if targetID == "" {
		targetID = c.activeTargetID
	}
	if targetID == "" {
		c.mu.Unlock()
		return "", fmt.Errorf("no active tab")
	}
	if sessionID := c.targets[targetID]; sessionID != "" {
		c.mu.Unlock()
		return sessionID, nil
	}
	c.mu.Unlock()

	var attached struct {
		SessionID string `json:"sessionId"`
	}
	if err := c.getClient().Call(ctx, "Target.attachToTarget", map[string]any{
		"targetId": targetID,
		"flatten":  true,
	}, &attached); err != nil {
		return "", err
	}
	c.mu.Lock()
	if c.targets == nil {
		c.targets = make(map[string]string)
	}
	c.targets[targetID] = attached.SessionID
	c.mu.Unlock()
	return attached.SessionID, nil
}
