package browser

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

type chromeProcess struct {
	cmd *exec.Cmd
}

func (p *chromeProcess) Close() error {
	if p == nil || p.cmd == nil || p.cmd.Process == nil {
		return nil
	}
	_ = p.cmd.Process.Signal(os.Interrupt)
	done := make(chan error, 1)
	go func() { done <- p.cmd.Wait() }()
	select {
	case err := <-done:
		return err
	case <-time.After(2 * time.Second):
		_ = p.cmd.Process.Kill()
		return <-done
	}
}

func defaultChromeCandidates() []string {
	switch runtime.GOOS {
	case "darwin":
		return []string{
			"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
			"/Applications/Chromium.app/Contents/MacOS/Chromium",
		}
	case "windows":
		return []string{
			filepath.Join(os.Getenv("PROGRAMFILES"), "Google", "Chrome", "Application", "chrome.exe"),
			filepath.Join(os.Getenv("PROGRAMFILES(X86)"), "Google", "Chrome", "Application", "chrome.exe"),
			filepath.Join(os.Getenv("LOCALAPPDATA"), "Google", "Chrome", "Application", "chrome.exe"),
		}
	default:
		return []string{"google-chrome", "google-chrome-stable", "chromium", "chromium-browser"}
	}
}

func findChrome(explicit string) (string, []string, error) {
	candidates := defaultChromeCandidates()
	if explicit != "" {
		candidates = append([]string{explicit}, candidates...)
	}
	for _, c := range candidates {
		if c == "" {
			continue
		}
		if filepath.IsAbs(c) {
			if info, err := os.Stat(c); err == nil && !info.IsDir() {
				return c, candidates, nil
			}
			continue
		}
		if path, err := exec.LookPath(c); err == nil {
			return path, candidates, nil
		}
	}
	return "", candidates, fmt.Errorf("chrome executable not found")
}

func buildChromeArgs(profileDir string, headless bool) []string {
	args := []string{
		"--remote-debugging-port=0",
		"--remote-debugging-address=127.0.0.1",
		"--user-data-dir=" + profileDir,
		"--no-first-run",
		"--no-default-browser-check",
	}
	if headless {
		args = append(args, "--headless=new")
	}
	return append(args, "about:blank")
}

func parseDevToolsActivePort(path string) (string, string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", "", err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	if !sc.Scan() {
		return "", "", fmt.Errorf("DevToolsActivePort missing port")
	}
	port := strings.TrimSpace(sc.Text())
	if !sc.Scan() {
		return "", "", fmt.Errorf("DevToolsActivePort missing browser path")
	}
	browserPath := strings.TrimSpace(sc.Text())
	if err := sc.Err(); err != nil {
		return "", "", err
	}
	if port == "" || browserPath == "" {
		return "", "", fmt.Errorf("DevToolsActivePort incomplete")
	}
	return port, browserPath, nil
}

func waitForDevToolsActivePort(ctx context.Context, path string) (string, string, error) {
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		port, browserPath, err := parseDevToolsActivePort(path)
		if err == nil {
			return port, browserPath, nil
		}
		select {
		case <-ctx.Done():
			return "", "", fmt.Errorf("browser launch timed out")
		case <-ticker.C:
		}
	}
}
