package client

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	flags "github.com/saif177/feature-flag-service"
)

type clock struct{ t time.Time }

func (c *clock) now() time.Time { return c.t }

func setup(t *testing.T, o Options) (*Client, flags.FlagStore, *int32, *clock) {
	t.Helper()
	envs := flags.NewEnvironments()
	store, _ := envs.Env(flags.EnvDev)
	var calls int32
	srv := flags.NewServer(envs)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		srv.ServeHTTP(w, r)
	}))
	t.Cleanup(ts.Close)
	clk := &clock{t: time.Unix(1_000_000, 0)}
	o.BaseURL = ts.URL
	o.Now = clk.now
	c, err := New(o)
	if err != nil {
		t.Fatal(err)
	}
	return c, store, &calls, clk
}

func TestEnabledAndCaching(t *testing.T) {
	c, store, calls, clk := setup(t, Options{TTL: time.Minute})
	store.Create("f", true)
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		on, err := c.Enabled(ctx, "f", EvalContext{UserID: "u"}, false)
		if err != nil || !on {
			t.Fatalf("got %v, %v", on, err)
		}
	}
	if n := atomic.LoadInt32(calls); n != 1 {
		t.Fatalf("calls = %d, want 1", n)
	}
	// Server-side change is invisible until TTL expiry...
	store.Set("f", false)
	if on, _ := c.Enabled(ctx, "f", EvalContext{UserID: "u"}, true); !on {
		t.Fatal("expected cached true")
	}
	clk.t = clk.t.Add(2 * time.Minute)
	if on, _ := c.Enabled(ctx, "f", EvalContext{UserID: "u"}, true); on {
		t.Fatal("expected refreshed false")
	}
	if s := c.Stats(); s.Hits != 3 || s.Misses != 2 {
		t.Fatalf("stats = %+v", s)
	}
}

func TestInvalidate(t *testing.T) {
	c, store, _, _ := setup(t, Options{})
	store.Create("f", true)
	ctx := context.Background()
	c.Enabled(ctx, "f", EvalContext{UserID: "a"}, false)
	c.Enabled(ctx, "f", EvalContext{UserID: "b"}, false)
	store.Set("f", false)
	c.Invalidate("f")
	for _, u := range []string{"a", "b"} {
		if on, _ := c.Enabled(ctx, "f", EvalContext{UserID: u}, true); on {
			t.Fatalf("user %s still sees cached value", u)
		}
	}
}

func TestContextSentToServer(t *testing.T) {
	c, store, _, _ := setup(t, Options{})
	store.Create("f", true)
	store.SetRollout("f", 0)
	store.AddRule("f", flags.Rule{Attribute: "email", Operator: flags.OpEndsWith, Values: []string{"@acme.com"}})
	ctx := context.Background()
	if on, _ := c.Enabled(ctx, "f", EvalContext{UserID: "u", Attributes: map[string]string{"email": "x@acme.com"}}, false); !on {
		t.Fatal("rule should match")
	}
	if on, _ := c.Enabled(ctx, "f", EvalContext{UserID: "u", Attributes: map[string]string{"email": "x@other.com"}}, true); on {
		t.Fatal("rule should not match")
	}
}

func TestNotFoundReturnsDefault(t *testing.T) {
	c, _, _, _ := setup(t, Options{})
	on, err := c.Enabled(context.Background(), "missing", EvalContext{}, true)
	if !IsNotFound(err) || !on {
		t.Fatalf("got %v, %v", on, err)
	}
}

func TestStaleServedWhenServiceDown(t *testing.T) {
	envs := flags.NewEnvironments()
	dev, _ := envs.Env(flags.EnvDev)
	dev.Create("f", true)
	ts := httptest.NewServer(flags.NewServer(envs))
	clk := &clock{t: time.Unix(1_000_000, 0)}
	c, _ := New(Options{BaseURL: ts.URL, TTL: time.Minute, StaleTTL: 10 * time.Minute, Now: clk.now})
	ctx := context.Background()
	if on, err := c.Enabled(ctx, "f", EvalContext{UserID: "u"}, false); !on || err != nil {
		t.Fatal(on, err)
	}
	ts.Close()

	clk.t = clk.t.Add(5 * time.Minute) // expired but within stale window
	on, err := c.Enabled(ctx, "f", EvalContext{UserID: "u"}, false)
	if !on || err == nil {
		t.Fatalf("want stale true with error, got %v, %v", on, err)
	}
	clk.t = clk.t.Add(20 * time.Minute) // beyond stale window
	on, err = c.Enabled(ctx, "f", EvalContext{UserID: "u"}, false)
	if on || err == nil {
		t.Fatalf("want default false with error, got %v, %v", on, err)
	}
	if s := c.Stats(); s.StaleServed != 1 || s.Errors != 2 {
		t.Fatalf("stats = %+v", s)
	}
}

func TestAPIKeyAuth(t *testing.T) {
	envs := flags.NewEnvironments()
	dev, _ := envs.Env(flags.EnvDev)
	dev.Create("f", true)
	keys, err := flags.ParseAPIKeys("sdk:read:secret")
	if err != nil {
		t.Fatal(err)
	}
	auth, err := flags.NewAuthenticator(keys...)
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(auth.Middleware(flags.NewServer(envs)))
	defer ts.Close()

	good, _ := New(Options{BaseURL: ts.URL, APIKey: "secret"})
	if on, err := good.Enabled(context.Background(), "f", EvalContext{UserID: "u"}, false); !on || err != nil {
		t.Fatal(on, err)
	}
	bad, _ := New(Options{BaseURL: ts.URL, APIKey: "nope"})
	on, err := bad.Enabled(context.Background(), "f", EvalContext{UserID: "u"}, false)
	var se *StatusError
	if on || !errors.As(err, &se) || se.Status != http.StatusUnauthorized {
		t.Fatalf("got %v, %v", on, err)
	}
}

func TestNewValidatesURL(t *testing.T) {
	for _, u := range []string{"", "localhost:8080", "ftp://x"} {
		if _, err := New(Options{BaseURL: u}); err == nil {
			t.Errorf("expected error for %q", u)
		}
	}
}

func TestCacheKeyAttributeOrder(t *testing.T) {
	a := cacheKey("f", EvalContext{UserID: "u", Attributes: map[string]string{"a": "1", "b": "2"}})
	b := cacheKey("f", EvalContext{UserID: "u", Attributes: map[string]string{"b": "2", "a": "1"}})
	if a != b {
		t.Fatal("cache key must be order independent")
	}
}
