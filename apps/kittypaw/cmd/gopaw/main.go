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

	"github.com/jinto/gopaw/client"
	"github.com/jinto/gopaw/core"
	"github.com/jinto/gopaw/engine"
	"github.com/jinto/gopaw/llm"
	mcpreg "github.com/jinto/gopaw/mcp"
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
		newPackagesCmd(),
		newPersonaCmd(),
		newSuggestionsCmd(),
		newFixesCmd(),
		newReflectionCmd(),
		newMemoryCmd(),
		newChannelsCmd(),
		newReloadCmd(),
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

	// Connect to configured MCP servers.
	var mcpReg *mcpreg.Registry
	if len(cfg.MCPServers) > 0 {
		if err := mcpreg.ValidateConfig(cfg.MCPServers); err != nil {
			return fmt.Errorf("MCP config: %w", err)
		}
		mcpReg = mcpreg.NewRegistry(cfg.MCPServers)
		connectCtx, connectCancel := context.WithTimeout(context.Background(), 15*time.Second)
		if errs := mcpReg.ConnectAll(connectCtx); len(errs) > 0 {
			slog.Warn("some MCP servers failed to connect", "failures", len(errs))
		}
		connectCancel()
	}

	// The server owns the engine session, channel spawner, and dispatch loop.
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	srv := server.New(cfg, st, provider, fallback, sbox, mcpReg)
	srv.StartChannels(ctx, cfg.Channels)

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

	// Create default profile with SOUL.md.
	if err := core.EnsureDefaultProfile(gopawDir); err != nil {
		return fmt.Errorf("ensure default profile: %w", err)
	}
	fmt.Printf("  profile:   %s\n", filepath.Join(gopawDir, "profiles", "default", "SOUL.md"))

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
	conn, err := client.NewDaemonConn(flagRemote)
	if err != nil {
		return err
	}
	// Ensure daemon is running (auto-starts if needed).
	if _, err := conn.Connect(); err != nil {
		return err
	}

	ctx := context.Background()
	cs, err := client.DialChat(ctx, conn.WebSocketURL(), conn.APIKey)
	if err != nil {
		return err
	}
	defer cs.Close()

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

		fmt.Print("paw> ")
		err := cs.Send(text, client.ChatOptions{
			OnToken: func(t string) { fmt.Print(t) },
			OnDone:  func(_ string, _ *int64) { fmt.Print("\n\n") },
			OnError: func(msg string) { fmt.Fprintf(os.Stderr, "\nerror: %s\n", msg) },
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
		}
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
// skills
// ---------------------------------------------------------------------------

func newSkillsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "skills",
		Short: "Manage skills",
	}
	cmd.AddCommand(
		newSkillsListCmd(),
		newSkillsEnableCmd(),
		newSkillsDisableCmd(),
		newSkillsDeleteCmd(),
		newSkillsExplainCmd(),
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
	cl, err := connectDaemon()
	if err != nil {
		return err
	}

	res, err := cl.Skills()
	if err != nil {
		return fmt.Errorf("load skills: %w", err)
	}

	skills := jsonSlice(res, "skills")
	if len(skills) == 0 {
		fmt.Println("No skills found.")
		return nil
	}

	fmt.Printf("%-20s %-40s %-8s %s\n", "NAME", "DESCRIPTION", "ENABLED", "TRIGGER")
	fmt.Println(strings.Repeat("-", 80))
	for _, s := range skills {
		enabled := "yes"
		if !jsonBool(s, "enabled") {
			enabled = "no"
		}
		desc := jsonStr(s, "description")
		if len(desc) > 38 {
			desc = desc[:38] + ".."
		}
		fmt.Printf("%-20s %-40s %-8s %s\n", jsonStr(s, "name"), desc, enabled, jsonStr(s, "trigger"))
	}
	return nil
}

