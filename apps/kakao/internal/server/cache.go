package server

import (
	"sync"
	"time"
)

type ttlCache[T any] struct {
	mu      sync.Mutex
	ttl     time.Duration
	entries map[string]ttlEntry[T]
}

type ttlEntry[T any] struct {
	value     T
	expiresAt time.Time
}

func newTTLCache[T any](ttl time.Duration) *ttlCache[T] {
	return &ttlCache[T]{
		ttl:     ttl,
		entries: make(map[string]ttlEntry[T]),
	}
}

func (c *ttlCache[T]) Set(key string, value T) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[key] = ttlEntry[T]{
		value:     value,
		expiresAt: time.Now().Add(c.ttl),
	}
}

func (c *ttlCache[T]) Get(key string) (T, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.entries[key]
	var zero T
	if !ok {
		return zero, false
	}
	if time.Now().After(entry.expiresAt) {
		delete(c.entries, key)
		return zero, false
	}
	return entry.value, true
}

func (c *ttlCache[T]) Delete(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.entries, key)
}
