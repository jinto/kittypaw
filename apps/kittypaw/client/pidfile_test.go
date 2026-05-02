package client

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func writeTestPidFile(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "daemon.pid")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	return path
}

func TestReadPidFile_Legacy1Line(t *testing.T) {
	// PID files written before Phase 13.4 carry only the PID. Must
	// remain readable so an in-place CLI upgrade against a server
	// that hasn't restarted yet keeps working.
	path := writeTestPidFile(t, "12345\n")
	pid, recordedStart, ok := ReadPidFile(path)
	if !ok {
		t.Fatal("expected ok=true for legacy 1-line PID file")
	}
	if pid != 12345 {
		t.Errorf("pid = %d, want 12345", pid)
	}
	if recordedStart != 0 {
		t.Errorf("recordedStart = %d, want 0 (legacy marker)", recordedStart)
	}
}

func TestReadPidFile_NewFormat(t *testing.T) {
	path := writeTestPidFile(t, "9876\n1735689600\n")
	pid, recordedStart, ok := ReadPidFile(path)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if pid != 9876 {
		t.Errorf("pid = %d, want 9876", pid)
	}
	if recordedStart != 1735689600 {
		t.Errorf("recordedStart = %d, want 1735689600", recordedStart)
	}
}

func TestReadPidFile_Missing(t *testing.T) {
	_, _, ok := ReadPidFile("/nonexistent/daemon.pid")
	if ok {
		t.Error("expected ok=false for missing file")
	}
}

func TestReadPidFile_Malformed(t *testing.T) {
	cases := []string{
		"",
		"not-a-pid\n",
		"abc\n123\n",
	}
	for _, c := range cases {
		t.Run(c, func(t *testing.T) {
			path := writeTestPidFile(t, c)
			_, _, ok := ReadPidFile(path)
			if ok {
				t.Errorf("expected ok=false for malformed %q", c)
			}
		})
	}
}

func TestReadPidFile_CorruptStartTimeRejected(t *testing.T) {
	// A 2-line PID file with a garbled second line must NOT silently
	// downgrade to legacy mode — that would let a corrupt or
	// hand-edited file bypass start-time verification entirely. The
	// caller treats the file as unreadable and refuses to signal.
	path := writeTestPidFile(t, "555\nnotanumber\n")
	_, _, ok := ReadPidFile(path)
	if ok {
		t.Fatal("expected ok=false for corrupt 2-line PID file (must not legacy-downgrade)")
	}
}

func TestWritePidFile_RoundTrip(t *testing.T) {
	// Override processStartTime for deterministic comparison.
	orig := processStartTime
	t.Cleanup(func() { processStartTime = orig })
	processStartTime = func(_ int) (int64, error) { return 42424242, nil }

	dir := t.TempDir()
	path := filepath.Join(dir, "daemon.pid")
	if err := WritePidFile(path, 1234); err != nil {
		t.Fatalf("WritePidFile: %v", err)
	}
	pid, recordedStart, ok := ReadPidFile(path)
	if !ok {
		t.Fatal("ReadPidFile after Write should succeed")
	}
	if pid != 1234 {
		t.Errorf("pid = %d, want 1234", pid)
	}
	if recordedStart != 42424242 {
		t.Errorf("recordedStart = %d, want 42424242", recordedStart)
	}
}

func TestWritePidFile_StartTimeFailureFallsBackToZero(t *testing.T) {
	orig := processStartTime
	t.Cleanup(func() { processStartTime = orig })
	processStartTime = func(_ int) (int64, error) {
		return 0, errors.New("not supported")
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "daemon.pid")
	if err := WritePidFile(path, 99); err != nil {
		t.Fatalf("WritePidFile must not fail when start time is unavailable: %v", err)
	}
	pid, recordedStart, ok := ReadPidFile(path)
	if !ok || pid != 99 || recordedStart != 0 {
		t.Errorf("got pid=%d start=%d ok=%v; want 99/0/true", pid, recordedStart, ok)
	}
}

func TestVerifyDaemonStartTime_LegacyZero(t *testing.T) {
	orig := processStartTime
	t.Cleanup(func() { processStartTime = orig })
	processStartTime = func(_ int) (int64, error) {
		t.Fatal("processStartTime should not be called for recordedStart=0")
		return 0, nil
	}
	if !VerifyDaemonStartTime(123, 0) {
		t.Error("recordedStart=0 must return true (legacy / unsupported-platform fallback)")
	}
}

func TestVerifyDaemonStartTime_Match(t *testing.T) {
	orig := processStartTime
	t.Cleanup(func() { processStartTime = orig })
	processStartTime = func(_ int) (int64, error) { return 555, nil }
	if !VerifyDaemonStartTime(123, 555) {
		t.Error("matching start times must return true")
	}
}

func TestVerifyDaemonStartTime_Mismatch(t *testing.T) {
	orig := processStartTime
	t.Cleanup(func() { processStartTime = orig })
	processStartTime = func(_ int) (int64, error) { return 999, nil }
	if VerifyDaemonStartTime(123, 555) {
		t.Error("PID-reuse detection: mismatch must return false")
	}
}

func TestVerifyDaemonStartTime_LookupFailureFailsClosed(t *testing.T) {
	// recordedStart!=0 means the server committed to Phase 13.4
	// hardening when it wrote the PID file. If the live process's
	// start time can't be read (ps blocked, /proc hidden, exec
	// failure, attacker-installed fake ps refusing to respond), we
	// must REFUSE to signal — falling back to "trust" would silently
	// bypass the very protection this code adds.
	orig := processStartTime
	t.Cleanup(func() { processStartTime = orig })
	processStartTime = func(_ int) (int64, error) {
		return 0, errors.New("ps blocked")
	}
	if VerifyDaemonStartTime(123, 555) {
		t.Error("lookup failure on a Phase-13.4 PID file must fail closed (return false)")
	}
}
