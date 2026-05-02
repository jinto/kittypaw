package server

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/kittypaw-app/kittykakao/internal/config"
	"github.com/kittypaw-app/kittykakao/internal/relay"
)

var (
	errSessionClosed = errors.New("websocket session closed")
	errSessionFull   = errors.New("websocket session send buffer full")
)

type Store interface {
	TokenExists(ctx context.Context, token string) (bool, error)
	PutToken(ctx context.Context, token string) error
	GetUserMapping(ctx context.Context, kakaoID string) (string, bool, error)
	PutUserMapping(ctx context.Context, kakaoID, token string) error
	GetKillswitch(ctx context.Context) (bool, error)
	SetKillswitch(ctx context.Context, enabled bool) error
	PutPending(ctx context.Context, actionID string, pending relay.PendingContext) error
	TakePending(ctx context.Context, actionID string) (relay.PendingContext, bool, error)
	CheckRateLimit(ctx context.Context, dailyLimit, monthlyLimit uint64) (relay.RateLimitResult, error)
	GetStats(ctx context.Context) (relay.Stats, error)
	CleanupExpiredPending(ctx context.Context, maxAgeSeconds int64) (uint64, error)
}

type State struct {
	Config     config.Config
	Store      Store
	HTTPClient *http.Client
	Version    string
	Commit     string

	mu            sync.RWMutex
	sessions      map[string]*wsSession
	pairCodes     *ttlCache[string]
	pairedMarkers *ttlCache[bool]
}

func NewState(cfg config.Config, store Store, version, commit string) *State {
	if version == "" {
		version = "dev"
	}
	if commit == "" {
		commit = "unknown"
	}
	return &State{
		Config: cfg,
		Store:  store,
		HTTPClient: &http.Client{
			Timeout: 30 * time.Second,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
		Version:       version,
		Commit:        commit,
		sessions:      make(map[string]*wsSession),
		pairCodes:     newTTLCache[string](5 * time.Minute),
		pairedMarkers: newTTLCache[bool](10 * time.Minute),
	}
}

func (s *State) StartPendingSweeper(ctx context.Context, interval time.Duration, maxAgeSeconds int64) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			n, err := s.Store.CleanupExpiredPending(ctx, maxAgeSeconds)
			if err != nil {
				slog.Warn("pending callback sweeper failed", "err", err)
				continue
			}
			if n > 0 {
				slog.Info("pending callback sweeper cleaned entries", "count", n)
			}
		}
	}
}

func (s *State) setSession(token string, session *wsSession) *wsSession {
	s.mu.Lock()
	defer s.mu.Unlock()
	old := s.sessions[token]
	s.sessions[token] = session
	return old
}

func (s *State) getSession(token string) (*wsSession, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	session, ok := s.sessions[token]
	return session, ok
}

func (s *State) removeSession(token string, session *wsSession) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.sessions[token] == session {
		delete(s.sessions, token)
	}
}

func (s *State) sessionCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.sessions)
}

type wsSession struct {
	send      chan relay.WSOutgoing
	done      chan struct{}
	closeOnce sync.Once
}

func newWSSession() *wsSession {
	return &wsSession{
		send: make(chan relay.WSOutgoing, 64),
		done: make(chan struct{}),
	}
}

func (s *wsSession) Send(frame relay.WSOutgoing) error {
	select {
	case <-s.done:
		return errSessionClosed
	default:
	}
	select {
	case s.send <- frame:
		return nil
	case <-s.done:
		return errSessionClosed
	default:
		return errSessionFull
	}
}

func (s *wsSession) Close() {
	s.closeOnce.Do(func() {
		close(s.done)
	})
}
