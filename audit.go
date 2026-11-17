package flags

import (
	"context"
	"sync"
	"time"
)

// Audit actions recorded by the HTTP API.
const (
	ActionCreate = "create"
	ActionUpdate = "update"
	ActionDelete = "delete"
)

// DefaultAuditCapacity is how many entries an AuditLog keeps before the
// oldest are discarded.
const DefaultAuditCapacity = 10000

// AuditEntry records one change to a flag: who made it, when, and the flag's
// state before and after. Old is nil for creates and New is nil for deletes.
type AuditEntry struct {
	ID     uint64    `json:"id"`
	Time   time.Time `json:"time"`
	Actor  string    `json:"actor"`
	Env    string    `json:"env"`
	Key    string    `json:"key"`
	Action string    `json:"action"`
	Old    *flagJSON `json:"old"`
	New    *flagJSON `json:"new"`
}

// AuditLog is a thread-safe, bounded, in-memory log of flag changes.
type AuditLog struct {
	mu      sync.Mutex
	entries []AuditEntry
	nextID  uint64
	cap     int
	now     func() time.Time
}

// NewAuditLog returns a log keeping at most capacity entries (<=0 means
// DefaultAuditCapacity).
func NewAuditLog(capacity int) *AuditLog {
	if capacity <= 0 {
		capacity = DefaultAuditCapacity
	}
	return &AuditLog{cap: capacity, now: time.Now, nextID: 1}
}

// Record appends an entry, assigning its ID and timestamp, and returns it.
func (l *AuditLog) Record(actor, env, key, action string, old, new *Flag) AuditEntry {
	l.mu.Lock()
	defer l.mu.Unlock()
	e := AuditEntry{
		ID: l.nextID, Time: l.now().UTC(), Actor: actor, Env: env, Key: key, Action: action,
		Old: snapshot(old), New: snapshot(new),
	}
	l.nextID++
	l.entries = append(l.entries, e)
	if len(l.entries) > l.cap {
		l.entries = append([]AuditEntry(nil), l.entries[len(l.entries)-l.cap:]...)
	}
	return e
}

func snapshot(f *Flag) *flagJSON {
	if f == nil {
		return nil
	}
	j := toFlagJSON(*f)
	return &j
}

// AuditQuery filters AuditLog.Query. Empty fields match everything.
type AuditQuery struct {
	Env   string
	Key   string
	Actor string
	Limit int // max entries returned; <=0 means no limit
}

// Query returns matching entries, newest first.
func (l *AuditLog) Query(q AuditQuery) []AuditEntry {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := []AuditEntry{}
	for i := len(l.entries) - 1; i >= 0; i-- {
		e := l.entries[i]
		if (q.Env != "" && e.Env != q.Env) || (q.Key != "" && e.Key != q.Key) || (q.Actor != "" && e.Actor != q.Actor) {
			continue
		}
		out = append(out, e)
		if q.Limit > 0 && len(out) >= q.Limit {
			break
		}
	}
	return out
}

type actorKey struct{}

// WithActor returns a context carrying the name of the authenticated caller.
func WithActor(ctx context.Context, name string) context.Context {
	return context.WithValue(ctx, actorKey{}, name)
}

// ActorFrom returns the caller name set by WithActor, or "anonymous".
func ActorFrom(ctx context.Context) string {
	if s, _ := ctx.Value(actorKey{}).(string); s != "" {
		return s
	}
	return "anonymous"
}
