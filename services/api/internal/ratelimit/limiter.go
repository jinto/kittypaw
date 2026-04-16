package ratelimit

import (
	"sync"
	"time"
)

type window struct {
	count   int
	resetAt time.Time
}

type Limiter struct {
	mu      sync.Mutex
	windows map[string]*window
	daily   map[string]*window
	stop    chan struct{}
	nowFunc func() time.Time // for testing
}

func New() *Limiter {
	return NewWithClock(nil)
}

func NewWithClock(nowFunc func() time.Time) *Limiter {
	l := &Limiter{
		windows: make(map[string]*window),
		daily:   make(map[string]*window),
		stop:    make(chan struct{}),
		nowFunc: nowFunc,
	}
	go l.sweep()
	return l
}

func (l *Limiter) Close() {
	close(l.stop)
}

func (l *Limiter) now() time.Time {
	if l.nowFunc != nil {
		return l.nowFunc()
	}
	return time.Now()
}

// Allow checks if a request is allowed under the per-minute limit.
func (l *Limiter) Allow(key string, limit int) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.now()
	w, ok := l.windows[key]
	if !ok || now.After(w.resetAt) {
		l.windows[key] = &window{count: 1, resetAt: now.Add(time.Minute)}
		return true
	}

	if w.count >= limit {
		return false
	}
	w.count++
	return true
}

// AllowDaily checks if a request is allowed under the daily limit.
func (l *Limiter) AllowDaily(key string, limit int) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.now()
	w, ok := l.daily[key]
	if !ok || now.After(w.resetAt) {
		// Reset at midnight or 24h from first request.
		l.daily[key] = &window{count: 1, resetAt: now.Truncate(24 * time.Hour).Add(24 * time.Hour)}
		return true
	}

	if w.count >= limit {
		return false
	}
	w.count++
	return true
}

// SecondsUntilReset returns seconds until the per-minute window resets for a key.
func (l *Limiter) SecondsUntilReset(key string) int { return l.secondsUntil(l.windows, key) }

// SecondsUntilDailyReset returns seconds until the daily window resets for a key.
func (l *Limiter) SecondsUntilDailyReset(key string) int { return l.secondsUntil(l.daily, key) }

func (l *Limiter) secondsUntil(m map[string]*window, key string) int {
	l.mu.Lock()
	defer l.mu.Unlock()

	w, ok := m[key]
	if !ok {
		return 0
	}
	d := time.Until(w.resetAt)
	if d <= 0 {
		return 0
	}
	return int(d.Seconds()) + 1
}

func (l *Limiter) sweep() {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			l.mu.Lock()
			now := time.Now()
			for k, w := range l.windows {
				if now.After(w.resetAt) {
					delete(l.windows, k)
				}
			}
			for k, w := range l.daily {
				if now.After(w.resetAt) {
					delete(l.daily, k)
				}
			}
			l.mu.Unlock()
		case <-l.stop:
			return
		}
	}
}
