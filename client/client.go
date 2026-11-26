// Package client is a small Go SDK for the feature-flag HTTP API. It wraps the
// evaluate endpoint with a local TTL cache so hot paths don't pay a network
// round-trip per check, and falls back to stale values (or a caller default)
// when the service is unreachable.
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"
)

// EvalContext describes who a flag is being evaluated for.
type EvalContext struct {
	UserID     string
	Attributes map[string]string
}

// Options configures a Client. Only BaseURL is required.
type Options struct {
	BaseURL    string        // e.g. "http://localhost:8080"
	Env        string        // environment name; default "dev"
	APIKey     string        // sent as a Bearer token when non-empty
	TTL        time.Duration // how long a result is served from cache; default 30s, <0 disables caching
	StaleTTL   time.Duration // how long an expired result may be served when the service is down; default 5m
	HTTPClient *http.Client  // default: 5s timeout
	Now        func() time.Time
}

// Stats reports cache effectiveness.
type Stats struct {
	Hits, Misses, StaleServed, Errors uint64
}

// StatusError is returned for non-2xx responses.
type StatusError struct {
	Status  int
	Message string
}

func (e *StatusError) Error() string {
	return fmt.Sprintf("flags api: status %d: %s", e.Status, e.Message)
}

// IsNotFound reports whether err is a 404 from the service.
func IsNotFound(err error) bool {
	var se *StatusError
	return errors.As(err, &se) && se.Status == http.StatusNotFound
}

type entry struct {
	enabled bool
	fetched time.Time
}

// Client evaluates flags against a remote service with local caching.
// It is safe for concurrent use.
type Client struct {
	base     string
	env      string
	apiKey   string
	ttl      time.Duration
	staleTTL time.Duration
	hc       *http.Client
	now      func() time.Time

	mu    sync.Mutex
	cache map[string]entry
	stats Stats
}

// New builds a Client.
func New(o Options) (*Client, error) {
	u, err := url.Parse(o.BaseURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, fmt.Errorf("client: invalid BaseURL %q", o.BaseURL)
	}
	c := &Client{
		base:     strings.TrimRight(o.BaseURL, "/"),
		env:      o.Env,
		apiKey:   o.APIKey,
		ttl:      o.TTL,
		staleTTL: o.StaleTTL,
		hc:       o.HTTPClient,
		now:      o.Now,
		cache:    map[string]entry{},
	}
	if c.env == "" {
		c.env = "dev"
	}
	if c.ttl == 0 {
		c.ttl = 30 * time.Second
	}
	if c.staleTTL == 0 {
		c.staleTTL = 5 * time.Minute
	}
	if c.hc == nil {
		c.hc = &http.Client{Timeout: 5 * time.Second}
	}
	if c.now == nil {
		c.now = time.Now
	}
	return c, nil
}

// Enabled reports whether flag key is on for ec. On a cache hit no request is
// made. If the service cannot be reached it serves a stale cached value when
// one exists, otherwise returns def together with the error — so callers can
// ignore the error and still get a safe answer.
func (c *Client) Enabled(ctx context.Context, key string, ec EvalContext, def bool) (bool, error) {
	ck := cacheKey(key, ec)
	now := c.now()

	c.mu.Lock()
	e, ok := c.cache[ck]
	if ok && c.ttl > 0 && now.Sub(e.fetched) < c.ttl {
		c.stats.Hits++
		c.mu.Unlock()
		return e.enabled, nil
	}
	c.stats.Misses++
	c.mu.Unlock()

	on, err := c.fetch(ctx, key, ec)
	c.mu.Lock()
	defer c.mu.Unlock()
	if err != nil {
		c.stats.Errors++
		// A 404 is authoritative: don't mask a deleted flag with stale data.
		if IsNotFound(err) {
			delete(c.cache, ck)
			return def, err
		}
		if ok && now.Sub(e.fetched) < c.ttl+c.staleTTL {
			c.stats.StaleServed++
			return e.enabled, err
		}
		return def, err
	}
	if c.ttl > 0 {
		c.cache[ck] = entry{enabled: on, fetched: c.now()}
	}
	return on, nil
}

// Invalidate drops cached results for key (all contexts).
func (c *Client) Invalidate(key string) {
	prefix := key + "\x00"
	c.mu.Lock()
	defer c.mu.Unlock()
	for k := range c.cache {
		if strings.HasPrefix(k, prefix) {
			delete(c.cache, k)
		}
	}
}

// Flush empties the whole cache.
func (c *Client) Flush() {
	c.mu.Lock()
	c.cache = map[string]entry{}
	c.mu.Unlock()
}

// Stats returns a snapshot of cache counters.
func (c *Client) Stats() Stats {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.stats
}

func (c *Client) fetch(ctx context.Context, key string, ec EvalContext) (bool, error) {
	body, _ := json.Marshal(map[string]any{"user_id": ec.UserID, "attributes": ec.Attributes})
	u := fmt.Sprintf("%s/v1/envs/%s/flags/%s/evaluate", c.base, url.PathEscape(c.env), url.PathEscape(key))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, bytes.NewReader(body))
	if err != nil {
		return false, err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return false, err
	}
	if resp.StatusCode/100 != 2 {
		var e struct {
			Error string `json:"error"`
		}
		msg := strings.TrimSpace(string(data))
		if json.Unmarshal(data, &e) == nil && e.Error != "" {
			msg = e.Error
		}
		return false, &StatusError{Status: resp.StatusCode, Message: msg}
	}
	var out struct {
		Enabled bool `json:"enabled"`
	}
	if err := json.Unmarshal(data, &out); err != nil {
		return false, fmt.Errorf("flags api: bad response: %w", err)
	}
	return out.Enabled, nil
}

// cacheKey is deterministic over attribute order. The flag key is the prefix
// so Invalidate can match it.
func cacheKey(key string, ec EvalContext) string {
	var b strings.Builder
	b.WriteString(key)
	b.WriteByte(0)
	b.WriteString(ec.UserID)
	attrs := make([]string, 0, len(ec.Attributes))
	for k := range ec.Attributes {
		attrs = append(attrs, k)
	}
	sort.Strings(attrs)
	for _, k := range attrs {
		b.WriteByte(0)
		b.WriteString(k)
		b.WriteByte('=')
		b.WriteString(ec.Attributes[k])
	}
	return b.String()
}
