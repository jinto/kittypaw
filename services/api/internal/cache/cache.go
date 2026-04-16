package cache

import (
	"sync"
	"time"
)

type entry struct {
	data      []byte
	fetchedAt time.Time
	ttl       time.Duration
}

func (e *entry) expired() bool {
	return time.Since(e.fetchedAt) > e.ttl
}

type Cache struct {
	mu      sync.RWMutex
	entries map[string]*entry
	stop    chan struct{}
}

func New() *Cache {
	c := &Cache{
		entries: make(map[string]*entry),
		stop:    make(chan struct{}),
	}
	go c.sweep()
	return c
}

func (c *Cache) Close() {
	close(c.stop)
}

func (c *Cache) Set(key string, data []byte, ttl time.Duration) {
	c.mu.Lock()
	c.entries[key] = &entry{
		data:      data,
		fetchedAt: time.Now(),
		ttl:       ttl,
	}
	c.mu.Unlock()
}

func (c *Cache) Get(key string) ([]byte, bool) {
	c.mu.RLock()
	e, ok := c.entries[key]
	c.mu.RUnlock()

	if !ok || e.expired() {
		return nil, false
	}
	return e.data, true
}

// GetStale returns the cached data even if expired.
// Returns (data, stale, found).
func (c *Cache) GetStale(key string) ([]byte, bool, bool) {
	c.mu.RLock()
	e, ok := c.entries[key]
	c.mu.RUnlock()

	if !ok {
		return nil, false, false
	}
	return e.data, e.expired(), true
}

func (c *Cache) sweep() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			c.mu.Lock()
			for k, e := range c.entries {
				// Remove entries that have been expired for more than 2x their TTL.
				if time.Since(e.fetchedAt) > 2*e.ttl {
					delete(c.entries, k)
				}
			}
			c.mu.Unlock()
		case <-c.stop:
			return
		}
	}
}
