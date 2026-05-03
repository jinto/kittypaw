package browser

import (
	"encoding/json"
	"time"

	"github.com/jinto/kittypaw/core"
)

const (
	defaultTextLimit      = 12000
	defaultElementsLimit  = 80
	defaultEvaluateLimit  = 8000
	defaultTypeTextLimit  = 4000
	defaultStartupTimeout = 15 * time.Second
)

type ControllerOptions struct {
	Config  core.BrowserConfig
	BaseDir string
}

func jsonResult(v any) (string, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func errorResult(msg string) (string, error) {
	return jsonResult(map[string]any{"error": msg})
}
