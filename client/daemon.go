package client

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jinto/kittypaw/core"
)

// DaemonConn manages daemon discovery and auto-start.
type DaemonConn struct {
	BaseURL   string
	APIKey    string
	AccountID string
	bindAddr  string
}

// NewDaemonConn creates a DaemonConn. If remoteURL is non-empty, connects
// directly to that URL. Otherwise resolves the daemon's bind address and
// API key from disk using a three-tier fallback — see resolveDaemonEndpoint
// for why three tiers are needed.
func NewDaemonConn(remoteURL string) (*DaemonConn, error) {
	return NewDaemonConnForAccount(remoteURL, "")
}

// NewDaemonConnForAccount creates a DaemonConn pinned to a local account when
// accountID is non-empty. For local chat this matters: WebSocket auth maps
// per-account API keys to per-account sessions, while a server-wide master key
// cannot disambiguate multiple active accounts.
func NewDaemonConnForAccount(remoteURL, accountID string) (*DaemonConn, error) {
	if accountID != "" {
		if err := core.ValidateAccountID(accountID); err != nil {
			return nil, err
		}
	}
	if remoteURL != "" {
		return &DaemonConn{BaseURL: remoteURL, AccountID: accountID}, nil
	}

	bind, apiKey, resolvedAccountID, err := resolveDaemonEndpointForAccount(accountID)
	if err != nil {
		return nil, err
	}
	host, port := parseBindAddr(bind)
	return &DaemonConn{
		BaseURL:   fmt.Sprintf("http://%s:%s", host, port),
		APIKey:    apiKey,
		AccountID: resolvedAccountID,
		bindAddr:  bind,
	}, nil
}

// resolveDaemonEndpoint finds daemon Bind + API key across the three
// layouts kittypaw can be in at any moment:
//
//  1. ~/.kittypaw/server.toml       — the designed-for-server-wide path
//     (CLAUDE.md). A complete file wins outright; a partial file supplies
//     only the fields it has, with missing Bind/API key filled from the
//     selected account config below. default_account is also honored here so
//     client discovery and server bootstrap pick the same account.
//  2. ~/.kittypaw/accounts/<id>/config.toml — the named-account layout.
//     Exactly one account is unambiguous. Multiple accounts require
//     server.toml because there is no safe client-side daemon endpoint to
//     infer from per-account configs.
//  3. ~/.kittypaw/config.toml       — the legacy / pre-migration path.
//     Used only when no accounts are present.
//
// This mirrors the read-side of the designed multi-account contract while
// leaving the write-side unchanged: whoever ends up implementing
// WriteServerConfigAtomic later flips tier 1 on with zero client edits.
func resolveDaemonEndpointForAccount(accountID string) (bind, apiKey, resolvedAccountID string, err error) {
	var tried []string
	var serverCfg *core.TopLevelServerConfig

	if scPath, perr := core.ServerConfigPath(); perr == nil {
		tried = append(tried, scPath)
		if sc, lerr := core.LoadServerConfig(scPath); lerr == nil {
			serverCfg = sc
		}
	}

	dir, derr := core.ConfigDir()
	if derr != nil {
		return "", "", "", fmt.Errorf("config dir: %w", derr)
	}

	accountsRoot := filepath.Join(dir, "accounts")
	tried = append(tried, accountsRoot)
	accounts, aerr := core.DiscoverAccounts(accountsRoot)
	if aerr != nil {
		return "", "", "", fmt.Errorf("discover accounts: %w", aerr)
	}
	if accountID != "" {
		account, err := findDaemonEndpointAccount(accounts, accountID)
		if err != nil {
			return "", "", "", err
		}
		bind := account.Config.Server.BindOrDefault()
		if serverCfg != nil && serverCfg.Bind != "" {
			bind = serverCfg.Bind
		}
		apiKey := account.Config.Server.APIKey
		if apiKey == "" && serverCfg != nil {
			apiKey = serverCfg.MasterAPIKey
		}
		if apiKey == "" {
			return "", "", "", fmt.Errorf("account %q has no API key; run `kittypaw setup` or restart the daemon once", accountID)
		}
		return bind, apiKey, account.ID, nil
	}
	if serverCfg != nil && (serverCfg.Bind != "" || serverCfg.MasterAPIKey != "" || serverCfg.DefaultAccount != "") {
		return resolveFromServerConfig(*serverCfg, accounts, dir)
	}
	switch len(accounts) {
	case 1:
		return accounts[0].Config.Server.BindOrDefault(), accounts[0].Config.Server.APIKey, accounts[0].ID, nil
	case 0:
		// Fall through to the legacy top-level path below.
	default:
		ids := make([]string, 0, len(accounts))
		for _, account := range accounts {
			ids = append(ids, account.ID)
		}
		sort.Strings(ids)
		return "", "", "", fmt.Errorf(
			"multiple accounts found (%s); create %s with bind and master_api_key, or pass --remote",
			strings.Join(ids, ", "), filepath.Join(dir, "server.toml"))
	}

	legacyPath := filepath.Join(dir, "config.toml")
	tried = append(tried, legacyPath)
	if cfg, lerr := core.LoadConfig(legacyPath); lerr == nil {
		return cfg.Server.BindOrDefault(), cfg.Server.APIKey, "", nil
	}

	return "", "", "", fmt.Errorf(
		"no daemon config found — run `kittypaw setup` first (checked: %s)",
		strings.Join(tried, ", "))
}

