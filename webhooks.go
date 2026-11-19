package flags

import (
	"bytes"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"sync"
	"time"
)

// ErrInvalidWebhook is returned for webhooks with a bad URL or unknown event.
var ErrInvalidWebhook = errors.New("invalid webhook")

// Webhook is a registered subscriber to flag changes. Env limits delivery to
// one environment ("" = all); Events limits it to the given audit actions
// (create/update/delete; empty = all). If Secret is set, each delivery is
// signed with HMAC-SHA256 in the X-Flags-Signature header ("sha256=<hex>").
type Webhook struct {
	ID     string   `json:"id"`
	URL    string   `json:"url"`
	Env    string   `json:"env"`
	Events []string `json:"events"`
	Secret string   `json:"-"`
}

func (w Webhook) matches(e AuditEntry) bool {
	if w.Env != "" && w.Env != e.Env {
		return false
	}
	if len(w.Events) == 0 {
		return true
	}
	for _, ev := range w.Events {
		if ev == e.Action {
			return true
		}
	}
	return false
}

// WebhookPayload is the JSON body POSTed to subscribers.
type WebhookPayload struct {
	Event string     `json:"event"` // always "flag.changed"
	Entry AuditEntry `json:"entry"`
}

// Webhooks keeps the registry of subscribers and delivers change events to
// them asynchronously, retrying failed deliveries with exponential backoff.
type Webhooks struct {
	mu    sync.Mutex
	hooks map[string]Webhook

	client      *http.Client
	maxAttempts int
	backoff     time.Duration
	queue       chan job
	wg          sync.WaitGroup
	closeOnce   sync.Once
	closed      chan struct{}
}

type job struct {
	hook    Webhook
	payload []byte
}

// WebhookOptions tunes delivery; zero values use sensible defaults.
type WebhookOptions struct {
	Client      *http.Client  // default: 5s timeout
	MaxAttempts int           // default 4
	Backoff     time.Duration // base delay, doubled per retry; default 1s
	Workers     int           // default 4
	QueueSize   int           // default 256; events are dropped when full
}

// NewWebhooks starts the delivery workers. Call Close to stop them.
func NewWebhooks(o WebhookOptions) *Webhooks {
	if o.Client == nil {
		o.Client = &http.Client{Timeout: 5 * time.Second}
	}
	if o.MaxAttempts <= 0 {
		o.MaxAttempts = 4
	}
	if o.Backoff <= 0 {
		o.Backoff = time.Second
	}
	if o.Workers <= 0 {
		o.Workers = 4
	}
	if o.QueueSize <= 0 {
		o.QueueSize = 256
	}
	w := &Webhooks{
		hooks: map[string]Webhook{}, client: o.Client, maxAttempts: o.MaxAttempts,
		backoff: o.Backoff, queue: make(chan job, o.QueueSize), closed: make(chan struct{}),
	}
	for i := 0; i < o.Workers; i++ {
		w.wg.Add(1)
		go w.worker()
	}
	return w
}

// Register validates and stores a webhook, assigning its ID.
func (w *Webhooks) Register(h Webhook) (Webhook, error) {
	u, err := url.Parse(h.URL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return Webhook{}, fmt.Errorf("%w: url must be an absolute http(s) URL", ErrInvalidWebhook)
	}
	for _, ev := range h.Events {
		if ev != ActionCreate && ev != ActionUpdate && ev != ActionDelete {
			return Webhook{}, fmt.Errorf("%w: unknown event %q", ErrInvalidWebhook, ev)
		}
	}
	if h.Events == nil {
		h.Events = []string{}
	}
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return Webhook{}, err
	}
	h.ID = "wh_" + hex.EncodeToString(b)
	w.mu.Lock()
	w.hooks[h.ID] = h
	w.mu.Unlock()
	return h, nil
}

// Delete removes a webhook; it reports whether it existed.
func (w *Webhooks) Delete(id string) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	_, ok := w.hooks[id]
	delete(w.hooks, id)
	return ok
}

// List returns webhooks sorted by ID, optionally only those scoped to env
// or to all environments.
func (w *Webhooks) List(env string) []Webhook {
	w.mu.Lock()
	defer w.mu.Unlock()
	out := []Webhook{}
	for _, h := range w.hooks {
		if env == "" || h.Env == "" || h.Env == env {
			out = append(out, h)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// Dispatch queues e for every matching webhook. It never blocks: if the
// queue is full the delivery is dropped.
func (w *Webhooks) Dispatch(e AuditEntry) {
	body, err := json.Marshal(WebhookPayload{Event: "flag.changed", Entry: e})
	if err != nil {
		return
	}
	for _, h := range w.List("") {
		if !h.matches(e) {
			continue
		}
		select {
		case <-w.closed:
			return
		default:
		}
		select {
		case w.queue <- job{h, body}:
		default:
		}
	}
}

// Close stops accepting work, drains queued deliveries (abandoning pending
// retries) and waits for the workers to exit.
func (w *Webhooks) Close() {
	w.closeOnce.Do(func() {
		close(w.closed)
		close(w.queue)
	})
	w.wg.Wait()
}

func (w *Webhooks) worker() {
	defer w.wg.Done()
	for j := range w.queue {
		w.deliver(j)
	}
}

func (w *Webhooks) deliver(j job) {
	delay := w.backoff
	for attempt := 1; attempt <= w.maxAttempts; attempt++ {
		if w.post(j) == nil || attempt == w.maxAttempts {
			return
		}
		select {
		case <-time.After(delay):
			delay *= 2
		case <-w.closed:
			return
		}
	}
}

func (w *Webhooks) post(j job) error {
	req, err := http.NewRequest(http.MethodPost, j.hook.URL, bytes.NewReader(j.payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "feature-flag-service-webhook")
	if j.hook.Secret != "" {
		req.Header.Set("X-Flags-Signature", SignPayload(j.hook.Secret, j.payload))
	}
	resp, err := w.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<16))
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("webhook %s: status %d", j.hook.ID, resp.StatusCode)
	}
	return nil
}

// SignPayload returns the X-Flags-Signature value for body under secret.
func SignPayload(secret string, body []byte) string {
	m := hmac.New(sha256.New, []byte(secret))
	m.Write(body)
	return "sha256=" + hex.EncodeToString(m.Sum(nil))
}
