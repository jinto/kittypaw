// Command kittypaw is the CLI for the KittyPaw AI agent platform.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/google/uuid"
	"golang.org/x/term"

	"github.com/chzyer/readline"
	"github.com/mattn/go-isatty"
	"github.com/mattn/go-runewidth"
	"github.com/spf13/cobra"

	"github.com/jinto/kittypaw/client"
	"github.com/jinto/kittypaw/core"
	"github.com/jinto/kittypaw/server"
	"github.com/jinto/kittypaw/store"
)

// version is set via ldflags at build time.
var version = "dev"

// flags
var (
	flagRemote string // --remote: connect to daemon instead of running locally
	flagBind   string // serve --bind
	flagDryRun bool   // run --dry-run
	flagSkill  string // log --skill
	flagLimit  int    // log --limit
)

func main() {
	root := newRootCmd()
	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}

// ---------------------------------------------------------------------------
// Table helpers — CJK-aware padding and truncation
// ---------------------------------------------------------------------------

// padW right-pads s to exactly w display columns (CJK = 2 cols).
func padW(s string, w int) string {
	sw := runewidth.StringWidth(s)
	if sw >= w {
		return s
	}
	return s + strings.Repeat(" ", w-sw)
}

// truncW truncates s to at most w display columns, appending ".." if cut.
func truncW(s string, w int) string {
	if runewidth.StringWidth(s) <= w {
		return s
	}
	return runewidth.Truncate(s, w, "..")
}

// ---------------------------------------------------------------------------
// Root
// ---------------------------------------------------------------------------

func newRootCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:          "kittypaw",
		Short:        "KittyPaw — AI agent platform",
		Version:      version,
		SilenceUsage: true,
	}

	cmd.PersistentFlags().StringVar(&flagRemote, "remote", "", "connect to remote daemon instead of local")

	cmd.AddCommand(
		newServeCmd(),
		newStopCmd(),
		newSetupCmd(),
		newChatCmd(),
		newStatusCmd(),
		newSkillCmd(),
		newRunCmd(),
		newConfigCmd(),
		newAgentCmd(),
		newLogCmd(),
		newDaemonCmd(),
		newPersonaCmd(),
		newReflectionCmd(),
		newMemoryCmd(),
		newChannelsCmd(),
		newReloadCmd(),
		newResetCmd(),
		newLoginCmd(),
		newTenantCmd(),
		newFamilyCmd(),
		newServiceCmd(),
	)

	return cmd
}

// ---------------------------------------------------------------------------
// serve
// ---------------------------------------------------------------------------

func newServeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Start the HTTP server",
		RunE:  runServe,
	}
	cmd.Flags().StringVar(&flagBind, "bind", ":3000", "address to bind")
	return cmd
}

func runServe(_ *cobra.Command, _ []string) error {
	deps, err := bootstrap()
	if err != nil {
		return err
	}
	defer func() {
		for _, td := range deps {
			_ = td.Close()
		}
	}()

	// Check port availability before starting channels.
	if err := checkPort(flagBind); err != nil {
		return err
	}

	// Write PID file so `kittypaw stop` can find us.
	writePidFile()
	defer removePidFile()

	// The server owns the engine session, channel spawner, and dispatch loop.
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	srv := server.New(deps, version)
	if err := srv.StartChannels(ctx); err != nil {
		return fmt.Errorf("start channels: %w", err)
	}

	// Start HTTP server (blocks until shutdown signal).
	slog.Info("kittypaw serving", "bind", flagBind)
	return srv.ListenAndServe(flagBind)
}

// ---------------------------------------------------------------------------
// init
// ---------------------------------------------------------------------------

func newSetupCmd() *cobra.Command {
	flags := &setupFlags{}
	cmd := &cobra.Command{
		Use:   "setup",
		Short: "Set up or reconfigure KittyPaw",
		Long:  "Set up KittyPaw interactively (LLM, channels, web search, workspace) or via flags for CI.",
		RunE: func(c *cobra.Command, _ []string) error {
			return runSetup(c, flags)
		},
	}
	cmd.Flags().StringVar(&flags.provider, "provider", "", "LLM provider (anthropic|openrouter|local)")
	cmd.Flags().StringVar(&flags.apiKey, "api-key", "", "LLM API key")
	cmd.Flags().StringVar(&flags.localURL, "local-url", "", "Local LLM URL (default: http://localhost:11434/v1)")
	cmd.Flags().StringVar(&flags.localModel, "local-model", "", "Local LLM model name")
	cmd.Flags().StringVar(&flags.telegramToken, "telegram-token", "", "Telegram bot token")
	cmd.Flags().StringVar(&flags.telegramChatID, "telegram-chat-id", "", "Telegram chat ID")
	cmd.Flags().StringVar(&flags.firecrawlKey, "firecrawl-api-key", "", "Firecrawl API key for web search")
	cmd.Flags().StringVar(&flags.workspace, "workspace", "", "Workspace directory path")
	cmd.Flags().BoolVar(&flags.httpAccess, "http-access", false, "Grant HTTP access capability")
	cmd.Flags().BoolVar(&flags.force, "force", false, "Overwrite existing config without confirmation")
	cmd.Flags().BoolVar(&flags.noChat, "no-chat", false, "Skip the post-setup chat REPL prompt (auto-entry)")
	cmd.Flags().BoolVar(&flags.noService, "no-service", false, "Skip the post-setup service-install prompt")
	cmd.Flags().BoolVar(&flags.web, "web", false, "Open the web onboarding UI in a browser (requires a running daemon)")
	return cmd
}

func runSetup(cmd *cobra.Command, flags *setupFlags) error {
	if flags.web {
		return runSetupWeb(cmd)
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}

	kittypawDir := filepath.Join(home, ".kittypaw")
	cfgPath, err := core.ConfigPath()
	if err != nil {
		return err
	}
	tenantDir := filepath.Dir(cfgPath)
	for _, dir := range []string{
		kittypawDir,
		filepath.Join(kittypawDir, "data"),
		filepath.Join(kittypawDir, "skills"),
		tenantDir,
	} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return fmt.Errorf("create %s: %w", dir, err)
		}
	}
	var existing *core.Config
	if cfg, err := core.LoadConfig(cfgPath); err == nil {
		existing = cfg
	}

	// Run wizard.
	result, err := runWizard(*flags, existing)
	if err != nil {
		return err
	}

	// Merge and write config.
	base := core.DefaultConfig()
	if existing != nil {
		base = *existing
	}
	merged := core.MergeWizardSettings(&base, result)
	if err := core.WriteConfigAtomic(merged, cfgPath); err != nil {
		return fmt.Errorf("write config: %w", err)
	}

	// HTTP access grant (requires store).
	if result.HTTPAccess {
		if st, err := openStore(); err == nil {
			_ = st.GrantCapability("http")
			_ = st.Close()
		}
	}

	// Save API server URL to per-tenant secrets for package source bindings.
	if result.APIServerURL != "" {
		if secrets, err := core.LoadTenantSecrets(core.DefaultTenantID); err == nil {
			_ = secrets.Set("kittypaw-api", "api_url", result.APIServerURL)
		}
	}

	// Ensure default profile under the multi-tenant default-tenant base when
	// migration has already run; otherwise the top-level path so a fresh
	// install still lands somewhere `MigrateLegacyLayout` will pick up.
	profileBase, err := defaultTenantBase()
	if err != nil {
		return fmt.Errorf("resolve tenant base: %w", err)
	}
	if err := core.EnsureDefaultProfile(profileBase); err != nil {
		return fmt.Errorf("ensure default profile: %w", err)
	}

	// Ask a live daemon to reload before we display the completion box — a
	// subsequent `kittypaw chat` connects to a server that already sees the
	// new config (AC-RELOAD-SYNC). Outcome gates auto-entry below.
	reloadRes := maybeReloadDaemon(defaultDaemonDial, os.Stdout, os.Stderr)

	fmt.Println()
	fmt.Println("  ╭──────────────────────────────────────────────╮")
	fmt.Println("  │")
	fmt.Println("  │  ✓ 설정 완료")
	fmt.Printf("  │  %s\n", cfgPath)
	fmt.Println("  │")
	fmt.Println("  │  다음 단계")
	fmt.Println("  │    kittypaw serve    # 메시지 수신 서비스 실행")
	fmt.Println("  │    kittypaw chat     # 터미널에서 바로 대화")
	fmt.Println("  │")
	fmt.Println("  ╰──────────────────────────────────────────────╯")
	fmt.Println()

	// Share a single stdin scanner across the service-install and chat
	// prompts so one prompt's unread bytes don't get swallowed by a fresh
	// bufio.Scanner on the next.
	stdinTTY := isatty.IsTerminal(os.Stdin.Fd())
	stdoutTTY := isatty.IsTerminal(os.Stdout.Fd())
	serviceEligible := serviceInstallEligible(*flags, stdinTTY, stdoutTTY)
	chatEligible := autoChatEligible(*flags, stdinTTY, stdoutTTY)

	var scanner *bufio.Scanner
	if serviceEligible || chatEligible {
		scanner = bufio.NewScanner(os.Stdin)
	}

	if serviceEligible {
		_ = maybeInstallService(scanner, os.Stdout, os.Stderr)
	}

	// Auto-entry: when setup ran interactively, offer to drop straight into
	// the chat REPL. Non-interactive (provider flag set) and explicit
	// --no-chat paths skip this entirely (AC-1 / AC-2 / AC-3).
	if !chatEligible {
		return nil
	}
	// If a live daemon refused our reload, chat would attach to a server that
	// still holds the PREVIOUS config — silently running the old LLM key /
	// channels. Surface that and bail out instead of auto-entering.
	if reloadRes == reloadOutcomeFailed {
		_, _ = fmt.Fprintln(os.Stderr, setupMsgAutoChatBlocked)
		return nil
	}
	if !promptYesNo(scanner, setupPromptAutoChat, true) {
		return nil
	}
	return runChat(cmd, nil)
}

