package cache_test

import (
	"sync"
	"testing"
	"time"

	"github.com/jinto/kittypaw-api/internal/cache"
)

func TestSetGet(t *testing.T) {
	c := cache.New()
	defer c.Close()

	c.Set("key1", []byte("value1"), time.Hour)

	data, ok := c.Get("key1")
	if !ok {
		t.Fatal("expected cache hit")
	}
	if string(data) != "value1" {
		t.Fatalf("expected value1, got %q", string(data))
	}
}

func TestGetMiss(t *testing.T) {
	c := cache.New()
	defer c.Close()

	_, ok := c.Get("nonexistent")
	if ok {
		t.Fatal("expected cache miss")
	}
}

func TestTTLExpiry(t *testing.T) {
	c := cache.New()
	defer c.Close()

	c.Set("key1", []byte("value1"), 10*time.Millisecond)

	time.Sleep(20 * time.Millisecond)

	_, ok := c.Get("key1")
	if ok {
		t.Fatal("expected cache miss after TTL")
	}
}

func TestGetStale(t *testing.T) {
	c := cache.New()
	defer c.Close()

	c.Set("key1", []byte("stale-data"), 10*time.Millisecond)

	time.Sleep(20 * time.Millisecond)

	// Get should miss.
	_, ok := c.Get("key1")
	if ok {
		t.Fatal("expected fresh miss")
	}

	// GetStale should return stale data.
	data, stale, found := c.GetStale("key1")
	if !found {
		t.Fatal("expected GetStale to find entry")
	}
	if !stale {
		t.Fatal("expected stale=true")
	}
	if string(data) != "stale-data" {
		t.Fatalf("expected stale-data, got %q", string(data))
	}
}

func TestGetStaleNotFound(t *testing.T) {
	c := cache.New()
	defer c.Close()

	_, _, found := c.GetStale("nonexistent")
	if found {
		t.Fatal("expected not found")
	}
}

func TestConcurrentAccess(t *testing.T) {
	c := cache.New()
	defer c.Close()

	var wg sync.WaitGroup
	for i := range 100 {
		wg.Add(2)
		key := string(rune('a' + i%26))
		go func() {
			defer wg.Done()
			c.Set(key, []byte("data"), time.Hour)
		}()
		go func() {
			defer wg.Done()
			c.Get(key)
		}()
	}
	wg.Wait()
}
