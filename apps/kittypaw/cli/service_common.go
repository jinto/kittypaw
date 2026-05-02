//go:build linux || darwin

package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
)

// run executes name with args, piping stdout/stderr to the given writers.
// Any non-zero exit is returned as an error so callers can decide to
// propagate or swallow (most swallow for "already gone" cases by passing
// io.Discard for both streams).
func run(stdout, stderr io.Writer, name string, args ...string) error {
	c := exec.Command(name, args...)
	c.Stdout = stdout
	c.Stderr = stderr
	return c.Run()
}

// resolveBinPath returns the absolute path of the kittypaw binary the
// installed unit/plist should point at. An explicit --binary flag wins;
// otherwise fall back to os.Executable() so `kittypaw server install`
// registers the binary the user actually invoked.
func resolveBinPath(explicit string) (string, error) {
	if explicit != "" {
		return filepath.Abs(explicit)
	}
	exe, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("resolve binary path: %w", err)
	}
	return filepath.EvalSymlinks(exe)
}
