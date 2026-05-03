package browser

import (
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

type ScreenshotResult struct {
	Path  string `json:"path"`
	Mime  string `json:"mime"`
	Bytes int    `json:"bytes"`
}

func (c *Controller) writeScreenshot(encoded, format string) (ScreenshotResult, error) {
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return ScreenshotResult{}, err
	}
	if format == "" {
		format = "png"
	}
	dir := filepath.Join(c.dataDir, "screenshots")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return ScreenshotResult{}, err
	}
	path := filepath.Join(dir, fmt.Sprintf("shot-%s.%s", time.Now().Format("20060102-150405"), format))
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return ScreenshotResult{}, err
	}
	return ScreenshotResult{Path: path, Mime: "image/" + format, Bytes: len(data)}, nil
}