// runSetupWeb handles `kittypaw setup --web`. A daemon has to be listening
// already (either via `kittypaw service install` or `kittypaw serve` in
// another terminal) — the web wizard lives inside the daemon binary and
// cannot be served in isolation without duplicating that logic. If none is
// up we print explicit recovery steps rather than silently spawning a
// foreground daemon, which would surprise users by blocking their terminal.
func runSetupWeb(_ *cobra.Command) error {
	conn, err := client.NewDaemonConn(flagRemote)
	if err == nil && conn.IsRunning() {
		url := conn.BaseURL
		fmt.Printf("웹 온보딩: %s\n", url)
		if openErr := openBrowser(url); openErr != nil {
			fmt.Printf("(브라우저 자동 열기에 실패했습니다 — 위 URL을 수동으로 여세요: %v)\n", openErr)
		}
		return nil
	}
	fmt.Println("데몬이 아직 기동되지 않았습니다.")
	fmt.Println("다음 중 하나를 먼저 실행한 뒤 'kittypaw setup --web' 을 다시 시도하세요:")
	fmt.Println("  kittypaw service install      # 백그라운드 등록 + 기동")
	fmt.Println("  kittypaw serve                # 이 터미널에서 포그라운드 기동")
	fmt.Println("또는 브라우저로 http://127.0.0.1:3000 에 직접 접속하세요.")
	return nil
}

// openBrowser shells out to the platform-appropriate URL opener. Best
// effort — a non-zero exit or missing opener is surfaced to the caller
// so the CLI can fall back to printing the URL.
func openBrowser(url string) error {
	var bin string
	switch runtime.GOOS {
	case "darwin":
		bin = "open"
	case "linux":
		bin = "xdg-open"
	case "windows":
		bin = "rundll32"
	default:
		return fmt.Errorf("no known browser opener for %s", runtime.GOOS)
	}
	if runtime.GOOS == "windows" {
		return exec.Command(bin, "url.dll,FileProtocolHandler", url).Start()
	}
	return exec.Command(bin, url).Start()
}

// ---------------------------------------------------------------------------
// chat
// ---------------------------------------------------------------------------

func newChatCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "chat [message]",
		Short: "Interactive chat in terminal (or one-shot with argument)",
		Args:  cobra.ArbitraryArgs,
		RunE:  runChat,
	}
}

func runChat(_ *cobra.Command, args []string) error {
	oneShot := strings.Join(args, " ")

	conn, err := client.NewDaemonConn(flagRemote)
	if err != nil {
		return err
	}

	// Auto-start daemon if needed; it stays resident across chat
	// sessions. Users free resources via `kittypaw stop`.
	if _, err := conn.Connect(); err != nil {
		return err
	}

	ctx := context.Background()

	// Show server info.
	cl := client.New(conn.BaseURL, conn.APIKey)
	srvVer, model, channels, _ := cl.ServerInfo()
	fmt.Printf("KittyPaw chat (cli %s · server %s · %s", version, srvVer, model)
	if len(channels) > 0 {
		fmt.Printf(" · %s", strings.Join(channels, ","))
	}
	fmt.Println(")")

	if version != srvVer {
		fmt.Println("  ⚠ CLI and server versions differ. Consider restarting: kittypaw stop && kittypaw serve")
	}
	fmt.Println()

	cs, err := client.DialChat(ctx, conn.WebSocketURL(), conn.APIKey)
	if err != nil {
		return err
	}

	// Readline with history. Per-tenant so a household using multiple
	// tenants (one human user per tenant per CLAUDE.md) does not leak chat
	// fragments from one person's REPL into another's. Falls back to the
	// top-level path before migration.
	historyFile := ""
	if base, err := defaultTenantBase(); err == nil {
		historyFile = filepath.Join(base, "chat_history")
	}
	rl, err := readline.NewEx(&readline.Config{
		Prompt:      "you> ",
		HistoryFile: historyFile,
	})
	if err != nil {
		cs.Close()
		return fmt.Errorf("readline init: %w", err)
	}

	// Resource cleanup with sync.Once — safe from both normal exit and SIGINT.
	var cleanupOnce sync.Once
	closeResources := func() {
		cleanupOnce.Do(func() {
			_ = rl.Close()
			cs.Close()
			fmt.Print("\033[?25h") // ensure cursor visible
		})
	}
	defer closeResources()

	// Catch Ctrl-C: restore terminal then exit. Daemon keeps running.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT)
	go func() {
		<-sigCh
		signal.Stop(sigCh)
		fmt.Println()
		closeResources()
		os.Exit(0)
	}()

	// One-shot mode: send the message, print result, exit.
	if oneShot != "" {
		spin := newSpinner("paw> ")
		spin.Start()
		var chatErr error
		sendErr := cs.Send(oneShot, client.ChatOptions{
			OnDone: func(result string, _ *int64) {
				spin.Stop()
				fmt.Println(result)
			},
			OnError: func(msg string) {
				spin.Stop()
				chatErr = fmt.Errorf("%s", msg)
			},
		})
		spin.Stop()
		if sendErr != nil {
			return sendErr
		}
		return chatErr
	}

	for {
		text, err := rl.Readline()
		if err != nil { // Ctrl-D or error
			break
		}
		text = strings.TrimSpace(text)
		if text == "" {
			continue
		}

		// turnID is allocated once per user input. The transport-drop
		// retry path below replays sendOnce with the same turnID so
		// the server's RunTurn dedupes — the LLM is invoked once, the
		// retry waits for the in-flight result.
		turnID := uuid.NewString()

		// sendOnce drives one Send attempt with its own spinner. Stopping
		// the spinner inside the callbacks (before any Printf) is what
		// keeps "paw> ⠧paw> ..." double-prefix garbage from leaking out.
		sendOnce := func() (gotResult bool, sendErr error) {
			spin := newSpinner("paw> ")
			spin.Start()
			defer spin.Stop()
			opts := client.ChatOptions{
				OnDone: func(result string, _ *int64) {
					spin.Stop()
					gotResult = true
					fmt.Printf("paw> %s\n\n", result)
				},
				OnError: func(msg string) {
					spin.Stop()
					fmt.Fprintf(os.Stderr, "paw> %s\n\n", msg)
				},
			}
			sendErr = cs.SendTurn(text, turnID, opts)
			return
		}

		gotResult, sendErr := sendOnce()
		// Silent reconnect on transport drop: redial once and replay
		// the same input. Surface only if the retry also fails.
		// Server-side application errors (ErrServerSide) are excluded —
		// retrying them would double-charge the user without healing
		// the underlying failure.
		if sendErr != nil && !gotResult && isTransportDropErr(sendErr) {
			cs.Close()
			cs, err = client.DialChat(ctx, conn.WebSocketURL(), conn.APIKey)
			if err != nil {
				return fmt.Errorf("재연결 실패: %w", err)
			}
			gotResult, sendErr = sendOnce()
		}
		if sendErr != nil && !gotResult {
			fmt.Fprintf(os.Stderr, "error: %v\n\n", sendErr)
		}
	}

	fmt.Println()
	closeResources() // restore terminal before any post-chat output

	return nil
}

// isTransportDropErr reports whether err is a transient WebSocket
// teardown that the silent-reconnect path should swallow (EOF,
// broken pipe, closed conn, reset). Errors carrying client.ErrServerSide
// are application-layer failures and never qualify — replaying them
// would double-charge the user.
func isTransportDropErr(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, client.ErrServerSide) {
		return false
	}
	if errors.Is(err, io.EOF) ||
		errors.Is(err, io.ErrUnexpectedEOF) ||
		errors.Is(err, syscall.EPIPE) ||
		errors.Is(err, syscall.ECONNRESET) ||
		errors.Is(err, net.ErrClosed) {
		return true
	}
	// Substring fallback for libraries that surface raw text errors
	// without wrapping a typed sentinel.
	msg := err.Error()
	return strings.Contains(msg, "EOF") ||
		strings.Contains(msg, "broken pipe") ||
		strings.Contains(msg, "use of closed network connection") ||
		strings.Contains(msg, "connection reset by peer")
}

// ---------------------------------------------------------------------------
// status
// ---------------------------------------------------------------------------

func newStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show today's execution stats",
		RunE:  runStatus,
	}
}

