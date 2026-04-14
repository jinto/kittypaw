//go:build windows

package client

import (
	"os"
	"os/exec"
	"strconv"
	"strings"
)

func setSysProcAttr(cmd *exec.Cmd) {}

func lockPidFile(path string) (*os.File, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	return f, nil
}

func unlockPidFile(f *os.File) {
	f.Close()
}

func isKittypawProcess(pid int) bool {
	out, err := exec.Command("tasklist", "/FI", "PID eq "+strconv.Itoa(pid), "/NH").Output()
	if err != nil {
		return false
	}
	return strings.Contains(strings.ToLower(string(out)), "kittypaw")
}
