package authmw

import (
	"sync"
	"time"
)

// RateLimiter is a fixed-window per-token request counter. Good enough for
// single-node llm-proxy; replace with a store-backed limiter if the data
// plane ever runs on multiple nodes.
type RateLimiter struct {
	mu      sync.Mutex
	windows map[string]*rateWindow
}

type rateWindow struct {
	start time.Time
	count int64
}

func NewRateLimiter() *RateLimiter {
	return &RateLimiter{windows: map[string]*rateWindow{}}
}

// Allow reports whether a request for tokenID fits under rpm requests per
// minute. A nil rpm means unlimited.
func (l *RateLimiter) Allow(tokenID string, rpm *int64, now time.Time) bool {
	if rpm == nil {
		return true
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	w, ok := l.windows[tokenID]
	if !ok || now.Sub(w.start) >= time.Minute {
		l.windows[tokenID] = &rateWindow{start: now, count: 1}
		return *rpm >= 1
	}
	if w.count >= *rpm {
		return false
	}
	w.count++
	return true
}

// TokenLocks serializes dispatch for a single token. Budget checks depend on
// counters that are updated after inference finishes, so requests for the same
// token must not all pass the preflight check concurrently.
type TokenLocks struct {
	mu    sync.Mutex
	locks map[string]*sync.Mutex
}

func NewTokenLocks() *TokenLocks {
	return &TokenLocks{locks: map[string]*sync.Mutex{}}
}

func (l *TokenLocks) Lock(tokenID string) func() {
	l.mu.Lock()
	lock, ok := l.locks[tokenID]
	if !ok {
		lock = &sync.Mutex{}
		l.locks[tokenID] = lock
	}
	l.mu.Unlock()

	lock.Lock()
	return lock.Unlock
}