func runStatus(_ *cobra.Command, _ []string) error {
	cl, err := connectDaemon()
	if err != nil {
		return err
	}

	res, err := cl.Status()
	if err != nil {
		return fmt.Errorf("query stats: %w", err)
	}

	fmt.Println("Today's execution stats")
	fmt.Println("-----------------------")
	fmt.Printf("  Total runs:   %d\n", jsonInt(res, "total_runs"))
	fmt.Printf("  Successful:   %d\n", jsonInt(res, "successful"))
	fmt.Printf("  Failed:       %d\n", jsonInt(res, "failed"))
	fmt.Printf("  Auto-retries: %d\n", jsonInt(res, "auto_retries"))
	fmt.Printf("  Total tokens: %d\n", jsonInt(res, "total_tokens"))
	return nil
}

// ---------------------------------------------------------------------------
// skill — unified skill management
// ---------------------------------------------------------------------------

func newSkillCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "skill",
		Short: "Manage skills",
	}
	cmd.AddCommand(
		newSkillListCmd(),
		newSkillSearchCmd(),
		newSkillInstallCmd(),
		newSkillUninstallCmd(),
		newSkillInfoCmd(),
		newSkillCreateCmd(),
		newSkillEnableCmd(),
		newSkillDisableCmd(),
		newSkillExplainCmd(),
		newSkillConfigCmd(),
		newSkillSuggestCmd(),
		newSkillFixCmd(),
	)
	return cmd
}

// --- skill list ---

func newSkillListCmd() *cobra.Command {
	var filterType string
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List all skills",
		RunE: func(_ *cobra.Command, _ []string) error {
			var skills []map[string]any
			if filterType == "" || filterType == "skill" {
				if cl, err := connectDaemon(); err == nil {
					if res, err := cl.Skills(); err == nil {
						skills = jsonSlice(res, "skills")
					}
				}
			}

			var packages []core.SkillPackage
			if filterType == "" || filterType == "package" {
				if pm, err := localPackageManager(); err == nil {
					packages, _ = pm.ListInstalled()
				}
			}

			if len(skills) == 0 && len(packages) == 0 {
				fmt.Println("No skills found.")
				return nil
			}

			fmt.Printf("%s %s %s %s %s\n", padW("NAME", 20), padW("TYPE", 10), padW("VERSION", 10), padW("STATUS", 10), "DESCRIPTION")
			fmt.Println(strings.Repeat("-", 85))

			for _, s := range skills {
				status := "enabled"
				if !jsonBool(s, "enabled") {
					status = "disabled"
				}
				desc := truncW(jsonStr(s, "description"), 30)
				fmt.Printf("%s %s %s %s %s\n",
					padW(truncW(jsonStr(s, "name"), 20), 20), padW("skill", 10), padW(jsonStr(s, "version"), 10), padW(status, 10), desc)
			}

			for _, p := range packages {
				status := "installed"
				if p.Meta.Cron != "" {
					status = "cron"
				}
				desc := truncW(p.Meta.Description, 30)
				fmt.Printf("%s %s %s %s %s\n",
					padW(truncW(p.Meta.ID, 20), 20), padW("package", 10), padW(p.Meta.Version, 10), padW(status, 10), desc)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&filterType, "type", "", "filter by type: skill or package")
	return cmd
}

// --- skill search ---

func newSkillSearchCmd() *cobra.Command {
	var jsonOutput bool
	cmd := &cobra.Command{
		Use:   "search [query]",
		Short: "Search the skill registry",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			var query string
			if len(args) > 0 {
				query = args[0]
			}
			rc, err := registryClient()
			if err != nil {
				return err
			}
			idx, err := rc.FetchIndexWithMeta()
			if err != nil {
				return err
			}
			results := core.FilterEntries(idx.Entries, query)

			if jsonOutput {
				out := map[string]any{"results": results, "from_cache": idx.FromCache}
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(out)
			}

			if idx.FromCache {
				ts := "unknown"
				if !idx.CachedAt.IsZero() {
					ts = idx.CachedAt.Format("2006-01-02 15:04")
				}
				fmt.Printf("(cached results -- last updated: %s)\n\n", ts)
			}

			if len(results) == 0 {
				fmt.Println("No results found.")
				return nil
			}

			fmt.Printf("%s %s %s %s %s\n", padW("ID", 20), padW("NAME", 25), padW("VERSION", 10), padW("AUTHOR", 12), "DESCRIPTION")
			fmt.Println(strings.Repeat("-", 95))
			for _, e := range results {
				desc := truncW(e.Description, 40)
				name := truncW(e.Name, 25)
				author := truncW(e.Author, 12)
				fmt.Printf("%s %s %s %s %s\n", padW(e.ID, 20), padW(name, 25), padW(e.Version, 10), padW(author, 12), desc)
			}
			fmt.Printf("\nFound %d package(s).\n", len(results))
			return nil
		},
	}
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "output as JSON")
	return cmd
}

// --- skill install ---

func newSkillInstallCmd() *cobra.Command {
	var mdMode string
	cmd := &cobra.Command{
		Use:   "install <source>",
		Short: "Install a skill from GitHub, local path, or registry",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			arg := args[0]

			// GitHub URL or local path → daemon install.
			if strings.HasPrefix(arg, "https://") || strings.HasPrefix(arg, "http://") {
				return installViaDaemon(arg, mdMode)
			}

			fi, statErr := os.Stat(arg)
			if statErr != nil && !os.IsNotExist(statErr) {
				return statErr
			}
			if statErr == nil && fi.IsDir() {
				absPath, _ := filepath.Abs(arg)
				return installViaDaemon(absPath, mdMode)
			}

			// Otherwise treat as a registry package ID.
			secrets, err := core.LoadTenantSecrets(core.DefaultTenantID)
			if err != nil {
				return err
			}
			pm, err := localPackageManager()
			if err != nil {
				return err
			}
			rc, err := registryClient()
			if err != nil {
				return err
			}
			entry, err := rc.FindEntry(arg)
			if err != nil {
				return err
			}

			// Check for conflicting user skill before install.
			cfgDir, _ := core.ConfigDir()
			if cfgDir != "" {
				if existingSkill, _, loadErr := core.LoadSkillFrom(cfgDir, entry.ID); loadErr == nil && existingSkill != nil {
					fmt.Println("⚠  같은 이름의 사용자 스킬이 이미 존재합니다.")
					fmt.Println()
					fmt.Printf("  [사용자 스킬]  %s\n", existingSkill.Name)
					fmt.Printf("    설명: %s\n", existingSkill.Description)
					fmt.Printf("    생성: %s\n", existingSkill.CreatedAt)
					fmt.Println()
					fmt.Printf("  [패키지]  %s (%s) v%s\n", entry.Name, entry.ID, entry.Version)
					fmt.Printf("    설명: %s\n", entry.Description)
					fmt.Println()
					fmt.Println("  사용자 스킬이 패키지보다 우선 실행됩니다.")
					fmt.Println("  A. 사용자 스킬을 삭제하고 패키지 설치")
					fmt.Println("  B. 사용자 스킬 유지 (패키지도 설치하되 실행되지 않음)")
					fmt.Println("  C. 설치 취소")
					fmt.Print("  선택 [A/B/C]: ")

					var choice string
					fmt.Scanln(&choice)
					choice = strings.TrimSpace(strings.ToUpper(choice))

					switch choice {
					case "A":
						skillBase, baseErr := defaultTenantBase()
						if baseErr != nil {
							return fmt.Errorf("resolve tenant base: %w", baseErr)
						}
						if delErr := core.DeleteSkillFrom(skillBase, entry.ID); delErr != nil {
							return fmt.Errorf("사용자 스킬 삭제 실패: %w", delErr)
						}
						fmt.Printf("  사용자 스킬 %q 삭제 완료.\n", entry.ID)
					case "B":
						fmt.Println("  사용자 스킬 유지. 패키지 설치를 계속합니다.")
					default:
						fmt.Println("  설치 취소.")
						return nil
					}
				}
			}

			pkg, err := pm.InstallFromRegistry(rc, *entry)
			if err != nil {
				return err
			}
			fmt.Printf("Installed package %q (%s) v%s from registry\n",
				pkg.Meta.Name, pkg.Meta.ID, pkg.Meta.Version)

			// API-bound skills need a login. Install still succeeds — we just
			// warn early so the user sees the requirement instead of a
			// cryptic 401 at run time.
			if pkg.RequiresAPILogin() {
				mgr := core.NewAPITokenManager("", secrets)
				if tok, _ := mgr.LoadAccessToken(core.DefaultAPIServerURL); tok == "" {
					fmt.Fprintln(os.Stderr)
					fmt.Fprintln(os.Stderr, "  ℹ  이 스킬은 KittyPaw API 로그인이 필요합니다.")
					fmt.Fprintln(os.Stderr, "     kittypaw login 으로 로그인해 주세요.")
				}
			}

			return promptPackageConfig(pm, pkg)
		},
	}
	cmd.Flags().StringVar(&mdMode, "mode", "", "SKILL.md execution mode (prompt or native)")
	return cmd
}

func installViaDaemon(source, mdMode string) error {
	cl, err := connectDaemon()
	if err != nil {
		return err
	}
	result, err := cl.Install(source, mdMode)
	if err != nil {
		return err
	}
	fmt.Printf("Installed: %s (format: %s)\n",
		jsonStr(result, "SkillName"), jsonStr(result, "Format"))
	return nil
}