func resolveFromServerConfig(sc core.TopLevelServerConfig, accounts []*core.Account, dir string) (string, string, string, error) {
	bind := sc.Bind
	apiKey := sc.MasterAPIKey
	var resolvedAccountID string
	if sc.DefaultAccount != "" {
		account, err := selectDaemonEndpointAccount(accounts, sc.DefaultAccount)
		if err != nil {
			return "", "", "", err
		}
		if account != nil {
			resolvedAccountID = account.ID
		}
	}

	if bind == "" || apiKey == "" {
		account, err := selectDaemonEndpointAccount(accounts, sc.DefaultAccount)
		if err != nil {
			return "", "", "", err
		}
		if account == nil {
			legacyPath := filepath.Join(dir, "config.toml")
			cfg, lerr := core.LoadConfig(legacyPath)
			if lerr != nil {
				return "", "", "", fmt.Errorf("server.toml is missing bind or master_api_key and no account config exists")
			}
			if bind == "" {
				bind = cfg.Server.BindOrDefault()
			}
			if apiKey == "" {
				apiKey = cfg.Server.APIKey
			}
			return bind, apiKey, "", nil
		}
		resolvedAccountID = account.ID
		if bind == "" {
			bind = account.Config.Server.BindOrDefault()
		}
		if apiKey == "" {
			apiKey = account.Config.Server.APIKey
		}
	}
	return bind, apiKey, resolvedAccountID, nil
}

func findDaemonEndpointAccount(accounts []*core.Account, accountID string) (*core.Account, error) {
	for _, account := range accounts {
		if account.ID == accountID {
			return account, nil
		}
	}
	ids := make([]string, 0, len(accounts))
	for _, account := range accounts {
		ids = append(ids, account.ID)
	}
	sort.Strings(ids)
	if len(ids) == 0 {
		return nil, fmt.Errorf("account %q not found; run `kittypaw setup` first", accountID)
	}
	return nil, fmt.Errorf("account %q not found (available: %s)", accountID, strings.Join(ids, ", "))
}

