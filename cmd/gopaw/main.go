// Command gopaw is the CLI for the GoPaw AI agent platform.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/jinto/gopaw/channel"
	"github.com/jinto/gopaw/core"
	"github.com/jinto/gopaw/engine"
	"github.com/jinto/gopaw/llm"
	"github.com/jinto/gopaw/sandbox"
	"github.com/jinto/gopaw/server"
	"github.com/jinto/gopaw/store"
)

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
// Root
// ---------------------------------------------------------------------------

func newRootCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:          "gopaw",
		Short:        "GoPaw — AI agent platform",
		SilenceUsage: true,
	}

	cmd.PersistentFlags().StringVar(&flagRemote, "remote", "", "connect to remote daemon instead of local")

	cmd.AddCommand(
		newServeCmd(),
		newInitCmd(),
		newChatCmd(),
		newStatusCmd(),
		newSkillsCmd(),
		newRunCmd(),
		newTeachCmd(),
		newConfigCmd(),
		newAgentCmd(),
		newLogCmd(),
		newDaemonCmd(),
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
	cfg, st, provider, sbox, err := bootstrap()
	if err != nil {
		return err
	}
	defer st.Close()

	// Resolve fallback provider if a secondary model is configured.
	var fallback llm.Provider
	if m := cfg.DefaultModel(); m != nil {
		fallback, _ = llm.NewProviderFromModelConfig(*m)
	}

	// Start configured channels.
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	// The server owns the engine session internally.
	srv := server.New(cfg, st, provider, fallback, sbox)

	eventCh := make(chan core.Event, 64)

	// Build channel registry keyed by EventType for response routing.
	channels := make(map[core.EventType]channel.Channel)
	for _, chCfg := range cfg.Channels {
		ch, chErr := channel.FromConfig(chCfg)
		if chErr != nil {
			slog.Warn("skip channel", "type", chCfg.ChannelType, "error", chErr)
			continue
		}
		channels[chCfg.ChannelType.ToEventType()] = ch
		slog.Info("starting channel", "name", ch.Name())
		go func(ch channel.Channel) {
			if startErr := ch.Start(ctx, eventCh); startErr != nil {
				slog.Error("channel stopped", "name", ch.Name(), "error", startErr)
			}
		}(ch)
	}

	// Dispatch channel events to the engine and route responses back.
	go func() {
		for event := range eventCh {
			payload, err := event.ParsePayload()
			if err != nil {
				slog.Warn("channel event: bad payload", "type", event.Type, "error", err)
				continue
			}

			slog.Info("processing channel event",
				"type", event.Type,
				"chat_id", payload.ChatID,
				"from", payload.FromName,
			)

			response, err := srv.ProcessEvent(ctx, event)
			if err != nil {
				slog.Error("channel event: engine error",
					"type", event.Type,
					"chat_id", payload.ChatID,
					"error", err,
				)
				continue
			}

			ch, ok := channels[event.Type]
			if !ok {
				slog.Warn("channel event: no channel for response routing", "type", event.Type)
				continue
			}

			if err := ch.SendResponse(ctx, payload.ChatID, response); err != nil {
				slog.Error("channel event: send response failed",
					"type", event.Type,
					"chat_id", payload.ChatID,
					"error", err,
				)
				// Kakao uses ephemeral action IDs — retry is futile.
				if event.Type != core.EventKakaoTalk {
					if qErr := st.EnqueueResponse(string(event.Type), payload.ChatID, response); qErr != nil {
						slog.Error("channel event: enqueue response failed", "error", qErr)
					}
				}
			}
		}
	}()

	// Background retry loop for failed response deliveries.
	go retryPendingResponses(ctx, st, channels)

	// Start HTTP server (blocks until shutdown signal).
	slog.Info("gopaw serving", "bind", flagBind)
	return srv.ListenAndServe(flagBind)
}

// ---------------------------------------------------------------------------
// init
// ---------------------------------------------------------------------------

func newInitCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "init",
		Short: "Initialize config and data directories",
		RunE:  runInit,
	}
}

