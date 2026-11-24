package flags

import (
	"sync"
	"testing"
	"time"
)

// countingStore counts backend Get calls.
type countingStore struct {
	FlagStore
	mu   sync.Mutex
	gets int
}

func (c *countingStore) Get(key string) (Flag, error) {
	c.mu.Lock()
	c.gets++
	c.mu.Unlock()
	return c.FlagStore.Get(key)
}

func newCached(ttl time.Duration) (*CachedStore, *countingStore) {
	cs := &countingStore{FlagStore: NewStore()}
	return NewCachedStore(cs, ttl), cs
}

func TestCacheServesRepeatReads(t *testing.T) {
	c, backend := newCached(0)
	c.Create("f", true)
	for i := 0; i < 5; i++ {
		if !c.IsEnabledFor("f", "u1") {
			t.Fatal("expected enabled")
		}
	}
	if backend.gets != 1 {
		t.Fatalf("backend gets = %d, want 1", backend.gets)
	}
	st := c.Stats()
	if st.Hits != 4 || st.Misses != 1 {
		t.Fatalf("stats = %+v", st)
	}
}

func TestCacheInvalidatedByWrites(t *testing.T) {
	c, _ := newCached(0)
	c.Create("f", true)
	c.Evaluate("f", Context{UserID: "u"}) // warm

	c.Set("f", false)
	if c.IsEnabled("f") {
		t.Fatal("Set should invalidate")
	}
	c.Set("f", true)
	c.SetRollout("f", 0)
	if c.IsEnabledFor("f", "u") {
		t.Fatal("SetRollout should invalidate")
	}
	c.AddRule("f", Rule{Attribute: "email", Operator: OpEndsWith, Values: []string{"@acme.com"}})
	if !c.Evaluate("f", Context{UserID: "u", Attributes: map[string]string{"email": "a@acme.com"}}) {
		t.Fatal("AddRule should invalidate")
	}
	c.ClearRules("f")
	if c.Evaluate("f", Context{UserID: "u", Attributes: map[string]string{"email": "a@acme.com"}}) {
		t.Fatal("ClearRules should invalidate")
	}
	c.Delete("f")
	if _, err := c.Get("f"); err != ErrNotFound {
		t.Fatalf("Get after delete = %v", err)
	}
}

func TestCacheDoesNotCacheMissing(t *testing.T) {
	c, _ := newCached(0)
	if c.IsEnabled("nope") {
		t.Fatal("unknown flag enabled")
	}
	c.Create("nope", true)
	if !c.IsEnabled("nope") {
		t.Fatal("newly created flag should be visible")
	}
}

func TestCacheTTLExpiry(t *testing.T) {
	c, backend := newCached(time.Minute)
	now := time.Now()
	c.now = func() time.Time { return now }
	c.Create("f", true)
	c.Get("f")
	c.Get("f")
	if backend.gets != 1 {
		t.Fatalf("gets = %d, want 1", backend.gets)
	}

	// Change behind the cache's back: stale until TTL passes.
	backend.FlagStore.Set("f", false)
	if !c.IsEnabled("f") {
		t.Fatal("expected stale cached value within TTL")
	}
	now = now.Add(2 * time.Minute)
	if c.IsEnabled("f") {
		t.Fatal("expected refreshed value after TTL")
	}
	c.Invalidate("f")
	c.InvalidateAll()
}

func TestCacheReturnsIsolatedCopies(t *testing.T) {
	c, _ := newCached(0)
	c.Create("f", true)
	c.AddRule("f", Rule{Attribute: "user_id", Operator: OpEquals, Values: []string{"vip"}})
	f, _ := c.Get("f")
	f.Rules[0].Values[0] = "hacked"
	f.Enabled = false
	g, _ := c.Get("f")
	if !g.Enabled || g.Rules[0].Values[0] != "vip" {
		t.Fatalf("cache entry mutated via returned copy: %+v", g)
	}
}

func TestCacheMatchesUncachedEvaluation(t *testing.T) {
	c, _ := newCached(0)
	plain := NewStore()
	for _, s := range []FlagStore{c, plain} {
		s.Create("f", true)
		s.SetRollout("f", 40)
	}
	for i := 0; i < 200; i++ {
		u := string(rune('a'+i%26)) + string(rune('0'+i/26))
		if c.IsEnabledFor("f", u) != plain.IsEnabledFor("f", u) {
			t.Fatalf("mismatch for %s", u)
		}
	}
}

func TestCacheConcurrent(t *testing.T) {
	c, _ := newCached(time.Second)
	c.Create("f", true)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				if i == 0 {
					c.Set("f", j%2 == 0)
				} else {
					c.IsEnabledFor("f", "u")
				}
			}
		}(i)
	}
	wg.Wait()
	// Final state must agree with the backend after the last write.
	c.Set("f", true)
	if !c.IsEnabled("f") {
		t.Fatal("stale after final write")
	}
}
