//go:build !windows

package client

import (
	"os"
	"strings"
	"testing"
)

func TestRealProcessStartTime_OnSelf(t *testing.T) {
	// Sanity-check the platform-specific path against os.Getpid()
	// — if this returns an error or zero, the production write/
	// verify path will silently fall through to legacy behavior on
	// this platform, which the mocked unit tests can't catch.
	//
	// Skipped when /bin/ps refuses to exec — sandboxed CI / SIP
	// environments produce "operation not permitted" and the test
	// can't tell that from a real regression.
	first, err := realProcessStartTime(os.Getpid())
	if err != nil {
		if strings.Contains(err.Error(), "operation not permitted") ||
			strings.Contains(err.Error(), "permission denied") {
			t.Skipf("ps/proc blocked by sandbox: %v", err)
		}
		t.Fatalf("realProcessStartTime(self): %v", err)
	}
	if first <= 0 {
		t.Fatalf("expected positive start time, got %d", first)
	}
	// Two calls within the same process must return the same value —
	// the start time is set at exec and never changes.
	second, err := realProcessStartTime(os.Getpid())
	if err != nil {
		t.Fatalf("second realProcessStartTime: %v", err)
	}
	if first != second {
		t.Errorf("start time changed between calls: %d -> %d", first, second)
	}
}

func TestParseProcStat_NormalComm(t *testing.T) {
	// Sample /proc/<pid>/stat line. The 22nd whitespace-separated
	// field (index 19 post-comm) is the starttime — here 12345.
	in := "1234 (kittypaw) S 1 1234 1234 0 -1 4194304 1 0 0 0 0 0 0 0 20 0 1 0 12345 12345678 90 18446744073709551615 1 1 0 0 0 0 0 0 0 0 0 0 17 0 0 0 0 0 0 0 0 0 0 0 0 0 0\n"
	got, err := parseProcStat(in)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if got != 12345 {
		t.Errorf("starttime = %d, want 12345", got)
	}
}

func TestParseProcStat_CommWithParens(t *testing.T) {
	// comm field can contain ')' — Linux truncates at TASK_COMM_LEN=16
	// but otherwise places no constraint. Our parser anchors on the
	// LAST ')' so even a weird comm doesn't shift starttime's index.
	in := "1234 (foo (bar)) S 1 1234 1234 0 -1 4194304 1 0 0 0 0 0 0 0 20 0 1 0 99999 12345678 90 18446744073709551615 1 1 0 0 0 0 0 0 0 0 0 0 17 0 0 0 0 0 0 0 0 0 0 0 0 0 0\n"
	got, err := parseProcStat(in)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if got != 99999 {
		t.Errorf("starttime = %d, want 99999 (comm-with-parens robustness)", got)
	}
}

func TestParseProcStat_CommWithSpaces(t *testing.T) {
	// comm can also contain spaces.
	in := "5678 (my proc) R 1 5678 5678 0 -1 4194304 1 0 0 0 0 0 0 0 20 0 1 0 7777 12345678 90 18446744073709551615 1 1 0 0 0 0 0 0 0 0 0 0 17 0 0 0 0 0 0 0 0 0 0 0 0 0 0\n"
	got, err := parseProcStat(in)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if got != 7777 {
		t.Errorf("starttime = %d, want 7777", got)
	}
}

func TestParseProcStat_NoClosingParen(t *testing.T) {
	if _, err := parseProcStat("1234 (broken S 1"); err == nil {
		t.Error("expected error for /proc stat with no ')'")
	}
}

func TestParseProcStat_TooFewFields(t *testing.T) {
	// 19 post-comm fields — one short of the 20 we need.
	in := "1234 (kittypaw) S 1 1234 1234 0 -1 4194304 1 0 0 0 0 0 0 0 20 0 1\n"
	if _, err := parseProcStat(in); err == nil {
		t.Error("expected error when post-comm field count is insufficient")
	}
}
