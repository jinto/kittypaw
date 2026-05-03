package browser

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sync"
	"time"

	"github.com/jinto/kittypaw/core"
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
	mu        sync.Mutex
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
	return StatusResult{Enabled: c.cfg.Enabled, Managed: true, LastError: c.lastError}, nil
}

func (c *Controller) open(context.Context, string) (tabInfo, error) {
	return tabInfo{}, fmt.Errorf("browser backend not started")
}

func (c *Controller) tabs(context.Context) ([]tabInfo, error) {
	return nil, fmt.Errorf("browser backend not started")
}

func (c *Controller) use(context.Context, string) (tabInfo, error) {
	return tabInfo{}, fmt.Errorf("browser backend not started")
}

func (c *Controller) navigate(context.Context, string) (map[string]any, error) {
	return nil, fmt.Errorf("browser backend not started")
}

func (c *Controller) close(context.Context, string) error {
	return fmt.Errorf("browser backend not started")
}

func (c *Controller) Close() error { return nil }
