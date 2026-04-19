package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/jinto/kittypaw/client"
	"github.com/jinto/kittypaw/core"
)

type tenantAddFlags struct {
	telegramToken      string
	telegramTokenStdin bool
	adminChatID        string
	isFamily           bool
	llmProvider        string
	llmAPIKey          string
	llmModel           string
	noActivate         bool
}

const tenantEnvBotToken = "KITTYPAW_TELEGRAM_BOT_TOKEN"

func newTenantCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "tenant",
		Short: "Manage multi-tenant workspaces",
		Long:  "Create and inspect tenant workspaces under ~/.kittypaw/tenants/. Each tenant owns its own DB, secrets, skills, and channel bindings.",
	}
	cmd.AddCommand(newTenantAddCmd())
	cmd.AddCommand(newTenantRemoveCmd())
	return cmd
}

func newTenantAddCmd() *cobra.Command {
	f := &tenantAddFlags{}
	cmd := &cobra.Command{
		Use:   "add <name>",
		Short: "Provision a new tenant directory",
		Long: `Create a new tenant under ~/.kittypaw/tenants/<name>/ with its own
config.toml, data/, skills/, profiles/, and packages/ subtrees.

Bot-token sources (highest priority wins):
  1. --telegram-bot-token-stdin  (reads from stdin — recommended)
  2. $` + tenantEnvBotToken + `
  3. --telegram-bot-token        (visible in process list; prints a warning)

If a daemon is already running, the tenant is hot-activated: channels spawn
and dispatch begins without a restart (AC-U3). Pass --no-activate to skip
the activation RPC and only stage files on disk.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runTenantAdd(args[0], f, cmd.InOrStdin(), cmd.OutOrStdout(), cmd.ErrOrStderr())
		},
	}
	cmd.Flags().StringVar(&f.telegramToken, "telegram-bot-token", "", "Telegram bot token (visible in ps; prefer --telegram-bot-token-stdin)")
	cmd.Flags().BoolVar(&f.telegramTokenStdin, "telegram-bot-token-stdin", false, "Read Telegram bot token from stdin")
	cmd.Flags().StringVar(&f.adminChatID, "admin-chat-id", "", "Telegram admin chat ID (auto-detected from getUpdates when omitted)")
	cmd.Flags().BoolVar(&f.isFamily, "is-family", false, "Mark this tenant as the family coordinator (no channels)")
	cmd.Flags().StringVar(&f.llmProvider, "llm-provider", "", "LLM provider (anthropic|openai|local)")
	cmd.Flags().StringVar(&f.llmAPIKey, "llm-api-key", "", "LLM API key")
	cmd.Flags().StringVar(&f.llmModel, "llm-model", "", "LLM model name")
	cmd.Flags().BoolVar(&f.noActivate, "no-activate", false, "Stage files only; skip hot-activation against a running daemon")
	return cmd
}

// Empty return means no token configured — family/no-token branches are validated by the caller.
func resolveTenantToken(f *tenantAddFlags, stdin io.Reader, stderr io.Writer) (string, error) {
	if f.telegramTokenStdin {
		b, err := io.ReadAll(stdin)
		if err != nil {
			return "", fmt.Errorf("read token from stdin: %w", err)
		}
		token := strings.TrimSpace(string(b))
		if token == "" {
			return "", errors.New("--telegram-bot-token-stdin was set but stdin is empty")
		}
		return token, nil
	}
	if env := strings.TrimSpace(os.Getenv(tenantEnvBotToken)); env != "" {
		if f.telegramToken != "" {
			_, _ = fmt.Fprintf(stderr, "warning: --telegram-bot-token ignored ($%s is set)\n", tenantEnvBotToken)
		}
		return env, nil
	}
	if f.telegramToken != "" {
		_, _ = fmt.Fprintln(stderr, "warning: bot token passed via flag is visible in the process list; prefer --telegram-bot-token-stdin")
		return f.telegramToken, nil
	}
	return "", nil
}

func runTenantAdd(name string, f *tenantAddFlags, stdin io.Reader, stdout, stderr io.Writer) error {
	token, err := resolveTenantToken(f, stdin, stderr)
	if err != nil {
		return err
	}

	if f.isFamily && token != "" {
		return fmt.Errorf("--is-family and a telegram bot token are mutually exclusive")
	}
	if !f.isFamily && token == "" {
		return fmt.Errorf("a Telegram bot token is required for non-family tenants (set --telegram-bot-token-stdin, $%s, or --telegram-bot-token, or pass --is-family)", tenantEnvBotToken)
	}
	if token != "" && !core.ValidateTelegramToken(token) {
		return errors.New("invalid telegram bot token format")
	}

	cfgDir, err := core.ConfigDir()
	if err != nil {
		return fmt.Errorf("resolve config dir: %w", err)
	}
	tenantsDir := filepath.Join(cfgDir, "tenants")

	chatID := f.adminChatID
	if token != "" && chatID == "" {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		detected, derr := core.FetchTelegramChatID(ctx, token)
		cancel()
		if derr == nil {
			chatID = detected
		} else {
			_, _ = fmt.Fprintf(stderr, "info: chat_id auto-detect skipped (%v); pass --admin-chat-id later if needed\n", derr)
		}
	}

	tt, err := core.InitTenant(tenantsDir, name, core.TenantOpts{
		TelegramToken: token,
		AdminChatID:   chatID,
		IsFamily:      f.isFamily,
		LLMProvider:   f.llmProvider,
		LLMAPIKey:     f.llmAPIKey,
		LLMModel:      f.llmModel,
	})
	if err != nil {
		return err
	}

	_, _ = fmt.Fprintf(stdout, "tenant %q created at %s\n", tt.ID, tt.BaseDir)

	if f.noActivate {
		_, _ = fmt.Fprintln(stdout, "Skipped activation (--no-activate). Restart 'kittypaw serve' or re-run without the flag to activate.")
		return nil
	}
	if err := activateTenantOnDaemon(tt.ID, stdout, stderr); err != nil {
		// Don't fail the whole command — files are already on disk; the user
		// can recover with a daemon restart. Surface the error clearly so
		// they know hot-activate didn't take.
		_, _ = fmt.Fprintf(stderr, "warning: hot-activation failed: %v\n", err)
		_, _ = fmt.Fprintln(stdout, "Restart 'kittypaw serve' to activate, or re-run `kittypaw tenant add` after starting the daemon.")
	}
	return nil
}

func newTenantRemoveCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "remove <name>",
		Short: "Decommission a tenant (safe, reversible via .trash/)",
		Long: `Decommission a tenant safely:

  1. If a daemon is running, deactivate the tenant (stops channels, drains
     sessions) via admin RPC — no restart required.
  2. If the removed tenant is personal and a family tenant exists, delete
     the matching [share.<name>] stanza from family/config.toml so stale
     allowlist entries don't re-grant access if the name is re-used later.
  3. Move ~/.kittypaw/tenants/<name>/ to ~/.kittypaw/.trash/<name>-<ts>/.
     The move is atomic (same partition) and reversible by manual rename.
  4. Print a warning that the Telegram bot token is still valid — the admin
     must revoke it via @BotFather /revoke.

The command aborts BEFORE touching the family config or the tenant
directory if the daemon returns an error, so a failed step 1 leaves the
tenant fully runnable. Re-running after the daemon reports healthy
completes the decommission.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runTenantRemove(args[0], cmd.OutOrStdout(), cmd.ErrOrStderr())
		},
	}
	return cmd
}

