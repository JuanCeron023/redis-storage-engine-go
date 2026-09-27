// Package store is CacheNova's concurrent, TTL-aware key/value store.
//
// It supports the subset of Redis semantics needed for the target use cases —
// caching, sessions, and rate-limit counters: string GET/SET with optional
// expiry, atomic INCR/DECR, DEL/EXISTS, and TTL introspection. Expiration is
// both lazy (on access) and active (a background sweeper), matching how Redis
// reclaims expired keys.
package store

import (
	"sync"
	"sync/atomic"
	"time"
)

type entry struct {
	value     []byte
	expiresAt time.Time // zero == no expiry
}

func (e entry) expired(now time.Time) bool {
	return !e.expiresAt.IsZero() && now.After(e.expiresAt)
}

// Store is a sharded-by-single-mutex KV store. For the target workloads a
// single RWMutex is more than fast enough; the design leaves room to shard by
// key hash later without changing the public API.
type Store struct {
	mu   sync.RWMutex
	data map[string]entry

	// Observability counters (atomic so the dashboard can read them cheaply).
	hits   atomic.Int64
	misses atomic.Int64
	ops    atomic.Int64

	stop chan struct{}
}

// New creates a store and starts the active-expiration sweeper.
func New() *Store {
	s := &Store{data: make(map[string]entry), stop: make(chan struct{})}
	go s.sweep()
	return s
}

// Close stops the background sweeper.
func (s *Store) Close() { close(s.stop) }

// Set stores value under key. A ttl of 0 means no expiry.
func (s *Store) Set(key string, value []byte, ttl time.Duration) {
	s.ops.Add(1)
	var exp time.Time
	if ttl > 0 {
		exp = time.Now().Add(ttl)
	}
	s.mu.Lock()
	s.data[key] = entry{value: value, expiresAt: exp}
	s.mu.Unlock()
}

// Get returns the value for key, applying lazy expiration.
func (s *Store) Get(key string) ([]byte, bool) {
	s.ops.Add(1)
	now := time.Now()
	s.mu.RLock()
	e, ok := s.data[key]
	s.mu.RUnlock()
	if !ok || e.expired(now) {
		if ok { // expired: delete lazily
			s.mu.Lock()
			if e2, ok2 := s.data[key]; ok2 && e2.expired(time.Now()) {
				delete(s.data, key)
			}
			s.mu.Unlock()
		}
		s.misses.Add(1)
		return nil, false
	}
	s.hits.Add(1)
	return e.value, true
}

// Del removes keys and returns how many were present.
func (s *Store) Del(keys ...string) int {
	s.ops.Add(1)
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, k := range keys {
		if _, ok := s.data[k]; ok {
			delete(s.data, k)
			n++
		}
	}
	return n
}

// Exists returns how many of keys currently exist (and are not expired).
func (s *Store) Exists(keys ...string) int {
	now := time.Now()
	s.mu.RLock()
	defer s.mu.RUnlock()
	n := 0
	for _, k := range keys {
		if e, ok := s.data[k]; ok && !e.expired(now) {
			n++
		}
	}
	return n
}

// Incr atomically increments the integer value at key by delta and returns the
// new value. Non-existent keys start at 0. Returns an error if the existing
// value is not an integer.
func (s *Store) Incr(key string, delta int64) (int64, error) {
	s.ops.Add(1)
	s.mu.Lock()
	defer s.mu.Unlock()
	var cur int64
	if e, ok := s.data[key]; ok && !e.expired(time.Now()) {
		n, err := parseInt(e.value)
		if err != nil {
			return 0, err
		}
		cur = n
	}
	cur += delta
	// Preserve existing TTL if present.
	exp := time.Time{}
	if e, ok := s.data[key]; ok {
		exp = e.expiresAt
	}
	s.data[key] = entry{value: []byte(formatInt(cur)), expiresAt: exp}
	return cur, nil
}

// TTL returns remaining seconds: -2 if the key is missing, -1 if it has no
// expiry, otherwise the number of whole seconds left.
func (s *Store) TTL(key string) int64 {
	now := time.Now()
	s.mu.RLock()
	defer s.mu.RUnlock()
	e, ok := s.data[key]
	if !ok || e.expired(now) {
		return -2
	}
	if e.expiresAt.IsZero() {
		return -1
	}
	return int64(time.Until(e.expiresAt).Seconds())
}

// Snapshot returns a copy of live keys for persistence/dashboard use.
func (s *Store) Snapshot() map[string][]byte {
	now := time.Now()
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make(map[string][]byte, len(s.data))
	for k, e := range s.data {
		if !e.expired(now) {
			cp := make([]byte, len(e.value))
			copy(cp, e.value)
			out[k] = cp
		}
	}
	return out
}

// Stats is a snapshot of runtime counters for the dashboard.
type Stats struct {
	Keys   int   `json:"keys"`
	Hits   int64 `json:"hits"`
	Misses int64 `json:"misses"`
	Ops    int64 `json:"ops"`
}

// Stats returns current counters.
func (s *Store) Stats() Stats {
	s.mu.RLock()
	keys := len(s.data)
	s.mu.RUnlock()
	return Stats{Keys: keys, Hits: s.hits.Load(), Misses: s.misses.Load(), Ops: s.ops.Load()}
}

// sweep actively evicts expired keys so memory doesn't grow with untouched
// expired entries. It samples the whole map every second; for the target
// dataset sizes this is cheap and predictable.
func (s *Store) sweep() {
	t := time.NewTicker(time.Second)
	defer t.Stop()
	for {
		select {
		case <-s.stop:
			return
		case <-t.C:
			now := time.Now()
			s.mu.Lock()
			for k, e := range s.data {
				if e.expired(now) {
					delete(s.data, k)
				}
			}
			s.mu.Unlock()
		}
	}
}
