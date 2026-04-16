package auth

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"sync"
	"time"
)

const (
	stateMaxEntries = 10000
	stateTTL        = 10 * time.Minute
	stateSweepEvery = 1 * time.Minute
)

type stateEntry struct {
	codeVerifier string
	createdAt    time.Time
}

type StateStore struct {
	mu      sync.Mutex
	entries map[string]stateEntry
	stop    chan struct{}
}

func NewStateStore() *StateStore {
	s := &StateStore{
		entries: make(map[string]stateEntry),
		stop:    make(chan struct{}),
	}
	go s.sweep()
	return s
}

func (s *StateStore) Close() {
	close(s.stop)
}

func (s *StateStore) Create(codeVerifier string) (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate state: %w", err)
	}
	state := base64.RawURLEncoding.EncodeToString(b)

	s.mu.Lock()
	defer s.mu.Unlock()

	s.evictExpiredLocked()

	if len(s.entries) >= stateMaxEntries {
		return "", fmt.Errorf("state store full")
	}

	s.entries[state] = stateEntry{
		codeVerifier: codeVerifier,
		createdAt:    time.Now(),
	}
	return state, nil
}

func (s *StateStore) Consume(state string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	entry, ok := s.entries[state]
	if !ok {
		return "", fmt.Errorf("unknown or expired state")
	}
	delete(s.entries, state)

	if time.Since(entry.createdAt) > stateTTL {
		return "", fmt.Errorf("state expired")
	}
	return entry.codeVerifier, nil
}

func (s *StateStore) evictExpiredLocked() {
	now := time.Now()
	for k, v := range s.entries {
		if now.Sub(v.createdAt) > stateTTL {
			delete(s.entries, k)
		}
	}
}

func (s *StateStore) sweep() {
	ticker := time.NewTicker(stateSweepEvery)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			s.mu.Lock()
			s.evictExpiredLocked()
			s.mu.Unlock()
		case <-s.stop:
			return
		}
	}
}
