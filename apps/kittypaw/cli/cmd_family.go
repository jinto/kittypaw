package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/jinto/kittypaw/core"
)

type familyInitFlags struct {
	max      int
	noFamily bool
}

type memberStatus string

const (
	statusOK              memberStatus = "ok"
	statusSkippedExisting memberStatus = "skipped_existing"
	statusFailed          memberStatus = "failed"
)

type memberEntry struct {
	Name   string
	Reason string
	Status memberStatus
}

type seenSet struct {
	tenants map[string]struct{}
	tokens  map[string]string // token → tenantID (for dedup error messages)
}

func newFamilyCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "family",
		Short: "Manage the family (household) tenants",
		Long:  "Bootstrap and manage the household's tenants: one personal tenant per member plus an optional family coordinator.",
	}
	cmd.AddCommand(newFamilyInitCmd())
	return cmd
}

func newFamilyInitCmd() *cobra.Command {
	f := &familyInitFlags{}
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Interactively onboard an entire household in one go",
		Long: `Walk through the family one member at a time — prompting for each member's
name, Telegram bot token, and admin chat_id — and provision a personal tenant
per member under ~/.kittypaw/tenants/. A shared "family" coordinator tenant is
created at the end (skip with --no-family).

This is the bulk equivalent of running "kittypaw tenant add" N times. It reuses
the same staging→rename atomic provisioning and, if a daemon is already running,
hot-activates each tenant without a restart.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, cancel := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer cancel()
			return runFamilyInit(ctx, f, isTTY(),
				cmd.InOrStdin(), cmd.OutOrStdout(), cmd.ErrOrStderr())
		},
	}
	cmd.Flags().IntVar(&f.max, "max", 10, "maximum number of members to onboard before stopping")
	cmd.Flags().BoolVar(&f.noFamily, "no-family", false, "skip creation of the shared family tenant")
	return cmd
}

const botFatherHint = `
Before starting, create one Telegram bot per family member:
  1. Open Telegram and message @BotFather → /newbot
  2. Pick a name and username; BotFather replies with a token like 12345:ABC...
  3. Message YOUR new bot once (say "hi") so it can read your chat_id

You'll be asked for: member name → bot token → admin chat_id, per member.
Leave the name blank to finish.
`

// runFamilyInit is the pure entry point so tests can inject stdin/stdout.
// interactive is threaded separately because go-isatty needs the real
// os.Stdin fd, which a test can't synthesize.
func runFamilyInit(ctx context.Context, f *familyInitFlags, interactive bool,
	stdin io.Reader, stdout, stderr io.Writer) error {
	if !interactive {
		return errors.New("kittypaw family init is interactive only — rerun in a real terminal")
	}

	cfgDir, err := core.ConfigDir()
	if err != nil {
		return fmt.Errorf("resolve config dir: %w", err)
	}
	tenantsDir := filepath.Join(cfgDir, "tenants")

	seen, err := scanExistingTenants(tenantsDir)
	if err != nil {
		return fmt.Errorf("scan tenants: %w", err)
	}

	_, _ = fmt.Fprint(stdout, botFatherHint)

	reader := bufio.NewReader(stdin)
	entries := promptMembers(ctx, reader, stdout, stderr, tenantsDir, f.max, seen)

	if !f.noFamily {
		if err := ctx.Err(); err == nil {
			entries = append(entries, createFamilyTenant(tenantsDir, seen, stdout, stderr))
		}
	}

	failed := printSummary(entries, stdout)
	if failed > 0 {
		return fmt.Errorf("family init finished with %d failed member(s)", failed)
	}
	return nil
}

// printSummary renders the OK/SKIPPED/FAILED buckets and returns the count
// of failed entries. skipped_existing does not count — an admin re-running
// the wizard to add one more person should not see a red exit code from
// already-onboarded tenants.
func printSummary(entries []memberEntry, stdout io.Writer) int {
	var ok, skipped, failed []memberEntry
	for _, e := range entries {
		switch e.Status {
		case statusOK:
			ok = append(ok, e)
		case statusSkippedExisting:
			skipped = append(skipped, e)
		case statusFailed:
			failed = append(failed, e)
		}
	}

	_, _ = fmt.Fprintln(stdout)
	_, _ = fmt.Fprintln(stdout, "=== Family init summary ===")
	if len(ok) > 0 {
		_, _ = fmt.Fprintf(stdout, "OK:      %s\n", joinNames(ok))
	}
	if len(skipped) > 0 {
		_, _ = fmt.Fprintf(stdout, "SKIPPED: %s\n", joinNames(skipped))
	}
	if len(failed) > 0 {
		_, _ = fmt.Fprintln(stdout, "FAILED:")
		for _, e := range failed {
			_, _ = fmt.Fprintf(stdout, "  - %s: %s\n", e.Name, e.Reason)
		}
	}

	return len(failed)
}

func joinNames(entries []memberEntry) string {
	names := make([]string, len(entries))
	for i, e := range entries {
		names[i] = e.Name
	}
	return strings.Join(names, ", ")
}

// promptMembers runs the interactive onboarding loop. Stopping conditions,
// in precedence order: ctx canceled, len(entries) == max, blank name, EOF.
// Validation errors re-prompt instead of aborting so one typo doesn't force
// the admin to restart the whole wizard.
func promptMembers(ctx context.Context, reader *bufio.Reader, stdout, stderr io.Writer,
	tenantsDir string, max int, seen *seenSet) []memberEntry {
	var entries []memberEntry

	for len(entries) < max {
		if err := ctx.Err(); err != nil {
			return entries
		}

		name, ok := promptUntilValid(reader, stdout, stderr, "Member name (blank to finish)",
			func(s string) error {
				if s == "" {
					return nil // sentinel: "" means stop
				}
				return core.ValidateTenantID(s)
			})
		if !ok || name == "" {
			return entries
		}

		token, ok := promptUntilValid(reader, stdout, stderr, "  Telegram bot token",
			func(s string) error {
				if !core.ValidateTelegramToken(s) {
					return errors.New("invalid telegram bot token format (expected e.g. 12345:AbCdEf...)")
				}
				if owner := seen.tokens[s]; owner != "" {
					return fmt.Errorf("token already used by tenant %q (duplicate)", owner)
				}
				return nil
			})
		if !ok {
			return entries
		}

		chatID, ok := promptUntilValid(reader, stdout, stderr, "  Admin chat ID",
			func(s string) error {
				if s == "" {
					return errors.New("chat_id is required — message the bot once and check the reply, or use @userinfobot")
				}
				return nil
			})
		if !ok {
			return entries
		}

		entry := provisionMember(tenantsDir, name, token, chatID, stdout, stderr)
		entries = append(entries, entry)
		if entry.Status == statusOK {
			seen.tenants[name] = struct{}{}
			seen.tokens[token] = name
		}
	}
	return entries
}

// promptUntilValid returns (value, true) on success or ("", false) on EOF.
// Validation errors are printed to stderr and the prompt repeats.
func promptUntilValid(reader *bufio.Reader, stdout, stderr io.Writer,
	label string, validate func(string) error) (string, bool) {
	for {
		_, _ = fmt.Fprintf(stdout, "%s: ", label)
		line, err := reader.ReadString('\n')
		if err != nil && line == "" {
			return "", false
		}
		s := strings.TrimSpace(line)
		if verr := validate(s); verr != nil {
			_, _ = fmt.Fprintf(stderr, "  ✗ %v\n", verr)
			if err != nil {
				return "", false
			}
			continue
		}
		return s, true
	}
}

// provisionMember turns one prompted member into an on-disk tenant.
// Activation failures are treated as warnings (disk state is good; the
// admin can restart the daemon later) rather than full failures.
func provisionMember(tenantsDir, name, token, chatID string, stdout, stderr io.Writer) memberEntry {
	if !core.ValidateTelegramToken(token) {
		return memberEntry{
			Name:   name,
			Status: statusFailed,
			Reason: "invalid telegram bot token format",
		}
	}

	tt, err := core.InitTenant(tenantsDir, name, core.TenantOpts{
		TelegramToken: token,
		AdminChatID:   chatID,
	})
	if err != nil {
		if errors.Is(err, core.ErrTenantExists) {
			_, _ = fmt.Fprintf(stderr, "warning: tenant %q already exists, skipping\n", name)
			return memberEntry{
				Name:   name,
				Status: statusSkippedExisting,
				Reason: "tenant directory already present; not touched",
			}
		}
		return memberEntry{
			Name:   name,
			Status: statusFailed,
			Reason: err.Error(),
		}
	}

	_, _ = fmt.Fprintf(stdout, "tenant %q created at %s\n", tt.ID, tt.BaseDir)

	if err := activateTenantOnDaemon(tt.ID, stdout, stderr); err != nil {
		_, _ = fmt.Fprintf(stderr, "warning: hot-activation failed for %q: %v\n", name, err)
	}
	return memberEntry{Name: name, Status: statusOK}
}

const familyTenantID = "family"

// createFamilyTenant provisions the household coordinator AFTER the
// personal-member loop so a partial Ctrl-C doesn't leave a family tenant
// with nobody to fan out to.
func createFamilyTenant(tenantsDir string, seen *seenSet, stdout, stderr io.Writer) memberEntry {
	if _, exists := seen.tenants[familyTenantID]; exists {
		_, _ = fmt.Fprintf(stderr, "warning: tenant %q already exists, skipping\n", familyTenantID)
		return memberEntry{
			Name:   familyTenantID,
			Status: statusSkippedExisting,
			Reason: "family tenant already present; not touched",
		}
	}

	tt, err := core.InitTenant(tenantsDir, familyTenantID, core.TenantOpts{IsFamily: true})
	if err != nil {
		if errors.Is(err, core.ErrTenantExists) {
			// Defensive: seen set was stale (e.g. another process raced us).
			return memberEntry{
				Name:   familyTenantID,
				Status: statusSkippedExisting,
				Reason: "family tenant already present; not touched",
			}
		}
		return memberEntry{
			Name:   familyTenantID,
			Status: statusFailed,
			Reason: err.Error(),
		}
	}

	// Seed an empty [share.family] stanza as a discoverable placeholder —
	// absent vs. empty reads the same at runtime, but the admin can see
	// where to grant cross-tenant reads without hand-editing TOML.
	cfgPath := filepath.Join(tt.BaseDir, "config.toml")
	cfg, err := core.LoadConfig(cfgPath)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "warning: family share stanza seed skipped (load: %v)\n", err)
	} else {
		if cfg.Share == nil {
			cfg.Share = map[string]core.ShareConfig{}
		}
		if _, ok := cfg.Share[familyTenantID]; !ok {
			cfg.Share[familyTenantID] = core.ShareConfig{Read: []string{}}
		}
		if werr := core.WriteConfigAtomic(cfg, cfgPath); werr != nil {
			_, _ = fmt.Fprintf(stderr, "warning: family share stanza seed skipped (write: %v)\n", werr)
		}
	}

	_, _ = fmt.Fprintf(stdout, "family tenant created at %s\n", tt.BaseDir)

	if err := activateTenantOnDaemon(tt.ID, stdout, stderr); err != nil {
		_, _ = fmt.Fprintf(stderr, "warning: hot-activation failed for family: %v\n", err)
	}

	return memberEntry{Name: familyTenantID, Status: statusOK}
}

// scanExistingTenants snapshots the tenants dir so the wizard can skip
// duplicate names and reject tokens that a peer already claimed.
func scanExistingTenants(tenantsDir string) (*seenSet, error) {
	seen := &seenSet{
		tenants: make(map[string]struct{}),
		tokens:  make(map[string]string),
	}

	discovered, err := core.DiscoverTenants(tenantsDir)
	if err != nil {
		return nil, fmt.Errorf("discover tenants: %w", err)
	}

	for _, t := range discovered {
		seen.tenants[t.ID] = struct{}{}
		if t.Config == nil {
			continue
		}
		for _, ch := range t.Config.Channels {
			if ch.ChannelType != core.ChannelTelegram {
				continue
			}
			token := strings.TrimSpace(ch.Token)
			if token == "" {
				continue
			}
			seen.tokens[token] = t.ID
		}
	}
	return seen, nil
}
