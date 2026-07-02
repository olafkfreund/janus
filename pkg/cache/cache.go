// Package cache provides a minimal, dependency-free, generic TTL cache suitable
// for per-process caching of hot read paths (config topology, secrets, idempotent
// responses). For cross-pod consistency, back these with Redis in phase 2.
package cache

import (
	"sync"
	"time"
)

// janitorInterval is how often the background sweep evicts expired entries.
// Kept coarse since eviction is best-effort housekeeping, not a correctness
// requirement (Get already treats expired entries as misses).
const janitorInterval = 90 * time.Second

type entry[V any] struct {
	value   V
	expires time.Time
}

// TTLCache is a concurrency-safe map with per-entry expiry.
type TTLCache[V any] struct {
	mu          sync.RWMutex
	ttl         time.Duration
	m           map[string]entry[V]
	janitorOnce sync.Once
}

// New returns a cache with the given default TTL. A ttl <= 0 disables caching:
// Get always misses and Set is a no-op, so callers can wire it unconditionally.
func New[V any](ttl time.Duration) *TTLCache[V] {
	return &TTLCache[V]{ttl: ttl, m: make(map[string]entry[V])}
}

// Enabled reports whether the cache stores anything (ttl > 0).
func (c *TTLCache[V]) Enabled() bool { return c != nil && c.ttl > 0 }

// Get returns the value for key if present and unexpired.
func (c *TTLCache[V]) Get(key string) (V, bool) {
	var zero V
	if !c.Enabled() {
		return zero, false
	}
	c.mu.RLock()
	e, ok := c.m[key]
	c.mu.RUnlock()
	if !ok || time.Now().After(e.expires) {
		return zero, false
	}
	return e.value, true
}

// Set stores value under key with the cache's TTL.
func (c *TTLCache[V]) Set(key string, value V) {
	if !c.Enabled() {
		return
	}
	c.startJanitor()
	c.mu.Lock()
	c.m[key] = entry[V]{value: value, expires: time.Now().Add(c.ttl)}
	c.mu.Unlock()
}

// startJanitor lazily launches a single background goroutine that
// periodically sweeps expired entries out of the map. Without this, expired
// entries that are never Set again (e.g. one-off keys in a high-cardinality
// response cache) sit in the map forever, growing memory unboundedly. Only
// called from Set, so a disabled cache (ttl <= 0) never starts a janitor.
func (c *TTLCache[V]) startJanitor() {
	c.janitorOnce.Do(func() {
		go c.janitor()
	})
}

func (c *TTLCache[V]) janitor() {
	ticker := time.NewTicker(janitorInterval)
	defer ticker.Stop()
	for range ticker.C {
		c.evictExpired()
	}
}

// evictExpired removes all entries whose TTL has passed.
func (c *TTLCache[V]) evictExpired() {
	now := time.Now()
	c.mu.Lock()
	for k, e := range c.m {
		if now.After(e.expires) {
			delete(c.m, k)
		}
	}
	c.mu.Unlock()
}

// Delete removes a single key.
func (c *TTLCache[V]) Delete(key string) {
	if c == nil {
		return
	}
	c.mu.Lock()
	delete(c.m, key)
	c.mu.Unlock()
}

// Purge clears the entire cache (used to invalidate on writes).
func (c *TTLCache[V]) Purge() {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.m = make(map[string]entry[V])
	c.mu.Unlock()
}