func runInit(_ *cobra.Command, _ []string) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}

	gopawDir := filepath.Join(home, ".gopaw")
	dataDir := filepath.Join(gopawDir, "data")
	skillsDir := filepath.Join(gopawDir, "skills")

	for _, dir := range []string{gopawDir, dataDir, skillsDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("create %s: %w", dir, err)
		}
		fmt.Printf("  directory: %s\n", dir)
	}

	configPath := filepath.Join(gopawDir, "config.toml")
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		if err := os.WriteFile(configPath, []byte(defaultConfigTOML), 0o600); err != nil {
			return fmt.Errorf("write config: %w", err)
		}
		fmt.Printf("  created:   %s\n", configPath)
	} else {
		fmt.Printf("  exists:    %s\n", configPath)
	}

	fmt.Println("\ngopaw initialized. Edit config.toml to set your LLM API key.")
	return nil
}

const defaultConfigTOML = `# GoPaw configuration
# See https://github.com/jinto/gopaw for documentation.

[llm]
provider = "anthropic"
api_key  = ""
model    = "claude-sonnet-4-20250514"
max_tokens = 4096

[sandbox]
timeout_secs   = 30
memory_limit_mb = 64

autonomy_level = "full"

[features]
progressive_retry  = true
context_compaction = true
`

// ---------------------------------------------------------------------------
// chat
// ---------------------------------------------------------------------------

func newChatCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "chat",
		Short: "Interactive chat in terminal",
		RunE:  runChat,
	}
}

func runChat(_ *cobra.Command, _ []string) error {
	cfg, st, provider, sbox, err := bootstrap()
	if err != nil {
		return err
	}
	defer st.Close()

	session := &engine.Session{
		Provider: provider,
		Sandbox:  sbox,
		Store:    st,
		Config:   cfg,
	}

	ctx := context.Background()
	scanner := bufio.NewScanner(os.Stdin)

	fmt.Println("GoPaw interactive chat (Ctrl-D to exit)")
	fmt.Println()

	for {
		fmt.Print("you> ")
		if !scanner.Scan() {
			break
		}
		text := strings.TrimSpace(scanner.Text())
		if text == "" {
			continue
		}

		event := desktopEvent(text)
		resp, err := session.Run(ctx, event, nil)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			continue
		}
		fmt.Printf("paw> %s\n\n", resp)
	}

	if err := scanner.Err(); err != nil {
		return err
	}
	fmt.Println()
	return nil
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
	st, err := openStore()
	if err != nil {
		return err
	}
	defer st.Close()

	stats, err := st.TodayStats()
	if err != nil {
		return fmt.Errorf("query stats: %w", err)
	}

	fmt.Println("Today's execution stats")
	fmt.Println("-----------------------")
	fmt.Printf("  Total runs:   %d\n", stats.TotalRuns)
	fmt.Printf("  Successful:   %d\n", stats.Successful)
	fmt.Printf("  Failed:       %d\n", stats.Failed)
	fmt.Printf("  Auto-retries: %d\n", stats.AutoRetries)
	fmt.Printf("  Total tokens: %d\n", stats.TotalTokens)
	return nil
}

// ---------------------------------------------------------------------------
// skills
// ---------------------------------------------------------------------------

func newSkillsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "skills",
		Short: "Manage skills",
	}
	cmd.AddCommand(
		newSkillsListCmd(),
		newSkillsDisableCmd(),
		newSkillsDeleteCmd(),
	)
	return cmd
}

func newSkillsListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all skills",
		RunE:  runSkillsList,
	}
}

func runSkillsList(_ *cobra.Command, _ []string) error {
	skills, err := core.LoadAllSkills()
	if err != nil {
		return fmt.Errorf("load skills: %w", err)
	}
	if len(skills) == 0 {
		fmt.Println("No skills found.")
		return nil
	}

	fmt.Printf("%-20s %-40s %-8s %s\n", "NAME", "DESCRIPTION", "ENABLED", "TRIGGER")
	fmt.Println(strings.Repeat("-", 80))
	for _, s := range skills {
		enabled := "yes"
		if !s.Skill.Enabled {
			enabled = "no"
		}
		desc := s.Skill.Description
		if len(desc) > 38 {
			desc = desc[:38] + ".."
		}
		fmt.Printf("%-20s %-40s %-8s %s\n", s.Skill.Name, desc, enabled, s.Skill.Trigger.Type)
	}
	return nil
}

func newSkillsDisableCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "disable <name>",
		Short: "Disable a skill",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			if err := core.DisableSkill(args[0]); err != nil {
				return err
			}
			fmt.Printf("Skill %q disabled.\n", args[0])
			return nil
		},
	}
}

func newSkillsDeleteCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "delete <name>",
		Short: "Delete a skill",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			if err := core.DeleteSkill(args[0]); err != nil {
				return err
			}
			fmt.Printf("Skill %q deleted.\n", args[0])
			return nil
		},
	}
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

	skill, code, err := core.LoadSkill(name)
	if err != nil {
		return fmt.Errorf("load skill: %w", err)
	}
	if skill == nil {
		return fmt.Errorf("skill %q not found", name)
	}

	if flagDryRun {
		fmt.Printf("Skill:       %s\n", skill.Name)
		fmt.Printf("Description: %s\n", skill.Description)
		fmt.Printf("Trigger:     %s\n", skill.Trigger.Type)
		fmt.Printf("Code length: %d bytes\n", len(code))
		fmt.Println("\n(dry run — not executed)")
		return nil
	}

	cfg, st, provider, sbox, err := bootstrap()
	if err != nil {
		return err
	}
	defer st.Close()

	session := &engine.Session{
		Provider: provider,
		Sandbox:  sbox,
		Store:    st,
		Config:   cfg,
	}

	event := desktopEvent(fmt.Sprintf("/run %s", name))
	ctx := context.Background()

	resp, err := session.Run(ctx, event, nil)
	if err != nil {
		return fmt.Errorf("run skill: %w", err)
	}
	fmt.Println(resp)
	return nil
}

// ---------------------------------------------------------------------------
// teach
// ---------------------------------------------------------------------------

func newTeachCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "teach <description...>",
		Short: "Teach a new skill from description",
		Args:  cobra.MinimumNArgs(1),
		RunE:  runTeach,
	}
}

