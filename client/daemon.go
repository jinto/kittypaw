package client

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/jinto/kittypaw/core"
)

// DaemonConn manages daemon discovery and auto-start.
type DaemonConn struct {
	BaseURL  string
	APIKey   string
	bindAddr string
}

// NewDaemonConn creates a DaemonConn. If remoteURL is non-empty, connects directly
// to that URL. Otherwise reads config.toml for server.bind and server.api_key.
func NewDaemonConn(remoteURL string) (*DaemonConn, error) {
	if remoteURL != "" {
		return &DaemonConn{BaseURL: remoteURL}, nil
	}

	cfgPath, err := core.ConfigPath()
	if err != nil {
		return nil, fmt.Errorf("config path: %w", err)
	}
	cfg, err := core.LoadConfig(cfgPath)
	if err != nil {
		return nil, fmt.Errorf("load config: %w", err)
	}

	bind := cfg.Server.BindOrDefault()
	host, port := parseBindAddr(bind)
	baseURL := fmt.Sprintf("http://%s:%s", host, port)

	return &DaemonConn{
		BaseURL:  baseURL,
		APIKey:   cfg.Server.APIKey,
		bindAddr: bind,
	}, nil
}

// Connect returns a Client connected to a running daemon.
// If no daemon is running, auto-starts one and polls until healthy.
func (d *DaemonConn) Connect() (*Client, error) {
	cl := New(d.BaseURL, d.APIKey)

	pidPath, err := daemonPidPath()
	if err != nil {
		return nil, err
	}

	// Try existing daemon first.
	if pid, ok := readPid(pidPath); ok {
		if isKittypawProcess(pid) {
			if cl.Health() == nil {
				return cl, nil
			}
			// Process alive but not healthy yet — poll (don't delete PID).
			if err := d.pollHealth(cl); err != nil {
				return nil, err
			}
			return cl, nil
		}
		// Process dead — stale PID, clean up.
		os.Remove(pidPath)
	}

	// No daemon running — try to start one.
	if err := d.spawnDaemon(pidPath); err != nil {
		return nil, err
	}

	// Poll until healthy.
	if err := d.pollHealth(cl); err != nil {
		return nil, err
	}
	return cl, nil
}

// IsRunning checks if a daemon is already running (without starting one).
func (d *DaemonConn) IsRunning() bool {
	pidPath, err := daemonPidPath()
	if err != nil {
		return false
	}
	pid, ok := readPid(pidPath)
	return ok && isKittypawProcess(pid)
}

// WebSocketURL returns the ws:// or wss:// URL for streaming chat.
func (d *DaemonConn) WebSocketURL() string {
	url := d.BaseURL
	if strings.HasPrefix(url, "https://") {
		url = "wss://" + url[len("https://"):]
	} else if strings.HasPrefix(url, "http://") {
		url = "ws://" + url[len("http://"):]
	}
	return url + "/ws"
}

func (d *DaemonConn) spawnDaemon(pidPath string) error {
	lockPath := pidPath + ".lock"
	lockFile, err := lockPidFile(lockPath)
	if err != nil {
		// Another process is starting the daemon — fall through to health polling.
		fmt.Fprintln(os.Stderr, "daemon이 시작 중입니다. 대기합니다...")
		return nil
	}
	defer unlockPidFile(lockFile)

	// Double-check after acquiring lock — daemon may have appeared.
	if pid, ok := readPid(pidPath); ok && isKittypawProcess(pid) {
		return nil
	}

	fmt.Fprintln(os.Stderr, "daemon이 실행 중이 아닙니다. 시작 중...")

	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("find executable: %w", err)
	}

	args := []string{"serve"}
	if d.bindAddr != "" {
		args = append(args, "--bind", d.bindAddr)
	}

	proc := exec.Command(exe, args...)
	logPath := filepath.Join(filepath.Dir(pidPath), "daemon.log")
	// Truncate log if it exceeds 10 MB to prevent unbounded growth.
	if fi, err := os.Stat(logPath); err == nil && fi.Size() > 10<<20 {
		_ = os.Truncate(logPath, 0)
	}
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err == nil {
		proc.Stdout = logFile
		proc.Stderr = logFile
		defer logFile.Close() // safe: child inherits FD via Start(), parent closes on return
	}
	setSysProcAttr(proc)

	if err := proc.Start(); err != nil {
		return fmt.Errorf("daemon 시작 실패: %w", err)
	}

	if err := os.WriteFile(pidPath, []byte(strconv.Itoa(proc.Process.Pid)), 0o600); err != nil {
		return fmt.Errorf("PID 파일 기록 실패: %w", err)
	}
	return nil
}

const (
	healthPollInterval = 200 * time.Millisecond
	healthPollMaxTries = 50 // 200ms × 50 = 10s
)

func (d *DaemonConn) pollHealth(cl *Client) error {
	for i := 0; i < healthPollMaxTries; i++ {
		if cl.Health() == nil {
			return nil
		}
		time.Sleep(healthPollInterval)
	}
	return fmt.Errorf("daemon 시작 타임아웃 (10초). `kittypaw daemon start`로 직접 시작하세요")
}

// lockPidFile and unlockPidFile are in proc_{unix,windows}.go

func daemonPidPath() (string, error) {
	dir, err := core.ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "daemon.pid"), nil
}

func readPid(path string) (int, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, false
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return 0, false
	}
	return pid, true
}

// isKittypawProcess checks if a PID belongs to a running kittypaw process.
// isKittypawProcess is in proc_{unix,windows}.go

func parseBindAddr(bind string) (host, port string) {
	if idx := strings.LastIndex(bind, ":"); idx >= 0 {
		host = bind[:idx]
		port = bind[idx+1:]
	}
	if host == "" || host == "0.0.0.0" {
		host = "localhost"
	}
	if port == "" {
		port = "3000"
	}
	return
}