func newSkillsEnableCmd() *cobra.Command {
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

func newSkillsExplainCmd() *cobra.Command {
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

func newSkillsDisableCmd() *cobra.Command {
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

func newSkillsDeleteCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "delete <name>",
		Short: "Delete a skill",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			cl, err := connectDaemon()
			if err != nil {
				return err
			}
			if _, err := cl.DeleteSkill(args[0]); err != nil {
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

	fmt.Printf("%-30s %-6s %s\n", "AGENT ID", "TURNS", "UPDATED")
	fmt.Println(strings.Repeat("-", 60))
	for _, a := range agents {
		fmt.Printf("%-30s %-6d %s\n", jsonStr(a, "agent_id"), jsonInt(a, "turn_count"), jsonStr(a, "updated_at"))
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

	fmt.Printf("%-5s %-20s %-20s %-7s %s\n", "ID", "SKILL", "STARTED", "STATUS", "DURATION")
	fmt.Println(strings.Repeat("-", 80))
	for _, r := range records {
		status := "OK"
		if !jsonBool(r, "success") {
			status = "FAIL"
		}
		duration := strconv.FormatInt(jsonInt(r, "duration_ms"), 10) + "ms"
		fmt.Printf("%-5d %-20s %-20s %-7s %s\n", jsonInt(r, "id"), jsonStr(r, "skill_name"), jsonStr(r, "started_at"), status, duration)
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
			fmt.Printf("%-30s %s\n", "ID", "REASON")
			fmt.Println(strings.Repeat("-", 60))
			for _, e := range evos {
				reason := jsonStr(e, "Value")
				if len(reason) > 40 {
					reason = reason[:40] + ".."
				}
				fmt.Printf("%-30s %s\n", jsonStr(e, "Key"), reason)
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
		fmt.Println("No profiles found. Run 'gopaw init' to create a default profile.")
		return nil
	}

	fmt.Printf("%-20s %-30s %-12s %s\n", "ID", "DESCRIPTION", "STATUS", "PRESET")
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
		desc := jsonStr(p, "description")
		if len(desc) > 28 {
			desc = desc[:28] + ".."
		}
		fmt.Printf("%-20s %-30s %-12s %s\n", jsonStr(p, "id"), desc, statusStr, presetStr)
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

// ---------------------------------------------------------------------------
// packages
// ---------------------------------------------------------------------------

func newPackagesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "packages",
		Aliases: []string{"pkg"},
		Short:   "Manage skill packages",
	}
	cmd.AddCommand(
		newPkgInstallCmd(),
		newPkgUninstallCmd(),
		newPkgListCmd(),
		newPkgSearchCmd(),
		newPkgInfoCmd(),
		newPkgConfigCmd(),
		newPkgRunCmd(),
	)
	return cmd
}

func newPkgInstallCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "install <path-or-id>",
		Short: "Install a package from a local directory or the registry",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			secrets, err := core.LoadSecrets()
			if err != nil {
				return err
			}
			pm := core.NewPackageManager(secrets)

			arg := args[0]

			// If arg is an existing directory, install locally.
			fi, statErr := os.Stat(arg)
			if statErr != nil && !os.IsNotExist(statErr) {
				return statErr // permission denied, etc.
			}
			if statErr == nil && fi.IsDir() {
				pkg, err := pm.Install(arg)
				if err != nil {
					return err
				}
				fmt.Printf("Installed package %q (%s) v%s\n", pkg.Meta.Name, pkg.Meta.ID, pkg.Meta.Version)
				return nil
			}

			// Otherwise treat as a registry package ID.
			rc, err := registryClient()
			if err != nil {
				return err
			}
			entry, err := rc.FindEntry(arg)
			if err != nil {
				return err
			}

			pkg, err := pm.InstallFromRegistry(rc, *entry)
			if err != nil {
				return err
			}
			fmt.Printf("Installed package %q (%s) v%s from registry\n", pkg.Meta.Name, pkg.Meta.ID, pkg.Meta.Version)
			return nil
		},
	}
}

func newPkgSearchCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "search [query]",
		Short: "Search the package registry",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			rc, err := registryClient()
			if err != nil {
				return err
			}

			var query string
			if len(args) > 0 {
				query = args[0]
			}

			results, err := rc.SearchEntries(query)
			if err != nil {
				return err
			}

			if len(results) == 0 {
				fmt.Println("No packages found.")
				return nil
			}

			fmt.Printf("%-20s %-25s %-10s %s\n", "ID", "NAME", "VERSION", "DESCRIPTION")
			fmt.Println(strings.Repeat("-", 80))
			for _, e := range results {
				desc := e.Description
				if len(desc) > 30 {
					desc = desc[:28] + ".."
				}
				name := e.Name
				if len(name) > 23 {
					name = name[:21] + ".."
				}
				fmt.Printf("%-20s %-25s %-10s %s\n", e.ID, name, e.Version, desc)
			}
			return nil
		},
	}
}

func newPkgInfoCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "info <id>",
		Short: "Show details of an installed package",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			secrets, err := core.LoadSecrets()
			if err != nil {
				return err
			}
			pm := core.NewPackageManager(secrets)

			pkg, _, err := pm.LoadPackage(args[0])
			if err != nil {
				return err
			}

			fmt.Printf("ID:          %s\n", pkg.Meta.ID)
			fmt.Printf("Name:        %s\n", pkg.Meta.Name)
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
				cfg, _ := pm.GetConfig(args[0])
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

			return nil
		},
	}
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

func newPkgUninstallCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "uninstall <id>",
		Short: "Uninstall a package",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			secrets, err := core.LoadSecrets()
			if err != nil {
				return err
			}
			pm := core.NewPackageManager(secrets)
			if err := pm.Uninstall(args[0]); err != nil {
				return err
			}
			fmt.Printf("Package %q uninstalled.\n", args[0])
			return nil
		},
	}
}

func newPkgListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List installed packages",
		RunE: func(_ *cobra.Command, _ []string) error {
			secrets, err := core.LoadSecrets()
			if err != nil {
				return err
			}
			pm := core.NewPackageManager(secrets)
			packages, err := pm.ListInstalled()
			if err != nil {
				return err
			}
			if len(packages) == 0 {
				fmt.Println("No packages installed.")
				return nil
			}
			fmt.Printf("%-20s %-30s %-10s %s\n", "ID", "NAME", "VERSION", "CRON")
			fmt.Println(strings.Repeat("-", 75))
			for _, p := range packages {
				cronStr := p.Meta.Cron
				if cronStr == "" {
					cronStr = "-"
				}
				name := p.Meta.Name
				if len(name) > 28 {
					name = name[:28] + ".."
				}
				fmt.Printf("%-20s %-30s %-10s %s\n", p.Meta.ID, name, p.Meta.Version, cronStr)
			}
			return nil
		},
	}
}

func newPkgConfigCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config <id> [key] [value]",
		Short: "Get or set package configuration",
		Args:  cobra.RangeArgs(1, 3),
		RunE: func(_ *cobra.Command, args []string) error {
			secrets, err := core.LoadSecrets()
			if err != nil {
				return err
			}
			pm := core.NewPackageManager(secrets)

			id := args[0]
			if len(args) == 1 {
				// Show all config.
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
				// Set config.
				return pm.SetConfig(id, args[1], args[2])
			}
			return fmt.Errorf("usage: gopaw packages config <id> [key value]")
		},
	}
	return cmd
}

func newPkgRunCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "run <id>",
		Short: "Run a package manually",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			secrets, err := core.LoadSecrets()
			if err != nil {
				return err
			}
			pm := core.NewPackageManager(secrets)
			pkg, code, err := pm.LoadPackage(args[0])
			if err != nil {
				return err
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

			payload, _ := json.Marshal(core.ChatPayload{
				Text:   "skill:pkg:" + pkg.Meta.ID,
				ChatID: "cli",
			})
			event := core.Event{
				Type:    core.EventDesktop,
				Payload: payload,
			}

			_ = code // code is available but session.Run loads it via skill matching
			resp, err := session.Run(context.Background(), event, nil)
			if err != nil {
				return fmt.Errorf("run package %q: %w", args[0], err)
			}
			fmt.Println(resp)
			return nil
		},
	}
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
			fmt.Printf("%-6s %-20s %-20s %s\n", "ID", "SKILL", "DATE", "INPUT")
			fmt.Println(strings.Repeat("-", 80))
			for _, r := range results {
				input := jsonStr(r, "input")
				if input == "" {
					input = jsonStr(r, "skill_name")
				}
				if len(input) > 30 {
					input = input[:30] + ".."
				}
				fmt.Printf("%-6d %-20s %-20s %s\n",
					jsonInt(r, "id"),
					jsonStr(r, "skill_name"),
					jsonStr(r, "started_at"),
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
			fmt.Printf("%-20s %-12s %s\n", "NAME", "TYPE", "STATUS")
			fmt.Println(strings.Repeat("-", 50))
			for _, ch := range channels {
				status := "stopped"
				if jsonBool(ch, "running") {
					status = "running"
				}
				fmt.Printf("%-20s %-12s %s\n",
					jsonStr(ch, "name"),
					jsonStr(ch, "type"),
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
// suggestions
// ---------------------------------------------------------------------------

func newSuggestionsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "suggestions",
		Short: "Manage skill suggestions",
	}
	cmd.AddCommand(
		newSuggestionsListCmd(),
		newSuggestionsAcceptCmd(),
		newSuggestionsDismissCmd(),
	)
	return cmd
}

func newSuggestionsListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List pending suggestions",
		RunE: func(_ *cobra.Command, _ []string) error {
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
			fmt.Printf("%-20s %s\n", "SKILL_ID", "DESCRIPTION")
			fmt.Println(strings.Repeat("-", 60))
			for _, s := range items {
				desc := jsonStr(s, "description")
				if len(desc) > 50 {
					desc = desc[:50] + ".."
				}
				fmt.Printf("%-20s %s\n", jsonStr(s, "skill_id"), desc)
			}
			return nil
		},
	}
}

func newSuggestionsAcceptCmd() *cobra.Command {
	return &cobra.Command{
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
	}
}

func newSuggestionsDismissCmd() *cobra.Command {
	return &cobra.Command{
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
	}
}

// ---------------------------------------------------------------------------
// fixes
// ---------------------------------------------------------------------------

func newFixesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "fixes",
		Short: "Manage skill fixes",
	}
	cmd.AddCommand(
		newFixesListCmd(),
		newFixesApproveCmd(),
	)
	return cmd
}

func newFixesListCmd() *cobra.Command {
	return &cobra.Command{
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
			fmt.Printf("%-6s %-8s %-20s %s\n", "ID", "APPLIED", "DATE", "ERROR")
			fmt.Println(strings.Repeat("-", 60))
			for _, f := range fixes {
				applied := "no"
				if jsonBool(f, "applied") {
					applied = "yes"
				}
				errMsg := jsonStr(f, "error_message")
				if len(errMsg) > 30 {
					errMsg = errMsg[:30] + ".."
				}
				fmt.Printf("%-6d %-8s %-20s %s\n", jsonInt(f, "id"), applied, jsonStr(f, "created_at"), errMsg)
			}
			return nil
		},
	}
}

func newFixesApproveCmd() *cobra.Command {
	return &cobra.Command{
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
			fmt.Printf("%-30s %s\n", "KEY", "VALUE")
			fmt.Println(strings.Repeat("-", 60))
			for _, c := range candidates {
				val := jsonStr(c, "Value")
				if len(val) > 40 {
					val = val[:40] + ".."
				}
				fmt.Printf("%-30s %s\n", jsonStr(c, "Key"), val)
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