// --- skill uninstall ---

func newSkillUninstallCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "uninstall <name>",
		Short: "Uninstall a skill or package",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			name := args[0]

			// Try package first (local, fast).
			if pm, err := localPackageManager(); err == nil {
				if err := pm.Uninstall(name); err == nil {
					fmt.Printf("Package %q uninstalled.\n", name)
					return nil
				}
			}

			// Fall back to skill deletion via daemon.
			cl, err := connectDaemon()
			if err != nil {
				return err
			}
			if _, err := cl.DeleteSkill(name); err != nil {
				return err
			}
			fmt.Printf("Skill %q deleted.\n", name)
			return nil
		},
	}
}

// --- skill info ---

func newSkillInfoCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "info <name>",
		Short: "Show details of an installed skill or package",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			name := args[0]

			// Try package first.
			if pm, err := localPackageManager(); err == nil {
				if pkg, _, loadErr := pm.LoadPackage(name); loadErr == nil {
					printPackageInfo(pm, pkg)
					return nil
				}
			}

			// Fall back to skill via daemon.
			cl, err := connectDaemon()
			if err != nil {
				return err
			}
			res, err := cl.Skills()
			if err != nil {
				return err
			}
			for _, s := range jsonSlice(res, "skills") {
				if jsonStr(s, "name") == name {
					fmt.Printf("Name:        %s\n", jsonStr(s, "name"))
					fmt.Printf("Type:        skill\n")
					if v := jsonStr(s, "version"); v != "" {
						fmt.Printf("Version:     %s\n", v)
					}
					if d := jsonStr(s, "description"); d != "" {
						fmt.Printf("Description: %s\n", d)
					}
					fmt.Printf("Enabled:     %v\n", jsonBool(s, "enabled"))
					if t := jsonStr(s, "trigger"); t != "" {
						fmt.Printf("Trigger:     %s\n", t)
					}
					return nil
				}
			}
			return fmt.Errorf("skill %q not found", name)
		},
	}
}

func printPackageInfo(pm *core.PackageManager, pkg *core.SkillPackage) {
	fmt.Printf("ID:          %s\n", pkg.Meta.ID)
	fmt.Printf("Name:        %s\n", pkg.Meta.Name)
	fmt.Printf("Type:        package\n")
	fmt.Printf("Version:     %s\n", pkg.Meta.Version)
	if pkg.Meta.Description != "" {
		fmt.Printf("Description: %s\n", pkg.Meta.Description)
	}
	if pkg.Meta.Author != "" {
		fmt.Printf("Author:      %s\n", pkg.Meta.Author)
	}
	if pkg.Meta.Cron != "" {
		fmt.Printf("Cron:        %s\n", pkg.Meta.Cron)
	}
	if pkg.Meta.Model != "" {
		fmt.Printf("Model:       %s\n", pkg.Meta.Model)
	}
	if len(pkg.Config) > 0 {
		fmt.Println("\nConfig Fields:")
		cfg, _ := pm.GetConfig(pkg.Meta.ID)
		for _, f := range pkg.Config {
			val := cfg[f.Key]
			if f.Secret && val != "" {
				val = "****"
			}
			req := ""
			if f.Required {
				req = " (required)"
			}
			fmt.Printf("  %-20s %s%s\n", f.Key, val, req)
		}
	}
	if len(pkg.Permissions.Primitives) > 0 {
		fmt.Printf("\nPermissions: %s\n", strings.Join(pkg.Permissions.Primitives, ", "))
	}
	if len(pkg.Permissions.AllowedHosts) > 0 {
		fmt.Printf("Hosts:       %s\n", strings.Join(pkg.Permissions.AllowedHosts, ", "))
	}
}

// --- skill create ---

func newSkillCreateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "create <description...>",
		Short: "Create a new skill from description",
		Args:  cobra.MinimumNArgs(1),
		RunE:  runTeach,
	}
}

// --- skill enable / disable / delete / explain ---

func newSkillEnableCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "enable <name>",
		Short: "Enable a skill",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			cl, err := connectDaemon()
			if err != nil {
				return err
			}
			if _, err := cl.EnableSkill(args[0]); err != nil {
				return err
			}
			fmt.Printf("Skill %q enabled.\n", args[0])
			return nil
		},
	}
}

func newSkillDisableCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "disable <name>",
		Short: "Disable a skill",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			cl, err := connectDaemon()
			if err != nil {
				return err
			}
			if _, err := cl.DisableSkill(args[0]); err != nil {
				return err
			}
			fmt.Printf("Skill %q disabled.\n", args[0])
			return nil
		},
	}
}

func newSkillExplainCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "explain <name>",
		Short: "Explain what a skill does",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			cl, err := connectDaemon()
			if err != nil {
				return err
			}
			res, err := cl.ExplainSkill(args[0])
			if err != nil {
				return err
			}
			fmt.Println(jsonStr(res, "explanation"))
			return nil
		},
	}
}

// --- skill config ---

func newSkillConfigCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "config <name> [key] [value]",
		Short: "Get or set skill configuration",
		Args:  cobra.RangeArgs(1, 3),
		RunE: func(_ *cobra.Command, args []string) error {
			pm, err := localPackageManager()
			if err != nil {
				return err
			}
			id := args[0]

			if len(args) == 1 {
				cfg, err := pm.GetConfig(id)
				if err != nil {
					return err
				}
				if len(cfg) == 0 {
					fmt.Println("No configuration fields.")
					return nil
				}
				for k, v := range cfg {
					fmt.Printf("  %s = %s\n", k, v)
				}
				return nil
			}
			if len(args) == 3 {
				return pm.SetConfig(id, args[1], args[2])
			}
			return fmt.Errorf("usage: kittypaw skill config <name> [key value]")
		},
	}
}

// --- skill suggest ---

func newSkillSuggestCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "suggest",
		Short: "Manage skill suggestions",
		RunE: func(_ *cobra.Command, _ []string) error {
			// Default action: list suggestions.
			cl, err := connectDaemon()
			if err != nil {
				return err
			}
			res, err := cl.SuggestionsList()
			if err != nil {
				return err
			}
			items := jsonSlice(res, "suggestions")
			if len(items) == 0 {
				fmt.Println("No suggestions found.")
				return nil
			}
			fmt.Printf("%s %s\n", padW("SKILL_ID", 20), "DESCRIPTION")
			fmt.Println(strings.Repeat("-", 60))
			for _, s := range items {
				desc := truncW(jsonStr(s, "description"), 50)
				fmt.Printf("%s %s\n", padW(jsonStr(s, "skill_id"), 20), desc)
			}
			return nil
		},
	}
	cmd.AddCommand(
		&cobra.Command{
			Use:   "accept <skill-id>",
			Short: "Accept a suggestion",
			Args:  cobra.ExactArgs(1),
			RunE: func(_ *cobra.Command, args []string) error {
				cl, err := connectDaemon()
				if err != nil {
					return err
				}
				if _, err := cl.SuggestionsAccept(args[0]); err != nil {
					return err
				}
				fmt.Printf("Suggestion %q accepted.\n", args[0])
				return nil
			},
		},
		&cobra.Command{
			Use:   "dismiss <skill-id>",
			Short: "Dismiss a suggestion",
			Args:  cobra.ExactArgs(1),
			RunE: func(_ *cobra.Command, args []string) error {
				cl, err := connectDaemon()
				if err != nil {
					return err
				}
				if _, err := cl.SuggestionsDismiss(args[0]); err != nil {
					return err
				}
				fmt.Printf("Suggestion %q dismissed.\n", args[0])
				return nil
			},
		},
	)
	return cmd
}

// --- skill fix ---

func newSkillFixCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "fix",
		Short: "Manage skill fixes",
	}
	cmd.AddCommand(
		&cobra.Command{
			Use:   "list <skill>",
			Short: "List fixes for a skill",
			Args:  cobra.ExactArgs(1),
			RunE: func(_ *cobra.Command, args []string) error {
				cl, err := connectDaemon()
				if err != nil {
					return err
				}
				res, err := cl.FixesList(args[0])
				if err != nil {
					return err
				}
				fixes := jsonSlice(res, "fixes")
				if len(fixes) == 0 {
					fmt.Println("No fixes found.")
					return nil
				}
				fmt.Printf("%s %s %s %s\n", padW("ID", 6), padW("APPLIED", 8), padW("DATE", 20), "ERROR")
				fmt.Println(strings.Repeat("-", 60))
				for _, f := range fixes {
					applied := "no"
					if jsonBool(f, "applied") {
						applied = "yes"
					}
					errMsg := truncW(jsonStr(f, "error_message"), 30)
					fmt.Printf("%s %s %s %s\n",
						padW(fmt.Sprintf("%d", jsonInt(f, "id")), 6), padW(applied, 8), padW(jsonStr(f, "created_at"), 20), errMsg)
				}
				return nil
			},
		},
		&cobra.Command{
			Use:   "approve <id>",
			Short: "Approve a fix",
			Args:  cobra.ExactArgs(1),
			RunE: func(_ *cobra.Command, args []string) error {
				cl, err := connectDaemon()
				if err != nil {
					return err
				}
				if _, err := cl.FixesApprove(args[0]); err != nil {
					return err
				}
				fmt.Printf("Fix %s approved.\n", args[0])
				return nil
			},
		},
	)
	return cmd
}