func selectDaemonEndpointAccount(accounts []*core.Account, configuredDefault string) (*core.Account, error) {
	if len(accounts) == 0 {
		return nil, nil
	}
	if configuredDefault != "" {
		for _, account := range accounts {
			if account.ID == configuredDefault {
				return account, nil
			}
		}
		return nil, fmt.Errorf("server.toml default_account %q not found", configuredDefault)
	}
	if len(accounts) == 1 {
		return accounts[0], nil
	}
	for _, account := range accounts {
		if account.ID == core.DefaultAccountID {
			return account, nil
		}
	}
	ids := make([]string, 0, len(accounts))
	for _, account := range accounts {
		ids = append(ids, account.ID)
	}
	sort.Strings(ids)
	return nil, fmt.Errorf(
		"multiple accounts found (%s); add default_account to %s or pass --remote",
		strings.Join(ids, ", "), "server.toml")
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

	if err := WritePidFile(pidPath, proc.Process.Pid); err != nil {
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
	pid, _, ok := ReadPidFile(path)
	return pid, ok
}

// WritePidFile writes pid + its start-time fingerprint in 2-line
// text format ("<pid>\n<start_time>\n"). Phase 13.4: when daemons
// became persistent across chat sessions (Phase 12), the PID-only
// validation in `kittypaw stop` started carrying real PID-reuse
// risk — a sleeping laptop could let the daemon's PID get recycled
// before the user remembered to stop it. Recording the start time
// alongside the PID lets stop refuse to signal a process that
// happens to share the recorded PID but has a different start
// time. start_time=0 is written when the platform doesn't surface
// a start time (Windows) — the verification path treats 0 as
// "skip" so legacy-platform behavior is preserved.
func WritePidFile(path string, pid int) error {
	startTime, _ := processStartTime(pid)
	content := fmt.Sprintf("%d\n%d\n", pid, startTime)
	return os.WriteFile(path, []byte(content), 0o600)
}

// ReadPidFile parses pid + recorded start time from path. Returns
// ok=false when the file is absent, the first line is not a valid
// PID, or the file has 2+ lines but the second is not a valid
// integer (a corrupt 2-line file must NOT silently downgrade to
// legacy mode — that would bypass start-time verification).
//
// recordedStart=0 is reserved for the *legitimate* legacy single-
// line format written before Phase 13.4. VerifyDaemonStartTime
// treats 0 as "skip verification" so an in-place upgrade (old
// daemon, new CLI) keeps working until the daemon restarts.
func ReadPidFile(path string) (pid int, recordedStart int64, ok bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, 0, false
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) == 0 {
		return 0, 0, false
	}
	pid, err = strconv.Atoi(strings.TrimSpace(lines[0]))
	if err != nil {
		return 0, 0, false
	}
	if len(lines) >= 2 {
		recordedStart, err = strconv.ParseInt(strings.TrimSpace(lines[1]), 10, 64)
		if err != nil {
			return 0, 0, false
		}
	}
	return pid, recordedStart, true
}

// VerifyDaemonStartTime returns true when the live process at pid
// has the same start-time fingerprint that was recorded in the PID
// file. Returns false on mismatch — the caller should treat the
// PID as stale (PID reuse) and refuse to signal.
//
// Two distinct paths around the recorded value:
//
//   - recordedStart=0 is the legacy / unsupported-platform marker.
//     Always returns true so a daemon written before Phase 13.4 (or
//     on a platform whose start time we can't read, e.g. Windows)
//     keeps working — there's nothing to verify against, and the
//     pre-Phase-13.4 PID-only contract is the best we can do.
//
//   - recordedStart!=0 is a Phase 13.4 fingerprint. If the live
//     start time can't be read (ps blocked, /proc hidden, exec
//     failure), we **fail closed** — the whole point of recording
//     the fingerprint was to refuse signals when verification is
//     impossible, so a "trust on error" fallback would silently
//     bypass the very protection this code adds.
func VerifyDaemonStartTime(pid int, recordedStart int64) bool {
	if recordedStart == 0 {
		return true
	}
	actual, err := processStartTime(pid)
	if err != nil {
		return false
	}
	return actual == recordedStart
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
