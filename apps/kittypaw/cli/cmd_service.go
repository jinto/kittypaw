//go:build linux || darwin

package main

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/spf13/cobra"
)

// serviceFlags captures CLI flags shared by `server install`. Kept as a
// module-level struct so subcommand closures can read/write it.
type serviceFlags struct {
	bindHost string
	bindPort int
	binPath  string
	follow   bool
	force    bool
}

func newServiceInstallCmd() *cobra.Command {
	f := &serviceFlags{}
	cmd := &cobra.Command{
		Use:   "install",
		Short: "Write the unit/plist and start the server",
		Long: `Install the per-user service and start it. Subsequent runs reinstall
cleanly — the server is stopped before the unit/plist is rewritten.

Refuses to install if no account has been provisioned yet (run 'kittypaw
setup' first) — a fresh server with zero accounts crash-loops under
launchd/systemd KeepAlive. Pass --force to install anyway.

If another process already holds the bind port (common when a second user
is onboarding on a shared host), installation fails with a hint to pick a
different --bind-port.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if !f.force {
				if err := preflightAccountReady(); err != nil {
					return err
				}
			}
			return serviceInstall(cmd.OutOrStdout(), cmd.ErrOrStderr(), f)
		},
	}
	cmd.Flags().StringVar(&f.bindHost, "bind-host", "127.0.0.1", "Host kittypaw server will listen on")
	cmd.Flags().IntVar(&f.bindPort, "bind-port", 3000, "Port kittypaw server will listen on")
	cmd.Flags().StringVar(&f.binPath, "binary", "", "Absolute path to kittypaw binary (auto-detected when empty)")
	cmd.Flags().BoolVar(&f.force, "force", false, "Install even when no account is provisioned (server will crash-loop)")
	return cmd
}

func newServiceUninstallCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "uninstall",
		Short: "Stop the server and remove the unit/plist",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return serviceUninstall(cmd.OutOrStdout(), cmd.ErrOrStderr())
		},
	}
	return cmd
}

func newServiceStatusCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show whether the server service is active and where it binds",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return serviceStatus(cmd.OutOrStdout(), cmd.ErrOrStderr())
		},
	}
	return cmd
}

func newServiceLogsCmd() *cobra.Command {
	f := &serviceFlags{}
	cmd := &cobra.Command{
		Use:   "logs",
		Short: "Tail the server log (journald on Linux, plist logs on macOS)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return serviceLogs(cmd.OutOrStdout(), cmd.ErrOrStderr(), f.follow)
		},
	}
	cmd.Flags().BoolVarP(&f.follow, "follow", "f", false, "Follow log output (-f)")
	return cmd
}

// configDirForCheck resolves the data directory without the side effect of
// creating it (unlike core.ConfigDir, which MkdirAll+Chmod). Used by
// preflight checks that MUST stay read-only — creating the directory here
// would silently defeat the "did you run setup?" question we are asking.
func configDirForCheck() string {
	if dir := os.Getenv("KITTYPAW_CONFIG_DIR"); dir != "" {
		return dir
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".kittypaw")
}

// preflightAccountReady refuses to install the service when no account has
// been provisioned. The server start loop fails fast on an empty account
// registry, and under launchd/systemd KeepAlive that becomes a crash-loop
// that spams stderr.log until an operator intervenes. Catching it here
// means the user sees a single actionable message.
func preflightAccountReady() error {
	accountsDir := filepath.Join(configDirForCheck(), "accounts")
	entries, err := os.ReadDir(accountsDir)
	if err == nil && len(entries) > 0 {
		return nil
	}
	return fmt.Errorf(
		"no account has been provisioned yet — %s is missing or empty.\n\n"+
			"  Run 'kittypaw setup' first to configure an LLM provider and\n"+
			"  create an account; the wizard offers to install the server at\n"+
			"  the end so you rarely need to call `server install` directly.\n\n"+
			"  Pass --force to install anyway — the server will crash-loop\n"+
			"  under launchd/systemd KeepAlive until setup runs",
		accountsDir)
}

// preflightPort probes host:port and returns an error if something is
// already listening. The probe uses a short DialTimeout — a successful dial
// means the port is taken, ECONNREFUSED means it's free.
func preflightPort(host string, port int) error {
	addr := net.JoinHostPort(host, strconv.Itoa(port))
	conn, err := net.DialTimeout("tcp", addr, 250*time.Millisecond)
	if err != nil {
		return nil // free
	}
	_ = conn.Close()
	return fmt.Errorf(
		"port %s is already in use.\n\n"+
			"  Another process — likely another OS user's kittypaw server — is\n"+
			"  bound to this port. Pick a free port and retry:\n\n"+
			"    kittypaw server install --bind-port 3001\n\n"+
			"  Then point your client at the same port:\n"+
			"    kittypaw chat --remote http://%s",
		addr, net.JoinHostPort(host, "3001"))
}