// ---------------------------------------------------------------------------
// run
// ---------------------------------------------------------------------------

func newRunCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "run <name>",
		Short: "Run a skill by name",
		Args:  cobra.ExactArgs(1),
		RunE:  runSkill,
	}
	cmd.Flags().BoolVar(&flagDryRun, "dry-run", false, "show what would happen without executing")
	return cmd
}

func runSkill(_ *cobra.Command, args []string) error {
	name := args[0]

	if flagDryRun {
		// Dry run stays local — no daemon needed.
		skill, code, err := core.LoadSkill(name)
		if err != nil {
			return fmt.Errorf("load skill: %w", err)
		}
		if skill == nil {
			return fmt.Errorf("skill %q not found", name)
		}
		fmt.Printf("Skill:       %s\n", skill.Name)
		fmt.Printf("Description: %s\n", skill.Description)
		fmt.Printf("Trigger:     %s\n", skill.Trigger.Type)
		fmt.Printf("Code length: %d bytes\n", len(code))
		fmt.Println("\n(dry run — not executed)")
		return nil
	}

	cl, err := connectDaemon()
	if err != nil {
		return err
	}

	res, err := cl.RunSkill(name)
	if err != nil {
		return fmt.Errorf("run skill: %w", err)
	}
	if output := jsonStr(res, "output"); output != "" {
		fmt.Println(output)
	}
	return nil
}

// ---------------------------------------------------------------------------
// teach (shared logic, used by skill create)
// ---------------------------------------------------------------------------

func runTeach(_ *cobra.Command, args []string) error {
	description := strings.Join(args, " ")

	cl, err := connectDaemon()
	if err != nil {
		return err
	}

	res, err := cl.Teach(description)
	if err != nil {
		return fmt.Errorf("teach skill: %w", err)
	}

	name := jsonStr(res, "skill_name")
	desc := jsonStr(res, "description")
	code := jsonStr(res, "code")
	syntaxOK := jsonBool(res, "syntax_ok")
	syntaxErr := jsonStr(res, "syntax_error")

	triggerType := ""
	if trig, ok := res["trigger"].(map[string]any); ok {
		triggerType = jsonStr(trig, "type")
	}

	// Print preview.
	fmt.Printf("스킬명: %s\n", name)
	fmt.Printf("설명:  %s\n", desc)
	fmt.Printf("트리거: %s\n", triggerType)

	if perms, ok := res["permissions"].([]any); ok && len(perms) > 0 {
		var permStrs []string
		for _, p := range perms {
			if s, ok := p.(string); ok {
				permStrs = append(permStrs, s)
			}
		}
		fmt.Printf("권한:  %s\n", strings.Join(permStrs, ", "))
	}

	fmt.Printf("\n--- 생성된 코드 ---\n%s\n--- 코드 끝 ---\n\n", code)

	if !syntaxOK {
		return fmt.Errorf("구문 오류: %s", syntaxErr)
	}

	// Interactive approval.
	fmt.Print("이 스킬을 저장하시겠습니까? (y/n): ")
	var answer string
	fmt.Scanln(&answer)
	if answer != "y" && answer != "Y" {
		fmt.Println("취소되었습니다.")
		return nil
	}

	if _, err := cl.TeachApprove(name, desc, code, triggerType, ""); err != nil {
		return fmt.Errorf("스킬 저장 실패: %w", err)
	}
	fmt.Printf("스킬 '%s' 저장 완료!\n", name)
	return nil
}

// ---------------------------------------------------------------------------
// config
// ---------------------------------------------------------------------------

func newConfigCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Configuration management",
	}
	cmd.AddCommand(newConfigCheckCmd())
	return cmd
}

func newConfigCheckCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "check",
		Short: "Show config summary",
		RunE:  runConfigCheck,
	}
}

func runConfigCheck(_ *cobra.Command, _ []string) error {
	cfgPath, err := core.ConfigPath()
	if err != nil {
		return err
	}
	cfg, err := core.LoadConfig(cfgPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	fmt.Printf("Config:     %s\n", cfgPath)
	fmt.Printf("Provider:   %s\n", cfg.LLM.Provider)
	fmt.Printf("Model:      %s\n", cfg.LLM.Model)
	fmt.Printf("Channels:   %d\n", len(cfg.Channels))
	fmt.Printf("Agents:     %d\n", len(cfg.Agents))
	fmt.Printf("Models:     %d\n", len(cfg.Models))
	fmt.Printf("Autonomy:   %s\n", cfg.AutonomyLevel)

	fmt.Println("\nFeatures:")
	fmt.Printf("  progressive_retry:  %v\n", cfg.Features.ProgressiveRetry)
	fmt.Printf("  context_compaction: %v\n", cfg.Features.ContextCompaction)
	fmt.Printf("  model_routing:      %v\n", cfg.Features.ModelRouting)
	fmt.Printf("  background_agents:  %v\n", cfg.Features.BackgroundAgents)
	if cfg.Features.DailyTokenLimit > 0 {
		fmt.Printf("  daily_token_limit:  %d\n", cfg.Features.DailyTokenLimit)
	}
	return nil
}

// ---------------------------------------------------------------------------
// agent
// ---------------------------------------------------------------------------

func newAgentCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "agent",
		Short: "Agent management",
	}
	cmd.AddCommand(newAgentListCmd())
	return cmd
}

func newAgentListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List configured agents",
		RunE:  runAgentList,
	}
}

func runAgentList(_ *cobra.Command, _ []string) error {
	cl, err := connectDaemon()
	if err != nil {
		return err
	}

	res, err := cl.Agents()
	if err != nil {
		return fmt.Errorf("list agents: %w", err)
	}

	agents := jsonSlice(res, "agents")
	if len(agents) == 0 {
		fmt.Println("No agents found.")
		return nil
	}

	fmt.Printf("%s %s %s\n", padW("AGENT ID", 30), padW("TURNS", 6), "UPDATED")
	fmt.Println(strings.Repeat("-", 60))
	for _, a := range agents {
		fmt.Printf("%s %s %s\n", padW(jsonStr(a, "agent_id"), 30), padW(fmt.Sprintf("%d", jsonInt(a, "turn_count")), 6), jsonStr(a, "updated_at"))
	}
	return nil
}

// ---------------------------------------------------------------------------
// log
// ---------------------------------------------------------------------------

func newLogCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "log",
		Short: "Show execution log",
		RunE:  runLog,
	}
	cmd.Flags().StringVar(&flagSkill, "skill", "", "filter by skill name")
	cmd.Flags().IntVar(&flagLimit, "limit", 20, "number of entries to show")
	return cmd
}

func runLog(_ *cobra.Command, _ []string) error {
	cl, err := connectDaemon()
	if err != nil {
		return err
	}

	res, err := cl.Executions(flagSkill, flagLimit)
	if err != nil {
		return fmt.Errorf("query executions: %w", err)
	}

	records := jsonSlice(res, "executions")
	if len(records) == 0 {
		fmt.Println("No execution records found.")
		return nil
	}

	fmt.Printf("%s %s %s %s %s\n", padW("ID", 5), padW("SKILL", 20), padW("STARTED", 20), padW("STATUS", 7), "DURATION")
	fmt.Println(strings.Repeat("-", 80))
	for _, r := range records {
		status := "OK"
		if !jsonBool(r, "success") {
			status = "FAIL"
		}
		duration := strconv.FormatInt(jsonInt(r, "duration_ms"), 10) + "ms"
		fmt.Printf("%s %s %s %s %s\n", padW(fmt.Sprintf("%d", jsonInt(r, "id")), 5), padW(truncW(jsonStr(r, "skill_name"), 20), 20), padW(jsonStr(r, "started_at"), 20), padW(status, 7), duration)
	}
	return nil
}

// ---------------------------------------------------------------------------
// daemon
// ---------------------------------------------------------------------------

func newDaemonCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "daemon",
		Short: "Manage daemon process",
	}
	cmd.AddCommand(
		newDaemonStartCmd(),
		newDaemonStopCmd(),
		newDaemonStatusCmd(),
	)
	return cmd
}

func newDaemonStartCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "start",
		Short: "Start daemon in background",
		RunE:  runDaemonStart,
	}
}

func runDaemonStart(_ *cobra.Command, _ []string) error {
	pidPath, err := daemonPidPath()
	if err != nil {
		return err
	}

	// Check if already running. Verify start-time so a stale PID
	// recycled by an unrelated process does not produce a false
	// "already running" error.
	if pid, recordedStart, ok := client.ReadPidFile(pidPath); ok {
		if processRunning(pid) && client.VerifyDaemonStartTime(pid, recordedStart) {
			return fmt.Errorf("daemon already running (pid %d)", pid)
		}
	}

	// Re-exec ourselves with "serve" in the background.
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("find executable: %w", err)
	}

	proc := exec.Command(exe, "serve")
	proc.Env = append(os.Environ(), "KITTYPAW_DAEMON=1")
	proc.Stdout = nil
	proc.Stderr = nil
	setSysProcAttr(proc)

	if err := proc.Start(); err != nil {
		return fmt.Errorf("start daemon: %w", err)
	}

	if err := client.WritePidFile(pidPath, proc.Process.Pid); err != nil {
		return fmt.Errorf("write pid file: %w", err)
	}

	fmt.Printf("Daemon started (pid %d).\n", proc.Process.Pid)
	return nil
}

func newDaemonStopCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "stop",
		Short: "Stop the daemon",
		RunE:  runDaemonStop,
	}
}

func runDaemonStop(_ *cobra.Command, _ []string) error {
	pidPath, err := daemonPidPath()
	if err != nil {
		return err
	}

	pid, recordedStart, ok := client.ReadPidFile(pidPath)
	if !ok {
		fmt.Println("No daemon pid file found.")
		return nil
	}

	// Phase 13.4 PID hardening: refuse to signal a recycled PID.
	if !client.VerifyDaemonStartTime(pid, recordedStart) {
		fmt.Printf("PID %d does not match the recorded daemon (PID was reused). Cleaning up stale pid file.\n", pid)
		os.Remove(pidPath)
		return nil
	}

	proc, err := os.FindProcess(pid)
	if err != nil {
		return fmt.Errorf("find process: %w", err)
	}

	if err := proc.Signal(syscall.SIGTERM); err != nil {
		fmt.Printf("Could not signal process %d: %v\n", pid, err)
	} else {
		fmt.Printf("Daemon stopped (pid %d).\n", pid)
	}

	os.Remove(pidPath)
	return nil
}

func newDaemonStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show daemon status",
		RunE:  runDaemonStatus,
	}
}

func runDaemonStatus(_ *cobra.Command, _ []string) error {
	pidPath, err := daemonPidPath()
	if err != nil {
		return err
	}

	pid, recordedStart, ok := client.ReadPidFile(pidPath)
	if !ok {
		fmt.Println("Daemon is not running (no pid file).")
		return nil
	}

	if !processRunning(pid) {
		fmt.Printf("Daemon is not running (stale pid %d).\n", pid)
		os.Remove(pidPath)
		return nil
	}
	if !client.VerifyDaemonStartTime(pid, recordedStart) {
		fmt.Printf("Daemon is not running (pid %d was reused by an unrelated process).\n", pid)
		os.Remove(pidPath)
		return nil
	}
	fmt.Printf("Daemon is running (pid %d).\n", pid)
	return nil
}

// ---------------------------------------------------------------------------
// stop
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// reset
// ---------------------------------------------------------------------------

func newResetCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "reset [agent-id]",
		Short: "Reset conversation history",
		Long:  "Clear conversation history for all agents, or a specific agent if specified.",
		RunE:  runReset,
	}
	return cmd
}

func runReset(_ *cobra.Command, args []string) error {
	st, err := openStore()
	if err != nil {
		return err
	}
	defer st.Close() //nolint:errcheck

	agentID := ""
	if len(args) > 0 {
		agentID = args[0]
	}

	deleted, err := st.ResetConversations(agentID)
	if err != nil {
		return fmt.Errorf("reset: %w", err)
	}

	if agentID != "" {
		fmt.Printf("Reset %d turns for agent %q.\n", deleted, agentID)
	} else {
		fmt.Printf("Reset %d turns (all agents).\n", deleted)
	}
	return nil
}

func newStopCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "stop",
		Short: "Stop the running server",
		RunE:  runStop,
	}
}

func runStop(_ *cobra.Command, _ []string) error {
	pidPath, err := daemonPidPath()
	if err != nil {
		return err
	}

	pid, recordedStart, ok := client.ReadPidFile(pidPath)
	if !ok || !processRunning(pid) {
		lang := detectLang()
		switch {
		case strings.HasPrefix(lang, "ko"):
			fmt.Println("실행 중인 KittyPaw가 없습니다.")
		case strings.HasPrefix(lang, "ja"):
			fmt.Println("実行中のKittyPawはありません。")
		default:
			fmt.Println("No running KittyPaw found.")
		}
		if ok {
			os.Remove(pidPath)
		}
		return nil
	}

	// Phase 13.4 PID hardening: recorded start time must match the
	// live process's. If it doesn't, the PID was reused by an
	// unrelated process and we must NOT signal it.
	if !client.VerifyDaemonStartTime(pid, recordedStart) {
		fmt.Printf("PID %d does not match the recorded daemon (PID was reused). Cleaning up stale pid file.\n", pid)
		os.Remove(pidPath)
		return nil
	}

	proc, err := os.FindProcess(pid)
	if err != nil {
		return fmt.Errorf("find process: %w", err)
	}

	if err := proc.Signal(syscall.SIGTERM); err != nil {
		return fmt.Errorf("stop process %d: %w", pid, err)
	}

	os.Remove(pidPath)
	fmt.Printf("Stopped (pid %d).\n", pid)
	return nil
}

// checkPort verifies that the bind address is available before starting
// channels. This avoids wasting time on Telegram/Slack connections only
// to fail on port conflict.
func checkPort(addr string) error {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		if isAddrInUse(err) {
			printPortInUseMessage(addr)
		}
		return err
	}
	_ = ln.Close()
	return nil
}

func isAddrInUse(err error) bool {
	return err != nil && strings.Contains(err.Error(), "address already in use")
}

func printPortInUseMessage(addr string) {
	lang := detectLang()
	switch {
	case strings.HasPrefix(lang, "ko"):
		fmt.Printf("\n  ⚠ %s 포트가 이미 사용 중입니다.\n", addr)
		fmt.Println("    이미 실행 중인 KittyPaw가 있을 수 있습니다.")
		fmt.Println()
		fmt.Println("    kittypaw stop     # 기존 서버 종료")
		fmt.Println("    kittypaw serve    # 다시 시작")
		fmt.Println()
	case strings.HasPrefix(lang, "ja"):
		fmt.Printf("\n  ⚠ ポート %s は既に使用中です。\n", addr)
		fmt.Println("    KittyPawが既に実行中の可能性があります。")
		fmt.Println()
		fmt.Println("    kittypaw stop     # サーバーを停止")
		fmt.Println("    kittypaw serve    # 再起動")
		fmt.Println()
	default:
		fmt.Printf("\n  ⚠ Port %s is already in use.\n", addr)
		fmt.Println("    Another KittyPaw instance may be running.")
		fmt.Println()
		fmt.Println("    kittypaw stop     # stop the existing server")
		fmt.Println("    kittypaw serve    # restart")
		fmt.Println()
	}
}

func writePidFile() {
	// When launched via `daemon start`, the parent manages the PID file.
	if os.Getenv("KITTYPAW_DAEMON") != "" {
		return
	}
	pidPath, err := daemonPidPath()
	if err != nil {
		return
	}
	_ = client.WritePidFile(pidPath, os.Getpid())
}

func removePidFile() {
	if os.Getenv("KITTYPAW_DAEMON") != "" {
		return
	}
	pidPath, err := daemonPidPath()
	if err != nil {
		return
	}
	// Only remove if it's our PID (another instance may have overwritten it).
	if pid, _, ok := client.ReadPidFile(pidPath); ok && pid == os.Getpid() {
		os.Remove(pidPath)
	}
}

// ---------------------------------------------------------------------------
// persona
// ---------------------------------------------------------------------------

func newPersonaCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "persona",
		Short: "Manage persona presets",
	}
	cmd.AddCommand(newPersonaListCmd(), newPersonaApplyCmd(), newPersonaEvolutionCmd())
	return cmd
}

func newPersonaEvolutionCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "evolution",
		Short: "Manage persona evolution proposals",
	}
	cmd.AddCommand(
		newEvolutionListCmd(),
		newEvolutionApproveCmd(),
		newEvolutionRejectCmd(),
	)
	return cmd
}

func newEvolutionListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List pending evolutions",
		RunE: func(_ *cobra.Command, _ []string) error {
			cl, err := connectDaemon()
			if err != nil {
				return err
			}
			res, err := cl.EvolutionList()
			if err != nil {
				return err
			}
			evos := jsonSlice(res, "evolutions")
			if len(evos) == 0 {
				fmt.Println("No pending evolutions.")
				return nil
			}
			fmt.Printf("%s %s\n", padW("ID", 30), "REASON")
			fmt.Println(strings.Repeat("-", 60))
			for _, e := range evos {
				reason := truncW(jsonStr(e, "Value"), 40)
				fmt.Printf("%s %s\n", padW(jsonStr(e, "Key"), 30), reason)
			}
			return nil
		},
	}
}

func newEvolutionApproveCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "approve <id>",
		Short: "Approve an evolution",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			cl, err := connectDaemon()
			if err != nil {
				return err
			}
			if _, err := cl.EvolutionApprove(args[0]); err != nil {
				return err
			}
			fmt.Println("Evolution approved.")
			return nil
		},
	}
}

func newEvolutionRejectCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "reject <id>",
		Short: "Reject an evolution",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			cl, err := connectDaemon()
			if err != nil {
				return err
			}
			if _, err := cl.EvolutionReject(args[0]); err != nil {
				return err
			}
			fmt.Println("Evolution rejected.")
			return nil
		},
	}
}

func newPersonaListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List profiles with preset status",
		RunE:  runPersonaList,
	}
}

