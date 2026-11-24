package flags

import (
	"sync"
	"time"
)

// CacheStats is a snapshot of cache effectiveness counters.
type CacheStats struct {
	Hits          uint64
	Misses        uint64
	Invalidations uint64
}

// CachedStore wraps any FlagStore with a read-through cache for single-flag
// lookups, which is what every evaluation performs. Writes made through the
// CachedStore invalidate the affected key immediately; a TTL bounds staleness
// for changes made to the underlying store behind its back.
//
// Only successful lookups are cached, so a flag created later is visible at
// once. List always goes to the backend.
type CachedStore struct {
	inner FlagStore
	ttl   time.Duration
	now   func() time.Time

	mu      sync.Mutex
	entries map[string]cacheEntry
	gen     uint64 // bumped on every invalidation to reject racing fills
	stats   CacheStats
}

type cacheEntry struct {
	flag    Flag
	expires time.Time
}

var _ FlagStore = (*CachedStore)(nil)

// NewCachedStore wraps inner. A ttl <= 0 means entries never expire and are
// only dropped by writes through this wrapper or Invalidate/InvalidateAll.
func NewCachedStore(inner FlagStore, ttl time.Duration) *CachedStore {
	return &CachedStore{inner: inner, ttl: ttl, now: time.Now, entries: make(map[string]cacheEntry)}
}

// Stats returns the current hit/miss/invalidation counters.
func (c *CachedStore) Stats() CacheStats {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.stats
}

// Invalidate drops the cached entry for key, if any.
func (c *CachedStore) Invalidate(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.gen++
	if _, ok := c.entries[key]; ok {
		delete(c.entries, key)
		c.stats.Invalidations++
	}
}

// InvalidateAll empties the cache.
func (c *CachedStore) InvalidateAll() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.gen++
	c.stats.Invalidations += uint64(len(c.entries))
	c.entries = make(map[string]cacheEntry)
}

func cloneFlag(f Flag) Flag {
	if f.Rules != nil {
		rules := make([]Rule, len(f.Rules))
		for i, r := range f.Rules {
			r.Values = append([]string(nil), r.Values...)
			rules[i] = r
		}
		f.Rules = rules
	}
	return f
}

// Get returns the flag, serving from cache when a fresh entry exists.
func (c *CachedStore) Get(key string) (Flag, error) {
	c.mu.Lock()
	if e, ok := c.entries[key]; ok && (c.ttl <= 0 || c.now().Before(e.expires)) {
		c.stats.Hits++
		c.mu.Unlock()
		return cloneFlag(e.flag), nil
	}
	c.stats.Misses++
	gen := c.gen
	c.mu.Unlock()

	f, err := c.inner.Get(key)
	if err != nil {
		return Flag{}, err
	}

	c.mu.Lock()
	// Skip the fill if a write invalidated the cache while we were loading;
	// the value we read may already be stale.
	if c.gen == gen {
		c.entries[key] = cacheEntry{flag: cloneFlag(f), expires: c.now().Add(c.ttl)}
	}
	c.mu.Unlock()
	return f, nil
}

// write runs a mutation against the backend and invalidates key afterwards,
// whether or not it succeeded (a failed write may still have partly applied).
func (c *CachedStore) write(key string, fn func() error) error {
	err := fn()
	c.Invalidate(key)
	return err
}

func (c *CachedStore) Create(key string, enabled bool) error {
	return c.write(key, func() error { return c.inner.Create(key, enabled) })
}

func (c *CachedStore) Set(key string, enabled bool) error {
	return c.write(key, func() error { return c.inner.Set(key, enabled) })
}

func (c *CachedStore) Delete(key string) error {
	return c.write(key, func() error { return c.inner.Delete(key) })
}

func (c *CachedStore) SetRollout(key string, percent int) error {
	return c.write(key, func() error { return c.inner.SetRollout(key, percent) })
}

func (c *CachedStore) AddRule(key string, r Rule) error {
	return c.write(key, func() error { return c.inner.AddRule(key, r) })
}

func (c *CachedStore) ClearRules(key string) error {
	return c.write(key, func() error { return c.inner.ClearRules(key) })
}

// List always reads from the backend.
func (c *CachedStore) List() []Flag { return c.inner.List() }

// IsEnabled reports whether the flag is fully on for everyone.
func (c *CachedStore) IsEnabled(key string) bool {
	f, err := c.Get(key)
	if err != nil {
		return false
	}
	return f.Enabled && f.Rollout >= 100
}

// Evaluate evaluates the flag for ctx using the cached definition. Unknown
// flags evaluate to false.
func (c *CachedStore) Evaluate(key string, ctx Context) bool {
	f, err := c.Get(key)
	if err != nil {
		return false
	}
	return evaluateFlag(f, ctx)
}

func (c *CachedStore) IsEnabledFor(key, userID string) bool {
	return c.Evaluate(key, Context{UserID: userID})
}