func runTenantRemove(name string, stdout, stderr io.Writer) error {
	if err := core.ValidateTenantID(name); err != nil {
		return err
	}

	cfgDir, err := core.ConfigDir()
	if err != nil {
		return fmt.Errorf("resolve config dir: %w", err)
	}
	tenantsDir := filepath.Join(cfgDir, "tenants")
	tenantDir := filepath.Join(tenantsDir, name)

	info, err := os.Stat(tenantDir)
	if err != nil || !info.IsDir() {
		return fmt.Errorf("tenant %q does not exist at %s", name, tenantDir)
	}

	// Load tenant's own config to learn is_family (so we can skip the
	// self-cleanup step and surface the extra warning). A missing
	// config.toml is treated as personal — the worst case is a no-op scrub.
	selfCfg, _ := core.LoadConfig(filepath.Join(tenantDir, "config.toml"))
	removedIsFamily := selfCfg != nil && selfCfg.IsFamily

	if err := deactivateTenantOnDaemon(name, stdout, stderr); err != nil {
		return fmt.Errorf("deactivate on daemon: %w", err)
	}

	if !removedIsFamily {
		if err := scrubFamilyShare(tenantsDir, name, stderr); err != nil {
			return fmt.Errorf("update family config: %w", err)
		}
	}

	trashedPath, err := moveTenantToTrash(cfgDir, tenantsDir, name)
	if err != nil {
		return fmt.Errorf("move to trash: %w", err)
	}

	_, _ = fmt.Fprintf(stdout, "tenant %q decommissioned → %s\n", name, trashedPath)
	_, _ = fmt.Fprintf(stderr, "warning: Telegram bot token for tenant %q is still valid. Revoke via @BotFather /revoke to fully decommission.\n", name)
	if removedIsFamily {
		_, _ = fmt.Fprintln(stderr, "note: family tenant removed — personal tenants will no longer see cross-tenant shares or fanout until a new family is provisioned.")
	}
	return nil
}

