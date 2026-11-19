package flags

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

type received struct {
	sig     string
	payload WebhookPayload
	raw     []byte
}

func receiver(t *testing.T, failFirst int32) (*httptest.Server, chan received) {
	t.Helper()
	ch := make(chan received, 16)
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) <= failFirst {
			w.WriteHeader(500)
			return
		}
		b, _ := io.ReadAll(r.Body)
		var p WebhookPayload
		_ = json.Unmarshal(b, &p)
		ch <- received{r.Header.Get("X-Flags-Signature"), p, b}
	}))
	t.Cleanup(srv.Close)
	return srv, ch
}

func wait(t *testing.T, ch chan received) received {
	t.Helper()
	select {
	case r := <-ch:
		return r
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for webhook")
		return received{}
	}
}

func newTestServer(t *testing.T) *Server {
	s := NewServer(NewEnvironments())
	s.hooks.Close()
	s.hooks = NewWebhooks(WebhookOptions{Backoff: time.Millisecond})
	t.Cleanup(s.hooks.Close)
	return s
}

func TestWebhookDeliveredSigned(t *testing.T) {
	rcv, ch := receiver(t, 0)
	s := newTestServer(t)
	code, out := do(t, s, "POST", "/v1/envs/dev/webhooks", `{"url":"`+rcv.URL+`","secret":"shh"}`)
	if code != 201 || out["id"] == "" || out["secret"] != nil {
		t.Fatalf("register: %d %v", code, out)
	}
	do(t, s, "POST", "/v1/envs/dev/flags", `{"key":"f","enabled":true}`)
	r := wait(t, ch)
	if r.payload.Event != "flag.changed" || r.payload.Entry.Key != "f" || r.payload.Entry.Action != ActionCreate {
		t.Fatalf("payload = %+v", r.payload)
	}
	if r.sig != SignPayload("shh", r.raw) {
		t.Fatalf("bad signature %q", r.sig)
	}
}

func TestWebhookFiltersEnvAndEvents(t *testing.T) {
	rcv, ch := receiver(t, 0)
	s := newTestServer(t)
	do(t, s, "POST", "/v1/envs/dev/webhooks", `{"url":"`+rcv.URL+`","events":["delete"]}`)
	do(t, s, "POST", "/v1/envs/dev/flags", `{"key":"f"}`)  // create: filtered out
	do(t, s, "POST", "/v1/envs/prod/flags", `{"key":"g"}`) // other env
	do(t, s, "DELETE", "/v1/envs/prod/flags/g", "")        // other env delete
	do(t, s, "DELETE", "/v1/envs/dev/flags/f", "")         // match
	r := wait(t, ch)
	if r.payload.Entry.Action != ActionDelete || r.payload.Entry.Env != "dev" {
		t.Fatalf("payload = %+v", r.payload)
	}
	select {
	case x := <-ch:
		t.Fatalf("unexpected extra delivery %+v", x.payload)
	case <-time.After(100 * time.Millisecond):
	}
}

func TestWebhookRetriesOnFailure(t *testing.T) {
	rcv, ch := receiver(t, 2)
	s := newTestServer(t)
	do(t, s, "POST", "/v1/envs/dev/webhooks", `{"url":"`+rcv.URL+`"}`)
	do(t, s, "POST", "/v1/envs/dev/flags", `{"key":"f"}`)
	wait(t, ch) // succeeds on 3rd attempt
}

func TestWebhookAllEnvs(t *testing.T) {
	rcv, ch := receiver(t, 0)
	s := newTestServer(t)
	do(t, s, "POST", "/v1/envs/dev/webhooks", `{"url":"`+rcv.URL+`","all_envs":true}`)
	do(t, s, "POST", "/v1/envs/prod/flags", `{"key":"f"}`)
	if r := wait(t, ch); r.payload.Entry.Env != "prod" {
		t.Fatalf("env = %q", r.payload.Entry.Env)
	}
}

func TestWebhookAPIManagement(t *testing.T) {
	s := newTestServer(t)
	if code, _ := do(t, s, "POST", "/v1/envs/dev/webhooks", `{"url":"ftp://x"}`); code != 400 {
		t.Fatalf("bad url = %d", code)
	}
	if code, _ := do(t, s, "POST", "/v1/envs/dev/webhooks", `{"url":"http://x.test","events":["nope"]}`); code != 400 {
		t.Fatalf("bad event = %d", code)
	}
	_, out := do(t, s, "POST", "/v1/envs/dev/webhooks", `{"url":"http://x.test"}`)
	id := out["id"].(string)
	_, out = do(t, s, "GET", "/v1/envs/dev/webhooks", "")
	if len(out["webhooks"].([]any)) != 1 {
		t.Fatalf("list = %v", out)
	}
	if _, out = do(t, s, "GET", "/v1/envs/prod/webhooks", ""); len(out["webhooks"].([]any)) != 0 {
		t.Fatalf("prod list = %v", out)
	}
	if code, _ := do(t, s, "DELETE", "/v1/envs/dev/webhooks/"+id, ""); code != 204 {
		t.Fatalf("delete = %d", code)
	}
	if code, _ := do(t, s, "DELETE", "/v1/envs/dev/webhooks/"+id, ""); code != 404 {
		t.Fatalf("delete again = %d", code)
	}
}