func runPersonaList(_ *cobra.Command, _ []string) error {
	cl, err := connectDaemon()
	if err != nil {
		return err
	}

	res, err := cl.ProfileList()
	if err != nil {
		return fmt.Errorf("list profiles: %w", err)
	}

	profiles := jsonSlice(res, "profiles")
	if len(profiles) == 0 {
		fmt.Println("No profiles found. Run 'kittypaw setup' to create a default profile.")
		return nil
	}

	fmt.Printf("%s %s %s %s\n", padW("ID", 20), padW("DESCRIPTION", 30), padW("STATUS", 12), "PRESET")
	fmt.Println(strings.Repeat("-", 75))

	for _, p := range profiles {
		statusStr := jsonStr(p, "preset_status")
		if statusStr == "" {
			statusStr = "unknown"
		}
		presetStr := jsonStr(p, "preset_id")
		if presetStr == "" {
			presetStr = "-"
		}
		if statusStr == "custom" && presetStr != "-" {
			presetStr += " (modified)"
		}
		desc := truncW(jsonStr(p, "description"), 30)
		fmt.Printf("%s %s %s %s\n", padW(jsonStr(p, "id"), 20), padW(desc, 30), padW(statusStr, 12), presetStr)
	}
	return nil
}

func newPersonaApplyCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "apply <preset-id> [profile-id]",
		Short: "Apply a preset to a profile (default: 'default')",
		Args:  cobra.RangeArgs(1, 2),
		RunE:  runPersonaApply,
	}
}

func runPersonaApply(_ *cobra.Command, args []string) error {
	presetID := args[0]
	profileID := "default"
	if len(args) > 1 {
		profileID = args[1]
	}

	cl, err := connectDaemon()
	if err != nil {
		return err
	}

	if _, err := cl.ProfileActivate(profileID, presetID); err != nil {
		return fmt.Errorf("apply preset: %w", err)
	}

	fmt.Printf("Applied preset %q to profile %q.\n", presetID, profileID)
	return nil
}

// promptPackageConfig prompts the user to configure package settings after install.
// Skips silently if there are no config fields or stdin is not a terminal.
func promptPackageConfig(pm *core.PackageManager, pkg *core.SkillPackage) error {
	if len(pkg.Config) == 0 {
		return nil
	}
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		fmt.Printf("  Tip: run `kittypaw skill config %s` to configure this package.\n", pkg.Meta.ID)
		return nil
	}

	// Load existing values so we can skip already-configured fields.
	existing, _ := pm.GetConfig(pkg.Meta.ID)

	// Count fields that still need input.
	var pending []core.ConfigField
	for _, field := range pkg.Config {
		cur := existing[field.Key]
		if cur != "" && cur != field.Default {
			if field.Source != "" {
				fmt.Printf("  %s: bound from %s\n", field.Key, field.Source)
			}
			continue // already configured
		}
		if !field.Required && cur == field.Default {
			continue // optional with default — fine as-is
		}
		pending = append(pending, field)
	}
	if len(pending) == 0 {
		fmt.Println("  All configuration fields already set.")
		return nil
	}

	fmt.Printf("\nThis package needs %d configuration value(s):\n", len(pending))
	scanner := bufio.NewScanner(os.Stdin)

	for _, field := range pending {
		resolved := field.ResolvedType()
		label := field.Label
		if label == "" {
			label = field.Key
		}

		var value string
		switch resolved {
		case "boolean":
			defHint := "[y/N]"
			if strings.EqualFold(field.Default, "true") {
				defHint = "[Y/n]"
			}
			fmt.Printf("  %s %s: ", label, defHint)
			scanner.Scan()
			input := strings.TrimSpace(scanner.Text())
			if input == "" {
				value = field.Default
			} else {
				switch strings.ToLower(input) {
				case "y", "yes", "true", "1":
					value = "true"
				default:
					value = "false"
				}
			}

		case "select":
			fmt.Printf("  %s:\n", label)
			for i, opt := range field.Options {
				marker := " "
				if opt == field.Default {
					marker = "*"
				}
				fmt.Printf("    %s %d) %s\n", marker, i+1, opt)
			}
			fmt.Printf("  Choose [1-%d]: ", len(field.Options))
			scanner.Scan()
			input := strings.TrimSpace(scanner.Text())
			if input == "" && field.Default != "" {
				value = field.Default
			} else if n, err := strconv.Atoi(input); err == nil && n >= 1 && n <= len(field.Options) {
				value = field.Options[n-1]
			} else {
				fmt.Println("    Invalid selection, skipping.")
				continue
			}

		case "secret":
			fmt.Printf("  %s (hidden): ", label)
			raw, err := term.ReadPassword(int(os.Stdin.Fd()))
			fmt.Println()
			if err != nil {
				return fmt.Errorf("read secret: %w", err)
			}
			value = strings.TrimSpace(string(raw))

		default: // string, number
			hint := ""
			if field.Default != "" {
				hint = fmt.Sprintf(" [%s]", field.Default)
			}
			fmt.Printf("  %s%s: ", label, hint)
			scanner.Scan()
			value = strings.TrimSpace(scanner.Text())
			if value == "" {
				value = field.Default
			}
		}

		if value == "" && field.Required {
			fmt.Printf("    Skipped (required — set later with `kittypaw skill config %s %s <value>`).\n", pkg.Meta.ID, field.Key)
			continue
		}
		if value == "" {
			continue
		}

		if err := pm.SetConfig(pkg.Meta.ID, field.Key, value); err != nil {
			fmt.Printf("    Warning: failed to save %s: %v\n", field.Key, err)
		}
	}
	fmt.Println("  Configuration saved.")
	return nil
}

// defaultTenantBase returns the base directory CLI commands should treat as
// the default tenant: ~/.kittypaw/tenants/default/ when the multi-tenant
// layout exists, falling back to ~/.kittypaw/ for fresh installs that have
// not yet been migrated. Centralizing this probe keeps CLI helpers and the
// daemon session looking at the same files — the daemon side has always
// used Session.BaseDir, but multiple CLI helpers were hardcoded to the
// legacy top-level path before this consolidation.
func defaultTenantBase() (string, error) {
	cfgDir, err := core.ConfigDir()
	if err != nil {
		return "", err
	}
	tenantBase := filepath.Join(cfgDir, "tenants", "default")
	if info, statErr := os.Stat(tenantBase); statErr == nil && info.IsDir() {
		return tenantBase, nil
	}
	return cfgDir, nil
}

// localPackageManager returns a PackageManager bound to the default tenant's
// BaseDir so CLI commands see the same packages the daemon does. CLI
// commands that touch packages (list/info/config/install/uninstall) MUST go
// through this helper — the bare `core.NewPackageManager` is baseDir-empty
// and only finds packages at the legacy path, which has been wrong since
// the multi-tenant migration.
func localPackageManager() (*core.PackageManager, error) {
	secrets, err := core.LoadTenantSecrets(core.DefaultTenantID)
	if err != nil {
		return nil, err
	}
	base, err := defaultTenantBase()
	if err != nil {
		return nil, err
	}
	return core.NewPackageManagerFrom(base, secrets), nil
}

// registryClient creates a RegistryClient from config, falling back to DefaultRegistryURL.
func registryClient() (*core.RegistryClient, error) {
	registryURL := core.DefaultRegistryURL

	cfgPath, err := core.ConfigPath()
	if err == nil {
		if cfg, loadErr := core.LoadConfig(cfgPath); loadErr == nil && cfg.Registry.URL != "" {
			registryURL = cfg.Registry.URL
		}
	}

	return core.NewRegistryClient(registryURL)
}

// ---------------------------------------------------------------------------
// memory
// ---------------------------------------------------------------------------

func newMemoryCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "memory",
		Short: "Memory operations",
	}
	cmd.AddCommand(newMemorySearchCmd())
	return cmd
}

func newMemorySearchCmd() *cobra.Command {
	var memLimit int
	cmd := &cobra.Command{
		Use:   "search <query>",
		Short: "Search execution memory",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			cl, err := connectDaemon()
			if err != nil {
				return err
			}
			query := strings.Join(args, " ")
			res, err := cl.MemorySearch(query, memLimit)
			if err != nil {
				return err
			}
			results := jsonSlice(res, "results")
			if len(results) == 0 {
				fmt.Println("No results found.")
				return nil
			}
			fmt.Printf("%s %s %s %s\n", padW("ID", 6), padW("SKILL", 20), padW("DATE", 20), "INPUT")
			fmt.Println(strings.Repeat("-", 80))
			for _, r := range results {
				input := jsonStr(r, "input")
				if input == "" {
					input = jsonStr(r, "skill_name")
				}
				input = truncW(input, 30)
				fmt.Printf("%s %s %s %s\n",
					padW(fmt.Sprintf("%d", jsonInt(r, "id")), 6),
					padW(truncW(jsonStr(r, "skill_name"), 20), 20),
					padW(jsonStr(r, "started_at"), 20),
					input,
				)
			}
			return nil
		},
	}
	cmd.Flags().IntVar(&memLimit, "limit", 20, "number of results")
	return cmd
}

// ---------------------------------------------------------------------------
// channels
// ---------------------------------------------------------------------------

func newChannelsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "channels",
		Short: "Manage messaging channels",
	}
	cmd.AddCommand(newChannelsListCmd())
	return cmd
}

func newChannelsListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List active channels",
		RunE: func(_ *cobra.Command, _ []string) error {
			cl, err := connectDaemon()
			if err != nil {
				return err
			}
			res, err := cl.ChannelsList()
			if err != nil {
				return err
			}
			// The server returns a JSON array; client wraps it under "items".
			channels := jsonSlice(res, "items")
			if len(channels) == 0 {
				fmt.Println("No channels found.")
				return nil
			}
			fmt.Printf("%s %s %s\n", padW("NAME", 20), padW("TYPE", 12), "STATUS")
			fmt.Println(strings.Repeat("-", 50))
			for _, ch := range channels {
				status := "stopped"
				if jsonBool(ch, "running") {
					status = "running"
				}
				fmt.Printf("%s %s %s\n",
					padW(jsonStr(ch, "name"), 20),
					padW(jsonStr(ch, "type"), 12),
					status,
				)
			}
			return nil
		},
	}
}

// ---------------------------------------------------------------------------
// reload
// ---------------------------------------------------------------------------

func newReloadCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "reload",
		Short: "Reload server configuration",
		RunE: func(_ *cobra.Command, _ []string) error {
			cl, err := connectDaemon()
			if err != nil {
				return err
			}
			if _, err := cl.Reload(); err != nil {
				return err
			}
			fmt.Println("Config reloaded.")
			return nil
		},
	}
}

// ---------------------------------------------------------------------------
// reflection
// ---------------------------------------------------------------------------

func newReflectionCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "reflection",
		Short: "Manage reflection system",
	}
	cmd.AddCommand(
		newReflectionListCmd(),
		newReflectionApproveCmd(),
		newReflectionRejectCmd(),
		newReflectionClearCmd(),
		newReflectionRunCmd(),
		newReflectionWeeklyReportCmd(),
	)
	return cmd
}

func newReflectionListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List reflection candidates",
		RunE: func(_ *cobra.Command, _ []string) error {
			cl, err := connectDaemon()
			if err != nil {
				return err
			}
			res, err := cl.ReflectionList()
			if err != nil {
				return err
			}
			candidates := jsonSlice(res, "candidates")
			if len(candidates) == 0 {
				fmt.Println("No reflection candidates found.")
				return nil
			}
			fmt.Printf("%s %s\n", padW("KEY", 30), "VALUE")
			fmt.Println(strings.Repeat("-", 60))
			for _, c := range candidates {
				val := truncW(jsonStr(c, "Value"), 40)
				fmt.Printf("%s %s\n", padW(jsonStr(c, "Key"), 30), val)
			}
			return nil
		},
	}
}

func newReflectionApproveCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "approve <key>",
		Short: "Approve a reflection candidate",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			cl, err := connectDaemon()
			if err != nil {
				return err
			}
			if _, err := cl.ReflectionApprove(args[0]); err != nil {
				return err
			}
			fmt.Println("Pattern approved.")
			return nil
		},
	}
}

func newReflectionRejectCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "reject <key>",
		Short: "Reject a reflection candidate",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			cl, err := connectDaemon()
			if err != nil {
				return err
			}
			if _, err := cl.ReflectionReject(args[0]); err != nil {
				return err
			}
			fmt.Println("Pattern rejected.")
			return nil
		},
	}
}

func newReflectionClearCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "clear",
		Short: "Clear all reflection candidates",
		RunE: func(_ *cobra.Command, _ []string) error {
			cl, err := connectDaemon()
			if err != nil {
				return err
			}
			if _, err := cl.ReflectionClear(); err != nil {
				return err
			}
			fmt.Println("Reflection data cleared.")
			return nil
		},
	}
}

func newReflectionRunCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "run",
		Short: "Trigger reflection cycle",
		RunE: func(_ *cobra.Command, _ []string) error {
			cl, err := connectDaemon()
			if err != nil {
				return err
			}
			if _, err := cl.ReflectionRun(); err != nil {
				return err
			}
			fmt.Println("Reflection cycle triggered.")
			return nil
		},
	}
}

func newReflectionWeeklyReportCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "weekly-report",
		Short: "Show weekly reflection report",
		RunE: func(_ *cobra.Command, _ []string) error {
			cl, err := connectDaemon()
			if err != nil {
				return err
			}
			res, err := cl.WeeklyReport()
			if err != nil {
				return err
			}
			fmt.Println(jsonStr(res, "report"))
			return nil
		},
	}
}

// ---------------------------------------------------------------------------
// Thin Client helpers
// ---------------------------------------------------------------------------

// connectDaemon returns a Client connected to the daemon via DaemonConn.
// Uses --remote flag if set, otherwise auto-discovers/starts local daemon.
func connectDaemon() (*client.Client, error) {
	conn, err := client.NewDaemonConn(flagRemote)
	if err != nil {
		return nil, err
	}
	return conn.Connect()
}

// jsonInt extracts an integer from a map[string]any (JSON numbers are float64).
func jsonInt(m map[string]any, key string) int64 {
	if v, ok := m[key].(float64); ok {
		return int64(v)
	}
	return 0
}

// jsonStr extracts a string from a map[string]any.
func jsonStr(m map[string]any, key string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

// jsonBool extracts a bool from a map[string]any.
func jsonBool(m map[string]any, key string) bool {
	if v, ok := m[key].(bool); ok {
		return v
	}
	return false
}

// jsonSlice extracts a slice of map items from a map[string]any.
func jsonSlice(m map[string]any, key string) []map[string]any {
	arr, ok := m[key].([]any)
	if !ok {
		return nil
	}
	result := make([]map[string]any, 0, len(arr))
	for _, item := range arr {
		if obj, ok := item.(map[string]any); ok {
			result = append(result, obj)
		}
	}
	return result
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// bootstrap discovers every tenant under ~/.kittypaw/tenants/ and opens
// per-tenant dependencies (store, LLM provider, sandbox, MCP, secrets,
// package manager, API token manager).
//
// Before discovery, a legacy ~/.kittypaw layout (config.toml at root, no
// tenants/) is migrated into tenants/default/ via MigrateLegacyLayout so
// v0.x installs upgrade transparently. Discovery fails loudly when no
// tenants are present — a daemon with nothing to route is not useful.
func bootstrap() ([]*server.TenantDeps, error) {
	baseDir, err := core.ConfigDir()
	if err != nil {
		return nil, fmt.Errorf("config dir: %w", err)
	}

	if err := core.MigrateLegacyLayout(baseDir); err != nil {
		return nil, fmt.Errorf("migrate legacy layout: %w", err)
	}

	tenantsRoot := filepath.Join(baseDir, "tenants")
	tenants, err := core.DiscoverTenants(tenantsRoot)
	if err != nil {
		return nil, fmt.Errorf("discover tenants: %w", err)
	}
	if len(tenants) == 0 {
		return nil, fmt.Errorf("no tenants found under %s (run `kittypaw setup` first)", tenantsRoot)
	}

	deps := make([]*server.TenantDeps, 0, len(tenants))
	closeOnErr := func() {
		for _, td := range deps {
			_ = td.Close()
		}
	}

	for _, t := range tenants {
		td, err := server.OpenTenantDeps(t)
		if err != nil {
			closeOnErr()
			return nil, err
		}
		deps = append(deps, td)
	}
	return deps, nil
}

// openStore opens the SQLite store at the default path.
func openStore() (*store.Store, error) {
	dir, err := core.ConfigDir()
	if err != nil {
		return nil, err
	}
	dbDir := filepath.Join(dir, "data")
	if err := os.MkdirAll(dbDir, 0o755); err != nil {
		return nil, err
	}
	dbPath := filepath.Join(dbDir, "kittypaw.db")
	st, err := store.Open(dbPath)
	if err != nil {
		return nil, fmt.Errorf("open store %s: %w", dbPath, err)
	}
	return st, nil
}

// daemonPidPath returns the path to the daemon pid file.
func daemonPidPath() (string, error) {
	dir, err := core.ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "daemon.pid"), nil
}

// processRunning checks whether a pid corresponds to a live process.
func processRunning(pid int) bool {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	// Signal 0 tests existence without sending a signal.
	return proc.Signal(syscall.Signal(0)) == nil
}

// ---------------------------------------------------------------------------
// spinner
// ---------------------------------------------------------------------------

var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

type spinner struct {
	prefix string
	stop   chan struct{}
	done   chan struct{}
}

func newSpinner(prefix string) *spinner {
	return &spinner{
		prefix: prefix,
		stop:   make(chan struct{}),
		done:   make(chan struct{}),
	}
}

func (s *spinner) Start() {
	go func() {
		defer close(s.done)
		fmt.Print("\033[?25l") // hide cursor
		i := 0
		ticker := time.NewTicker(80 * time.Millisecond)
		defer ticker.Stop()
		for {
			fmt.Printf("\r%s%s", s.prefix, spinnerFrames[i%len(spinnerFrames)])
			i++
			select {
			case <-s.stop:
				fmt.Print("\r\033[K\033[?25h") // clear line + show cursor
				return
			case <-ticker.C:
			}
		}
	}()
}

func (s *spinner) Stop() {
	select {
	case <-s.stop:
	default:
		close(s.stop)
	}
	<-s.done
}