// deactivateTenantOnDaemon calls POST /api/v1/admin/tenants/{id}/delete when
// a daemon is running. Absence of a daemon is not an error (AC-RM2 offline
// path); 404 from the daemon means the tenant isn't currently active, which
// is also fine (already decommissioned or never booted with it).
func deactivateTenantOnDaemon(name string, stdout, stderr io.Writer) error {
	conn, err := client.NewDaemonConn("")
	if err != nil {
		// Missing config.toml (pre-onboarding) is treated as offline — the
		// filesystem part of decommission still matters even if the user
		// never booted the daemon with this tenant.
		_, _ = fmt.Fprintf(stdout, "Daemon config unavailable (%v); skipping hot-deactivation.\n", err)
		return nil
	}
	if !conn.IsRunning() {
		_, _ = fmt.Fprintln(stdout, "Daemon is not running; skipping hot-deactivation.")
		return nil
	}

	cl := client.New(conn.BaseURL, conn.APIKey)
	if _, err := cl.TenantRemove(name); err != nil {
		// Treat 404 as benign (already gone). Everything else aborts so the
		// CLI doesn't mutate family config or the filesystem while a real
		// drain error is pending — AC-RM5.
		if strings.Contains(err.Error(), "404") {
			_, _ = fmt.Fprintf(stderr, "info: daemon reports tenant %q not active (already decommissioned?); continuing.\n", name)
			return nil
		}
		return err
	}
	_, _ = fmt.Fprintf(stdout, "tenant %q deactivated on daemon\n", name)
	return nil
}

// scrubFamilyShare removes the [share.<removed>] stanza from the family
// tenant's config.toml. No-op if no family tenant exists (AC-RM4) or the
// stanza is already absent. Uses WriteConfigAtomic so a crash mid-write
// never leaves the file truncated (AC-RM6).
func scrubFamilyShare(tenantsDir, removed string, stderr io.Writer) error {
	tenants, err := core.DiscoverTenants(tenantsDir)
	if err != nil {
		return err
	}
	var family *core.Tenant
	for _, tt := range tenants {
		if tt != nil && tt.Config != nil && tt.Config.IsFamily {
			family = tt
			break
		}
	}
	if family == nil {
		return nil
	}
	if _, ok := family.Config.Share[removed]; !ok {
		return nil
	}
	delete(family.Config.Share, removed)
	cfgPath := filepath.Join(family.BaseDir, "config.toml")
	if err := core.WriteConfigAtomic(family.Config, cfgPath); err != nil {
		return fmt.Errorf("atomic write %s: %w", cfgPath, err)
	}
	_, _ = fmt.Fprintf(stderr, "info: removed [share.%s] from family config at %s\n", removed, cfgPath)
	return nil
}

// moveTenantToTrash renames tenants/<name>/ to .trash/<name>-<ts>/ atomically
// within the same filesystem. On collision (same-second re-runs or prior
// residue) it appends a -2, -3, ... suffix rather than overwriting (AC-RM8).
func moveTenantToTrash(cfgDir, tenantsDir, name string) (string, error) {
	trashDir := filepath.Join(cfgDir, ".trash")
	if err := os.MkdirAll(trashDir, 0o700); err != nil {
		return "", fmt.Errorf("create trash dir: %w", err)
	}
	ts := time.Now().UTC().Format("20060102150405")
	base := filepath.Join(trashDir, name+"-"+ts)
	candidate := base
	for i := 2; ; i++ {
		if _, err := os.Stat(candidate); errors.Is(err, os.ErrNotExist) {
			break
		}
		candidate = fmt.Sprintf("%s-%d", base, i)
	}
	src := filepath.Join(tenantsDir, name)
	if err := os.Rename(src, candidate); err != nil {
		return "", fmt.Errorf("rename %s → %s: %w", src, candidate, err)
	}
	return candidate, nil
}

// activateTenantOnDaemon calls POST /api/v1/admin/tenants if a daemon is
// already running locally. Absence of a daemon is not an error — the user
// may be provisioning offline before first boot — so we fall back to a
// restart hint printed by the caller.
func activateTenantOnDaemon(tenantID string, stdout, stderr io.Writer) error {
	conn, err := client.NewDaemonConn("")
	if err != nil {
		return fmt.Errorf("read daemon config: %w", err)
	}
	if !conn.IsRunning() {
		_, _ = fmt.Fprintln(stdout, "Daemon is not running; start 'kittypaw serve' to activate this tenant.")
		return nil
	}

	cl := client.New(conn.BaseURL, conn.APIKey)
	resp, err := cl.TenantActivate(tenantID)
	if err != nil {
		return err
	}

	channels, _ := resp["channels"].(float64)
	isFamily, _ := resp["is_family"].(bool)
	_, _ = fmt.Fprintf(stdout, "tenant %q activated (channels=%d, is_family=%t)\n",
		tenantID, int(channels), isFamily)
	return nil
}
