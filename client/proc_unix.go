//go:build !windows

package client

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"
)

func setSysProcAttr(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}

func isKittypawProcess(pid int) bool {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	if proc.Signal(syscall.Signal(0)) != nil {
		return false
	}
	out, err := exec.Command("ps", "-p", strconv.Itoa(pid), "-o", "comm=").Output()
	if err != nil {
		return false
	}
	name := strings.TrimSpace(string(out))
	return strings.Contains(name, "kittypaw")
}

func lockPidFile(path string) (*os.File, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		return nil, err
	}
	return f, nil
}

func unlockPidFile(f *os.File) {
	syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	f.Close()
}

// processStartTime returns a stable per-platform identifier for
// when pid started. Used by Phase 13.4 PID file hardening to detect
// PID reuse: the daemon records its own start time alongside its
// PID, and `kittypaw server stop` re-queries the live process's start time
// before signaling — a mismatch means the recorded PID was
// recycled by an unrelated process.
//
// Units differ across platforms (Linux: raw starttime jiffies from
// /proc/<pid>/stat; BSD/macOS: unix seconds parsed from
// `ps -o lstart=`) but values are only ever compared between
// invocations on the same machine, so the cross-platform
// inconsistency is invisible to callers.
var processStartTime = realProcessStartTime

func realProcessStartTime(pid int) (int64, error) {
	switch runtime.GOOS {
	case "linux":
		return procStartTimeLinux(pid)
	case "darwin", "freebsd", "openbsd", "netbsd":
		return procStartTimeBSD(pid)
	default:
		return 0, fmt.Errorf("start-time fingerprinting not supported on %s", runtime.GOOS)
	}
}

func procStartTimeLinux(pid int) (int64, error) {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return 0, err
	}
	return parseProcStat(string(data))
}

// parseProcStat extracts the starttime (field 22 in proc(5), index
// 19 after the comm field) from a /proc/<pid>/stat line. comm can
// contain ANY printable bytes including spaces and parens up to
// TASK_COMM_LEN=16; we anchor on the LAST ')' to robustly skip it
// even when comm itself contains a ')' character. Returned as a
// raw int64 — the absolute value's meaning (jiffies since boot) is
// only ever compared between calls on the same booted kernel, so
// no time conversion is needed.
func parseProcStat(s string) (int64, error) {
	rparen := strings.LastIndex(s, ")")
	if rparen == -1 {
		return 0, fmt.Errorf("malformed /proc stat: no closing paren")
	}
	fields := strings.Fields(s[rparen+1:])
	// After comm: state(0) ppid(1) pgrp(2) session(3) tty_nr(4)
	// tpgid(5) flags(6) minflt(7) cminflt(8) majflt(9) cmajflt(10)
	// utime(11) stime(12) cutime(13) cstime(14) priority(15) nice(16)
	// num_threads(17) itrealvalue(18) starttime(19)
	if len(fields) < 20 {
		return 0, fmt.Errorf("malformed /proc stat: only %d post-comm fields", len(fields))
	}
	return strconv.ParseInt(fields[19], 10, 64)
}

// psBinaryPath is the absolute path to ps used by procStartTimeBSD.
// Hardcoding /bin/ps closes the PATH-injection footgun: an
// `exec.Command("ps", ...)` with a user-modified $PATH would
// happily run a fake `ps` planted earlier in the path, and that
// fake could forge a recorded start time to pass verification.
const psBinaryPath = "/bin/ps"

func procStartTimeBSD(pid int) (int64, error) {
	cmd := exec.Command(psBinaryPath, "-o", "lstart=", "-p", strconv.Itoa(pid))
	// Sanitized env: locale fixed to C so date format is predictable,
	// timezone fixed to UTC so the same process produces the same
	// fingerprint across DST transitions or TZ changes between the
	// daemon's WritePidFile call and the later VerifyDaemonStartTime
	// call. PATH is constrained to system bins as defense in depth.
	cmd.Env = []string{
		"LC_ALL=C",
		"LANG=C",
		"LC_TIME=C",
		"TZ=UTC",
		"PATH=/bin:/usr/bin",
	}
	out, err := cmd.Output()
	if err != nil {
		return 0, fmt.Errorf("ps for pid %d: %w", pid, err)
	}
	s := strings.TrimSpace(string(out))
	if s == "" {
		return 0, fmt.Errorf("ps returned empty for pid %d", pid)
	}
	// Format with TZ=UTC: "Mon Apr 28 23:00:00 2026" (no zone).
	t, err := time.ParseInLocation("Mon Jan _2 15:04:05 2006", s, time.UTC)
	if err != nil {
		return 0, fmt.Errorf("parse lstart %q: %w", s, err)
	}
	return t.Unix(), nil
}
