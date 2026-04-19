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
}

const tenantEnvBotToken = "KITTYPAW_TELEGRAM_BOT_TOKEN"

func newTenantCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "tenant",
		Short: "Manage multi-tenant workspaces",
		Long:  "Create and inspect tenant workspaces under ~/.kittypaw/tenants/. Each tenant owns its own DB, secrets, skills, and channel bindings.",
	}
	cmd.AddCommand(newTenantAddCmd())
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

Hot-reload is not yet supported; restart 'kittypaw serve' to activate.`,
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
	_, _ = fmt.Fprintln(stdout, "Restart 'kittypaw serve' to activate (hot-reload coming in C4).")
	return nil
}
