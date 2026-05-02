package auth

import (
	"crypto/rand"
	"fmt"
	"strings"
	"sync"
	"time"
)

const (
	cliCodeTTL        = 5 * time.Minute
	cliCodeMaxEntries = 1000
	cliCodeSweepEvery = 1 * time.Minute
	cliCodeLength     = 8

	// Charset excludes O/0/I/1 to avoid visual confusion.
	cliCodeCharset = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"
)

type cliCodeEntry struct {
	tokens    TokenResponse
	createdAt time.Time
}

// CLICodeStore holds one-time codes that map to OAuth token pairs.
// Used by the code-paste mode: the browser shows a code, the user
// types it into the CLI, and the CLI exchanges it for tokens.
type CLICodeStore struct {
	mu        sync.Mutex
	entries   map[string]cliCodeEntry
	stop      chan struct{}
	closeOnce sync.Once
}

func NewCLICodeStore() *CLICodeStore {
	s := &CLICodeStore{
		entries: make(map[string]cliCodeEntry),
		stop:    make(chan struct{}),
	}
	go s.sweep()
	return s
}

// Close stops the sweep goroutine. Idempotent (sync.Once).
func (s *CLICodeStore) Close() {
	s.closeOnce.Do(func() { close(s.stop) })
}

// Create stores a token pair and returns a displayable code like "ABCD-1234".
func (s *CLICodeStore) Create(tokens TokenResponse) (string, error) {
	code, err := generateCLICode()
	if err != nil {
		return "", err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.evictExpiredLocked()

	if len(s.entries) >= cliCodeMaxEntries {
		return "", fmt.Errorf("code store full")
	}

	s.entries[code] = cliCodeEntry{
		tokens:    tokens,
		createdAt: time.Now(),
	}

	// Display format: XXXX-YYYY
	return code[:4] + "-" + code[4:], nil
}

// Consume returns the stored tokens for the given code (one-time use).
// Accepts codes with or without the dash separator.
func (s *CLICodeStore) Consume(code string) (*TokenResponse, error) {
	normalized := strings.ReplaceAll(strings.TrimSpace(code), "-", "")
	normalized = strings.ToUpper(normalized)

	s.mu.Lock()
	defer s.mu.Unlock()

	entry, ok := s.entries[normalized]
	if !ok {
		return nil, fmt.Errorf("unknown or expired code")
	}
	delete(s.entries, normalized)

	if time.Since(entry.createdAt) > cliCodeTTL {
		return nil, fmt.Errorf("code expired")
	}
	return &entry.tokens, nil
}

func (s *CLICodeStore) evictExpiredLocked() {
	now := time.Now()
	for k, v := range s.entries {
		if now.Sub(v.createdAt) > cliCodeTTL {
			delete(s.entries, k)
		}
	}
}

func (s *CLICodeStore) sweep() {
	ticker := time.NewTicker(cliCodeSweepEvery)
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

func generateCLICode() (string, error) {
	b := make([]byte, cliCodeLength)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate code: %w", err)
	}
	code := make([]byte, cliCodeLength)
	for i := range code {
		code[i] = cliCodeCharset[int(b[i])%len(cliCodeCharset)]
	}
	return string(code), nil
}
