package flags

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestAuditLogBoundedAndFiltered(t *testing.T) {
	l := NewAuditLog(3)
	for _, k := range []string{"a", "b", "a", "c", "a"} {
		l.Record("u", "dev", k, ActionCreate, nil, &Flag{Key: k})
	}
	all := l.Query(AuditQuery{})
	if len(all) != 3 || all[0].ID != 5 || all[2].ID != 3 {
		t.Fatalf("bounded newest-first: %+v", all)
	}
	if got := l.Query(AuditQuery{Key: "a", Limit: 1}); len(got) != 1 || got[0].ID != 5 {
		t.Fatalf("filter+limit: %+v", got)
	}
	if got := l.Query(AuditQuery{Env: "prod"}); len(got) != 0 {
		t.Fatalf("env filter: %+v", got)
	}
}

func TestAuditEntryTimestamp(t *testing.T) {
	l := NewAuditLog(0)
	fixed := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	l.now = func() time.Time { return fixed }
	if e := l.Record("x", "dev", "k", ActionDelete, &Flag{Key: "k"}, nil); !e.Time.Equal(fixed) || e.New != nil || e.Old == nil {
		t.Fatalf("entry: %+v", e)
	}
}

func TestAPIAuditTrail(t *testing.T) {
	h := NewServer(NewEnvironments())
	do(t, h, "POST", "/v1/envs/dev/flags", `{"key":"f","enabled":false}`)
	do(t, h, "PATCH", "/v1/envs/dev/flags/f", `{"enabled":true,"rollout":40}`)
	do(t, h, "PATCH", "/v1/envs/dev/flags/missing", `{"enabled":true}`) // 404: not logged
	do(t, h, "PATCH", "/v1/envs/dev/flags/f", `{"rollout":500}`)        // 400: not logged
	do(t, h, "DELETE", "/v1/envs/dev/flags/f", "")
	do(t, h, "POST", "/v1/envs/prod/flags", `{"key":"g"}`)

	code, out := do(t, h, "GET", "/v1/envs/dev/audit", "")
	if code != 200 {
		t.Fatalf("audit = %d", code)
	}
	es := out["entries"].([]any)
	if len(es) != 3 {
		t.Fatalf("want 3 dev entries, got %v", es)
	}
	del, upd, cre := es[0].(map[string]any), es[1].(map[string]any), es[2].(map[string]any)
	if del["action"] != "delete" || del["new"] != nil || del["old"] == nil {
		t.Fatalf("delete entry: %v", del)
	}
	if upd["action"] != "update" || upd["actor"] != "anonymous" {
		t.Fatalf("update entry: %v", upd)
	}
	o, n := upd["old"].(map[string]any), upd["new"].(map[string]any)
	if o["enabled"] != false || n["enabled"] != true || o["rollout"] != float64(100) || n["rollout"] != float64(40) {
		t.Fatalf("old/new: %v -> %v", o, n)
	}
	if cre["action"] != "create" || cre["old"] != nil {
		t.Fatalf("create entry: %v", cre)
	}

	_, out = do(t, h, "GET", "/v1/envs/dev/audit?limit=1", "")
	if len(out["entries"].([]any)) != 1 {
		t.Fatalf("limit: %v", out)
	}
	if code, _ = do(t, h, "GET", "/v1/envs/dev/audit?limit=x", ""); code != 400 {
		t.Fatalf("bad limit = %d", code)
	}
	if code, _ = do(t, h, "GET", "/v1/envs/nope/audit", ""); code != 404 {
		t.Fatalf("unknown env = %d", code)
	}
}

func TestAuditRecordsAuthenticatedActor(t *testing.T) {
	auth, err := NewAuthenticator(APIKey{"alice", "k1", ScopeWrite}, APIKey{"bob", "k2", ScopeRead})
	if err != nil {
		t.Fatal(err)
	}
	srv := NewServer(NewEnvironments())
	h := auth.Middleware(srv)
	req := httptest.NewRequest("POST", "/v1/envs/dev/flags", strings.NewReader(`{"key":"f"}`))
	req.Header.Set("Authorization", "Bearer k1")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create = %d", rec.Code)
	}
	// A read-only key may read the audit trail.
	req = httptest.NewRequest("GET", "/v1/envs/dev/audit?actor=alice", nil)
	req.Header.Set("X-API-Key", "k2")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"actor":"alice"`) {
		t.Fatalf("audit read: %d %s", rec.Code, rec.Body.String())
	}
	if got := srv.Audit().Query(AuditQuery{Actor: "bob"}); len(got) != 0 {
		t.Fatalf("bob made no changes: %+v", got)
	}
}
