//go:build linux

package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/jinto/kittypaw/packaging"
)

const linuxUnitName = "kittypaw.service"

// renderUnit substitutes the default /usr/local/bin path and bind port in
// the systemd unit template with runtime values.
func renderUnit(tpl, binPath, bindHost string, bindPort int) string {
	out := strings.ReplaceAll(tpl,
		"ExecStart=/usr/local/bin/kittypaw ",
		"ExecStart="+binPath+" ")
	out = strings.ReplaceAll(out,
		"--bind 127.0.0.1:3000",
		fmt.Sprintf("--bind %s:%d", bindHost, bindPort))
	return out
}

func userSystemdDir() string {
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		base = filepath.Join(os.Getenv("HOME"), ".config")
	}
	return filepath.Join(base, "systemd", "user")
}

func serviceInstall(stdout, stderr io.Writer, f *serviceFlags) error {
	binPath, err := resolveBinPath(f.binPath)
	if err != nil {
		return err
	}

	// Stop a previously installed service so the rewrite and subsequent
	// enable --now cycle doesn't race against a live listener.
	_ = run(io.Discard, io.Discard, "systemctl", "--user", "stop", linuxUnitName)

	unit := renderUnit(packaging.LinuxSystemdUnit, binPath, f.bindHost, f.bindPort)
	destDir := userSystemdDir()
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return fmt.Errorf("create %s: %w", destDir, err)
	}
	destPath := filepath.Join(destDir, linuxUnitName)
	if err := os.WriteFile(destPath, []byte(unit), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", destPath, err)
	}
	_, _ = fmt.Fprintf(stdout, "installed unit: %s  (bind %s:%d)\n", destPath, f.bindHost, f.bindPort)

	if err := run(stdout, stderr, "systemctl", "--user", "daemon-reload"); err != nil {
		return fmt.Errorf("systemctl daemon-reload: %w", err)
	}
	if err := run(stdout, stderr, "systemctl", "--user", "enable", "--now", linuxUnitName); err != nil {
		return fmt.Errorf("systemctl enable --now: %w", err)
	}

	linuxPostInstallDiagnostics(stdout)
	_, _ = fmt.Fprintln(stdout, "\ndone. tail the log with:  kittypaw service logs -f")
	return nil
}

func serviceUninstall(stdout, stderr io.Writer) error {
	_ = run(stdout, stderr, "systemctl", "--user", "disable", "--now", linuxUnitName)
	destPath := filepath.Join(userSystemdDir(), linuxUnitName)
	if err := os.Remove(destPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove %s: %w", destPath, err)
	}
	_ = run(stdout, stderr, "systemctl", "--user", "daemon-reload")
	_, _ = fmt.Fprintf(stdout, "removed unit: %s\n", destPath)
	return nil
}

func serviceStatus(stdout, stderr io.Writer) error {
	// is-active exits non-zero when inactive — don't treat that as an error.
	active := run(io.Discard, io.Discard, "systemctl", "--user", "is-active", "--quiet", linuxUnitName) == nil
	if active {
		_, _ = fmt.Fprintln(stdout, "active: yes")
	} else {
		_, _ = fmt.Fprintln(stdout, "active: no")
	}

	// ExecStart and MainPID surfaced via `show -p`.
	if out, err := exec.Command("systemctl", "--user", "show",
		"-p", "ExecStart", "-p", "MainPID", "-p", "ActiveState", "-p", "Delegate",
		linuxUnitName).Output(); err == nil {
		_, _ = fmt.Fprint(stdout, string(out))
	}
	return nil
}

func serviceLogs(stdout, stderr io.Writer, follow bool) error {
	args := []string{"--user", "-u", linuxUnitName}
	if follow {
		args = append(args, "-f")
	} else {
		args = append(args, "-n", "200")
	}
	c := exec.Command("journalctl", args...)
	c.Stdout = stdout
	c.Stderr = stderr
	return c.Run()
}

func linuxPostInstallDiagnostics(stdout io.Writer) {
	// Delegate not set → resource directives silently ignored.
	if out, err := exec.Command("systemctl", "--user", "show",
		"-p", "Delegate", "--value", linuxUnitName).Output(); err == nil {
		if strings.TrimSpace(string(out)) != "yes" {
			_, _ = fmt.Fprintln(stdout, "\nnote: cgroup controller delegation is not enabled.")
			_, _ = fmt.Fprintln(stdout, "      MemoryMax / CPUQuota / TasksMax in the unit are ignored until")
			_, _ = fmt.Fprintln(stdout, "      an administrator runs:")
			_, _ = fmt.Fprintln(stdout, "        sudo mkdir -p /etc/systemd/system/user-.slice.d")
			_, _ = fmt.Fprintln(stdout, "        printf '[Slice]\\nDelegate=yes\\n' |")
			_, _ = fmt.Fprintln(stdout, "          sudo tee /etc/systemd/system/user-.slice.d/10-delegate.conf")
			_, _ = fmt.Fprintln(stdout, "        sudo systemctl daemon-reload")
		}
	}

	// Linger — without it the user manager exits on logout.
	if user := os.Getenv("USER"); user != "" {
		if out, err := exec.Command("loginctl", "show-user", user).Output(); err == nil {
			if !strings.Contains(string(out), "Linger=yes") {
				_, _ = fmt.Fprintln(stdout, "\nnote: linger is not enabled for user", user+".")
				_, _ = fmt.Fprintln(stdout, "      Without linger, kittypaw stops when you log out.")
				_, _ = fmt.Fprintln(stdout, "      Enable with:  sudo loginctl enable-linger", user)
			}
		}
	}
}
