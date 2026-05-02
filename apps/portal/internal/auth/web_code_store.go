package auth

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"sync"
	"time"
)

const (
	// webCodeTTL — chat 서버가 callback redirect 받자마자 server-to-server
	// exchange를 부른다는 가정. 60s = 사용자 retry 한 번 + chat ↔ API
	// 왕복 + 안전 마진. 더 짧으면 chat의 transient 네트워크 blip에 깨지고,
	// 더 길면 leaked-code 노출 시간이 늘어남.
	webCodeTTL        = 60 * time.Second
	webCodeMaxEntries = 10000
	webCodeSweepEvery = 30 * time.Second
	webCodeBytes      = 32 // base64url ≈ 43 chars
)

// WebCodeEntry holds the one-time-use authorization code state for the
// PKCE web flow (Plan 25). The verifier itself never reaches the server —
// only its S256 hash (CodeChallenge), checked at exchange time.
type WebCodeEntry struct {
	UserID        string
	RedirectURI   string
	CodeChallenge string
}

// WebCodeStore is the in-memory authorization-code ledger for the web
// OAuth flow. Each code maps to a single user authentication and binds
// the (redirect_uri, code_challenge) the caller must present at exchange.
//
// Mirrors CLICodeStore's lifecycle (sweep goroutine + sync.Once close).
// Diverges in entry shape: CLICodeStore caches a TokenResponse (since
// the CLI flow issues tokens at callback time), while WebCodeStore caches
// only the user identity and PKCE binding (tokens are issued at exchange
// time so a leaked code without verifier can't yield tokens).
type WebCodeStore struct {
	mu        sync.Mutex
	entries   map[string]webCodeRecord
	stop      chan struct{}
	closeOnce sync.Once
}

type webCodeRecord struct {
	entry     WebCodeEntry
	createdAt time.Time
}

func NewWebCodeStore() *WebCodeStore {
	s := &WebCodeStore{
		entries: make(map[string]webCodeRecord),
		stop:    make(chan struct{}),
	}
	go s.sweep()
	return s
}

// Close stops the sweep goroutine. Idempotent.
func (s *WebCodeStore) Close() {
	s.closeOnce.Do(func() { close(s.stop) })
}

// Create allocates a fresh code, stores the entry, and returns the code.
// The code is base64url(32 random bytes) — long enough that brute-force
// guessing within the 60s TTL is infeasible.
func (s *WebCodeStore) Create(entry WebCodeEntry) (string, error) {
	b := make([]byte, webCodeBytes)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate web code: %w", err)
	}
	code := base64.RawURLEncoding.EncodeToString(b)

	s.mu.Lock()
	defer s.mu.Unlock()

	s.evictExpiredLocked()

	if len(s.entries) >= webCodeMaxEntries {
		return "", fmt.Errorf("web code store full")
	}

	s.entries[code] = webCodeRecord{
		entry:     entry,
		createdAt: time.Now(),
	}
	return code, nil
}

// Consume returns the entry for the given code and removes it atomically.
// One-time use is the contract — a second Consume on the same code (race
// or replay) returns "unknown or expired code". Expired entries return
// "code expired" so handler logs distinguish legitimate-but-late from
// genuinely-unknown.
func (s *WebCodeStore) Consume(code string) (*WebCodeEntry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	rec, ok := s.entries[code]
	if !ok {
		return nil, fmt.Errorf("unknown or expired code")
	}
	delete(s.entries, code)

	if time.Since(rec.createdAt) > webCodeTTL {
		return nil, fmt.Errorf("code expired")
	}
	return &rec.entry, nil
}

func (s *WebCodeStore) evictExpiredLocked() {
	now := time.Now()
	for k, v := range s.entries {
		if now.Sub(v.createdAt) > webCodeTTL {
			delete(s.entries, k)
		}
	}
}

func (s *WebCodeStore) sweep() {
	ticker := time.NewTicker(webCodeSweepEvery)
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
