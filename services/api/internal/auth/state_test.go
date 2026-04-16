package auth

import (
	"testing"
	"time"
)

func TestStateCreateConsume(t *testing.T) {
	s := NewStateStore()
	defer s.Close()

	state, err := s.Create("verifier-abc")
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	verifier, err := s.Consume(state)
	if err != nil {
		t.Fatalf("consume: %v", err)
	}
	if verifier != "verifier-abc" {
		t.Fatalf("expected verifier-abc, got %q", verifier)
	}
}

func TestStateReplayProtection(t *testing.T) {
	s := NewStateStore()
	defer s.Close()

	state, err := s.Create("verifier")
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	if _, err := s.Consume(state); err != nil {
		t.Fatalf("first consume: %v", err)
	}

	if _, err := s.Consume(state); err == nil {
		t.Fatal("expected error on replay")
	}
}

func TestStateExpired(t *testing.T) {
	s := NewStateStore()
	defer s.Close()

	// Insert an already-expired entry directly.
	s.mu.Lock()
	s.entries["expired-state"] = stateEntry{
		codeVerifier: "v",
		createdAt:    time.Now().Add(-stateTTL - time.Second),
	}
	s.mu.Unlock()

	_, err := s.Consume("expired-state")
	if err == nil {
		t.Fatal("expected error for expired state")
	}
}

func TestStateCapExceeded(t *testing.T) {
	s := &StateStore{
		entries: make(map[string]stateEntry),
		stop:    make(chan struct{}),
	}
	defer s.Close()

	// Fill to capacity with non-expired entries.
	for i := range stateMaxEntries {
		s.entries[string(rune(i))+"-key"] = stateEntry{
			codeVerifier: "v",
			createdAt:    time.Now(),
		}
	}

	_, err := s.Create("overflow")
	if err == nil {
		t.Fatal("expected error when store is full")
	}
}

func TestStateEvictsExpiredOnCreate(t *testing.T) {
	s := &StateStore{
		entries: make(map[string]stateEntry),
		stop:    make(chan struct{}),
	}
	defer s.Close()

	// Fill to capacity with expired entries.
	for i := range stateMaxEntries {
		s.entries[string(rune(i))+"-key"] = stateEntry{
			codeVerifier: "v",
			createdAt:    time.Now().Add(-stateTTL - time.Second),
		}
	}

	// Should succeed after lazy eviction.
	_, err := s.Create("should-work")
	if err != nil {
		t.Fatalf("expected success after eviction, got: %v", err)
	}
}