func runTeach(_ *cobra.Command, args []string) error {
	description := strings.Join(args, " ")

	cfg, st, provider, sbox, err := bootstrap()
	if err != nil {
		return err
	}
	defer st.Close()

	session := &engine.Session{
		Provider: provider,
		Sandbox:  sbox,
		Store:    st,
		Config:   cfg,
	}

	event := desktopEvent(fmt.Sprintf("/teach %s", description))
	ctx := context.Background()

	resp, err := session.Run(ctx, event, nil)
	if err != nil {
		return fmt.Errorf("teach skill: %w", err)
	}
	fmt.Println(resp)
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
	st, err := openStore()
	if err != nil {
		return err
	}
	defer st.Close()

	agents, err := st.ListAgents()
	if err != nil {
		return fmt.Errorf("list agents: %w", err)
	}
	if len(agents) == 0 {
		fmt.Println("No agents found.")
		return nil
	}

	fmt.Printf("%-30s %-6s %s\n", "AGENT ID", "TURNS", "UPDATED")
	fmt.Println(strings.Repeat("-", 60))
	for _, a := range agents {
		fmt.Printf("%-30s %-6d %s\n", a.AgentID, a.TurnCount, a.UpdatedAt)
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
	st, err := openStore()
	if err != nil {
		return err
	}
	defer st.Close()

	var records []store.ExecutionRecord
	if flagSkill != "" {
		records, err = st.SearchExecutions(flagSkill, flagLimit)
	} else {
		records, err = st.RecentExecutions(flagLimit)
	}
	if err != nil {
		return fmt.Errorf("query executions: %w", err)
	}
	if len(records) == 0 {
		fmt.Println("No execution records found.")
		return nil
	}

	fmt.Printf("%-5s %-20s %-20s %-7s %s\n", "ID", "SKILL", "STARTED", "STATUS", "DURATION")
	fmt.Println(strings.Repeat("-", 80))
	for _, r := range records {
		status := "OK"
		if !r.Success {
			status = "FAIL"
		}
		duration := strconv.FormatInt(r.DurationMs, 10) + "ms"
		fmt.Printf("%-5d %-20s %-20s %-7s %s\n", r.ID, r.SkillName, r.StartedAt, status, duration)
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

	// Check if already running.
	if pid, ok := readPid(pidPath); ok {
		if processRunning(pid) {
			return fmt.Errorf("daemon already running (pid %d)", pid)
		}
	}

	// Re-exec ourselves with "serve" in the background.
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("find executable: %w", err)
	}

	proc := exec.Command(exe, "serve")
	proc.Stdout = nil
	proc.Stderr = nil
	proc.SysProcAttr = &syscall.SysProcAttr{Setsid: true}

	if err := proc.Start(); err != nil {
		return fmt.Errorf("start daemon: %w", err)
	}

	if err := os.WriteFile(pidPath, []byte(strconv.Itoa(proc.Process.Pid)), 0o644); err != nil {
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

	pid, ok := readPid(pidPath)
	if !ok {
		fmt.Println("No daemon pid file found.")
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

	pid, ok := readPid(pidPath)
	if !ok {
		fmt.Println("Daemon is not running (no pid file).")
		return nil
	}

	if processRunning(pid) {
		fmt.Printf("Daemon is running (pid %d).\n", pid)
	} else {
		fmt.Printf("Daemon is not running (stale pid %d).\n", pid)
		os.Remove(pidPath)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Response retry
// ---------------------------------------------------------------------------

// retryPendingResponses periodically retries failed response deliveries.
func retryPendingResponses(ctx context.Context, st *store.Store, channels map[core.EventType]channel.Channel) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	cleanupTicker := time.NewTicker(1 * time.Hour)
	defer cleanupTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			pending, err := st.DequeuePendingResponses(10)
			if err != nil {
				slog.Warn("retry: dequeue failed", "error", err)
				continue
			}
			for _, p := range pending {
				ch, ok := channels[core.EventType(p.EventType)]
				if !ok {
					// Channel no longer configured — drop the entry.
					_ = st.MarkResponseDelivered(p.ID)
					continue
				}
				if err := ch.SendResponse(ctx, p.ChatID, p.Response); err != nil {
					slog.Warn("retry: send failed",
						"id", p.ID, "retry", p.RetryCount, "error", err)
					if kept, rErr := st.IncrementResponseRetry(p.ID); rErr != nil {
						slog.Error("retry: increment failed", "id", p.ID, "error", rErr)
					} else if !kept {
						slog.Warn("retry: max retries exceeded, dropping", "id", p.ID)
					}
				} else {
					slog.Info("retry: delivered pending response",
						"id", p.ID, "chat_id", p.ChatID)
					_ = st.MarkResponseDelivered(p.ID)
				}
			}
		case <-cleanupTicker.C:
			if n, err := st.CleanupExpiredResponses(24); err != nil {
				slog.Warn("retry: cleanup failed", "error", err)
			} else if n > 0 {
				slog.Info("retry: cleaned up expired responses", "count", n)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// bootstrap loads config, opens the store, creates the LLM provider and sandbox.
func bootstrap() (*core.Config, *store.Store, llm.Provider, *sandbox.Sandbox, error) {
	cfgPath, err := core.ConfigPath()
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("config path: %w", err)
	}

	cfg, err := core.LoadConfig(cfgPath)
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("load config: %w", err)
	}

	st, err := openStore()
	if err != nil {
		return nil, nil, nil, nil, err
	}

	provider, err := llm.NewProviderFromConfig(cfg.LLM)
	if err != nil {
		st.Close()
		return nil, nil, nil, nil, fmt.Errorf("create llm provider: %w", err)
	}

	sbox := sandbox.New(cfg.Sandbox)

	return cfg, st, provider, sbox, nil
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
	dbPath := filepath.Join(dbDir, "gopaw.db")
	st, err := store.Open(dbPath)
	if err != nil {
		return nil, fmt.Errorf("open store %s: %w", dbPath, err)
	}
	return st, nil
}

// desktopEvent constructs a core.Event as if typed in the terminal.
func desktopEvent(text string) core.Event {
	payload, _ := json.Marshal(core.ChatPayload{
		ChatID: "cli",
		Text:   text,
	})
	return core.Event{
		Type:    core.EventDesktop,
		Payload: payload,
	}
}

// daemonPidPath returns the path to the daemon pid file.
func daemonPidPath() (string, error) {
	dir, err := core.ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "daemon.pid"), nil
}

// readPid reads a pid from a file. Returns (0, false) if the file doesn't exist
// or cannot be parsed.
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

// processRunning checks whether a pid corresponds to a live process.
func processRunning(pid int) bool {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	// Signal 0 tests existence without sending a signal.
	return proc.Signal(syscall.Signal(0)) == nil
}
