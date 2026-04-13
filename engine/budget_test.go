package engine

import (
	"sync"
	"testing"
)

func TestSharedBudget_UnlimitedAlwaysSucceeds(t *testing.T) {
	b := NewSharedBudget(0)
	for i := range 100 {
		if !b.TrySpend(1000) {
			t.Fatalf("unlimited budget rejected spend at iteration %d", i)
		}
	}
	if b.Used() != 100_000 {
		t.Fatalf("used = %d, want 100000", b.Used())
	}
	// Remaining returns max uint64 for unlimited.
	if b.Remaining() != ^uint64(0) {
		t.Fatal("unlimited remaining should be max uint64")
	}
}

func TestSharedBudget_ExactLimit(t *testing.T) {
	b := NewSharedBudget(100)
	if !b.TrySpend(100) {
		t.Fatal("spend exactly at limit should succeed")
	}
	if b.TrySpend(1) {
		t.Fatal("spend beyond limit should fail")
	}
	if b.Remaining() != 0 {
		t.Fatalf("remaining = %d, want 0", b.Remaining())
	}
}

func TestSharedBudget_PartialSpend(t *testing.T) {
	b := NewSharedBudget(100)
	if !b.TrySpend(60) {
		t.Fatal("first spend should succeed")
	}
	if !b.TrySpend(30) {
		t.Fatal("second spend should succeed (90 total)")
	}
	if b.TrySpend(20) {
		t.Fatal("third spend should fail (would be 110)")
	}
	if b.Remaining() != 10 {
		t.Fatalf("remaining = %d, want 10", b.Remaining())
	}
}

func TestSharedBudget_ConcurrentSpend(t *testing.T) {
	const limit = 5_000
	const goroutines = 100
	const perGoroutine = 100 // total attempts = 10000 > limit

	b := NewSharedBudget(limit)

	var wg sync.WaitGroup
	successes := make([]int, goroutines)

	for i := range goroutines {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			for range perGoroutine {
				if b.TrySpend(1) {
					successes[idx]++
				}
			}
		}(i)
	}
	wg.Wait()

	totalSucceeded := 0
	for _, n := range successes {
		totalSucceeded += n
	}

	// Exactly `limit` spends should succeed — no more, no less.
	if uint64(totalSucceeded) != limit {
		t.Fatalf("total succeeded = %d, want %d (budget exhaustion correctness)", totalSucceeded, limit)
	}
	if b.Used() != limit {
		t.Fatalf("used = %d, want %d", b.Used(), limit)
	}
}

func TestSharedBudget_TrySpendFromUsage_Nil(t *testing.T) {
	b := NewSharedBudget(100)
	if !b.TrySpendFromUsage(nil) {
		t.Fatal("nil usage should always succeed")
	}
	if b.Used() != 0 {
		t.Fatalf("used = %d after nil usage, want 0", b.Used())
	}
}
