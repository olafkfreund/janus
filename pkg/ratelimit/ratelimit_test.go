package ratelimit

import (
	"sync"
	"testing"
)

func TestAllowExhaustsBurstThenBlocks(t *testing.T) {
	// rate 0 so no tokens refill during the test: exactly burst requests pass.
	l := New(0, 3)
	for i := 0; i < 3; i++ {
		if !l.Allow("a") {
			t.Fatalf("request %d within burst should be allowed", i)
		}
	}
	if l.Allow("a") {
		t.Fatal("request beyond burst should be denied")
	}
}

func TestKeysAreIndependent(t *testing.T) {
	// The whole point of the identity tier: one noisy tenant must not consume
	// another's budget.
	l := New(0, 1)
	if !l.Allow("tenant-a") {
		t.Fatal("first request for tenant-a should be allowed")
	}
	if l.Allow("tenant-a") {
		t.Fatal("tenant-a is out of tokens")
	}
	if !l.Allow("tenant-b") {
		t.Fatal("tenant-b must have its own bucket")
	}
}

func TestNilLimiterAllows(t *testing.T) {
	var l *Limiter
	if !l.Allow("anything") {
		t.Fatal("nil limiter must allow, so callers can leave it unset")
	}
	l.StartEvictionSweeper(0, 0) // must not panic
}

func TestConcurrentAllowGrantsExactlyBurst(t *testing.T) {
	// Guards the bucket arithmetic against races: with no refill, the total
	// number of grants across concurrent callers must equal burst exactly.
	const burst = 50
	l := New(0, burst)
	var wg sync.WaitGroup
	var mu sync.Mutex
	granted := 0
	for i := 0; i < 200; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if l.Allow("shared") {
				mu.Lock()
				granted++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if granted != burst {
		t.Fatalf("granted %d, want exactly %d", granted, burst)
	}
}
