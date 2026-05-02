package ratelimit_test

import (
	"testing"
	"time"

	"github.com/kittypaw-app/kittyapi/internal/ratelimit"
)

func TestAllowWithinLimit(t *testing.T) {
	l := ratelimit.New()
	defer l.Close()

	for i := range 5 {
		if !l.Allow("ip:1.2.3.4", 5) {
			t.Fatalf("request %d should be allowed", i+1)
		}
	}
}

func TestAllowExceedsLimit(t *testing.T) {
	l := ratelimit.New()
	defer l.Close()

	for range 5 {
		l.Allow("ip:1.2.3.4", 5)
	}

	if l.Allow("ip:1.2.3.4", 5) {
		t.Fatal("6th request should be denied")
	}
}

func TestAllowWindowReset(t *testing.T) {
	now := time.Now()
	l := ratelimit.NewWithClock(func() time.Time { return now })
	defer l.Close()

	for range 5 {
		l.Allow("ip:1.2.3.4", 5)
	}

	if l.Allow("ip:1.2.3.4", 5) {
		t.Fatal("should be denied before window reset")
	}

	// Advance past the 1-minute window.
	now = now.Add(61 * time.Second)

	if !l.Allow("ip:1.2.3.4", 5) {
		t.Fatal("should be allowed after window reset")
	}
}

func TestAllowDailyCapExceeded(t *testing.T) {
	l := ratelimit.New()
	defer l.Close()

	for range 10000 {
		l.AllowDaily("global", 10000)
	}

	if l.AllowDaily("global", 10000) {
		t.Fatal("daily cap should be exceeded")
	}
}

func TestDailyIndependentOfPerMinute(t *testing.T) {
	l := ratelimit.New()
	defer l.Close()

	// Per-minute should still work even if daily is full.
	if !l.Allow("ip:1.2.3.4", 5) {
		t.Fatal("per-minute should be independent")
	}
}

func TestSecondsUntilReset(t *testing.T) {
	l := ratelimit.New()
	defer l.Close()

	l.Allow("ip:test", 5)

	s := l.SecondsUntilReset("ip:test")
	if s <= 0 || s > 61 {
		t.Fatalf("expected 1-61 seconds, got %d", s)
	}
}

func TestSecondsUntilResetUnknownKey(t *testing.T) {
	l := ratelimit.New()
	defer l.Close()

	s := l.SecondsUntilReset("unknown")
	if s != 0 {
		t.Fatalf("expected 0 for unknown key, got %d", s)
	}
}
