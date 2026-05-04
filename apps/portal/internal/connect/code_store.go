package connect

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"sync"
	"time"
)

const (
	defaultCodeTTL        = 5 * time.Minute
	defaultCodeMaxEntries = 10000
)

type CodeStoreOptions struct {
	TTL        time.Duration
	MaxEntries int
	Now        func() time.Time
}

type codeEntry struct {
	tokens    TokenSet
	createdAt time.Time
}

type CodeStore struct {
	mu         sync.Mutex
	entries    map[string]codeEntry
	ttl        time.Duration
	maxEntries int
	now        func() time.Time
}

func NewCodeStore(opts CodeStoreOptions) *CodeStore {
	ttl := opts.TTL
	if ttl <= 0 {
		ttl = defaultCodeTTL
	}
	maxEntries := opts.MaxEntries
	if maxEntries <= 0 {
		maxEntries = defaultCodeMaxEntries
	}
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	return &CodeStore{
		entries:    make(map[string]codeEntry),
		ttl:        ttl,
		maxEntries: maxEntries,
		now:        now,
	}
}

func (s *CodeStore) Create(tokens TokenSet) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := s.now()
	s.evictExpiredLocked(now)
	if len(s.entries) >= s.maxEntries {
		return "", fmt.Errorf("connect code store full")
	}

	code, err := generateCode()
	if err != nil {
		return "", err
	}
	for {
		if _, exists := s.entries[code]; !exists {
			break
		}
		code, err = generateCode()
		if err != nil {
			return "", err
		}
	}
	s.entries[code] = codeEntry{tokens: tokens, createdAt: now}
	return code, nil
}

func (s *CodeStore) Consume(code string) (TokenSet, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	entry, ok := s.entries[code]
	if !ok {
		return TokenSet{}, fmt.Errorf("unknown or expired connect code")
	}
	delete(s.entries, code)
	if s.now().Sub(entry.createdAt) > s.ttl {
		return TokenSet{}, fmt.Errorf("connect code expired")
	}
	return entry.tokens, nil
}

func (s *CodeStore) evictExpiredLocked(now time.Time) {
	for code, entry := range s.entries {
		if now.Sub(entry.createdAt) > s.ttl {
			delete(s.entries, code)
		}
	}
}

func generateCode() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate connect code: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
