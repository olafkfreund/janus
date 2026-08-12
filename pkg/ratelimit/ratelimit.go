// Package ratelimit provides a minimal, dependency-free token-bucket limiter
// keyed by an arbitrary string.
//
// It is used at two tiers, which exist for different reasons:
//
//   - Keyed by client IP, as outer middleware: a coarse DoS / brute-force guard
//     that must work before any authentication has happened.
//   - Keyed by authenticated client identity, inside the MCP request path:
//     per-tenant fairness. Enterprise MCP clients commonly egress through a
//     single NAT address, so IP keying alone would make one company's users
//     starve each other while an attacker on a unique IP got a full bucket.
//
// The identity tier deliberately runs only after auth succeeds — keying on an
// unvalidated bearer token would let an attacker mint a fresh bucket per
// request and bypass the limiter entirely.
//
// Limits are per-process. With N replicas the effective global limit is N times
// the configured rate; use a shared (Redis) limiter if an exact global ceiling
// is ever required.
package ratelimit

import (
	"sync"
	"time"
)

type bucket struct {
	tokens float64
	last   time.Time
}

// Limiter is a concurrency-safe token-bucket rate limiter.
type Limiter struct {
	mu      sync.Mutex
	buckets map[string]*bucket
	rate    float64 // tokens per second
	burst   float64
}

// New returns a limiter granting rate tokens per second up to burst.
func New(rate, burst float64) *Limiter {
	return &Limiter{buckets: make(map[string]*bucket), rate: rate, burst: burst}
}

// Allow reports whether a request under the given key may proceed, consuming a
// token if so. A nil Limiter allows everything, so callers can leave it unset.
func (l *Limiter) Allow(key string) bool {
	if l == nil {
		return true
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	b, ok := l.buckets[key]
	if !ok {
		l.buckets[key] = &bucket{tokens: l.burst - 1, last: now}
		return true
	}
	b.tokens += now.Sub(b.last).Seconds() * l.rate
	if b.tokens > l.burst {
		b.tokens = l.burst
	}
	b.last = now
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

// StartEvictionSweeper runs a background goroutine that periodically deletes
// buckets idle longer than ttl, preventing the map from growing without bound
// as distinct keys churn through over time.
func (l *Limiter) StartEvictionSweeper(interval, ttl time.Duration) {
	if l == nil {
		return
	}
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for range ticker.C {
			cutoff := time.Now().Add(-ttl)
			l.mu.Lock()
			for key, b := range l.buckets {
				if b.last.Before(cutoff) {
					delete(l.buckets, key)
				}
			}
			l.mu.Unlock()
		}
	}()
}
