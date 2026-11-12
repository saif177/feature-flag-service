package flags

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func authedServer(t *testing.T) http.Handler {
	t.Helper()
	a, err := NewAuthenticator(
		APIKey{Name: "ro", Key: "read-key", Scope: ScopeRead},
		APIKey{Name: "rw", Key: "write-key", Scope: ScopeWrite},
	)
	if err != nil {
		t.Fatal(err)
	}
	return a.Middleware(NewServer(NewEnvironments()))
}

func doAuth(h http.Handler, method, path, body string, hdr map[string]string) int {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec.Code
}

func TestAuthMissingAndInvalid(t *testing.T) {
	h := authedServer(t)
	for name, hdr := range map[string]map[string]string{
		"none":       nil,
		"wrong":      {"Authorization": "Bearer nope"},
		"non-bearer": {"Authorization": "Basic write-key"},
		"empty":      {"X-API-Key": ""},
	} {
		if c := doAuth(h, "GET", "/v1/envs/dev/flags", "", hdr); c != 401 {
			t.Errorf("%s: got %d, want 401", name, c)
		}
	}
}

func TestAuthScopes(t *testing.T) {
	h := authedServer(t)
	ro := map[string]string{"Authorization": "Bearer read-key"}
	rw := map[string]string{"X-API-Key": "write-key"}
	base := "/v1/envs/dev/flags"

	if c := doAuth(h, "POST", base, `{"key":"f","enabled":true}`, ro); c != 403 {
		t.Errorf("read key create = %d, want 403", c)
	}
	if c := doAuth(h, "POST", base, `{"key":"f","enabled":true}`, rw); c != 201 {
		t.Errorf("write key create = %d, want 201", c)
	}
	if c := doAuth(h, "GET", base+"/f", "", ro); c != 200 {
		t.Errorf("read key get = %d, want 200", c)
	}
	if c := doAuth(h, "POST", base+"/f/evaluate", `{"user_id":"u"}`, ro); c != 200 {
		t.Errorf("read key evaluate = %d, want 200", c)
	}
	if c := doAuth(h, "PATCH", base+"/f", `{"enabled":false}`, ro); c != 403 {
		t.Errorf("read key patch = %d, want 403", c)
	}
	if c := doAuth(h, "DELETE", base+"/f", "", ro); c != 403 {
		t.Errorf("read key delete = %d, want 403", c)
	}
	if c := doAuth(h, "DELETE", base+"/f", "", rw); c != 204 {
		t.Errorf("write key delete = %d, want 204", c)
	}
}

func TestAuthenticatorValidation(t *testing.T) {
	if _, err := NewAuthenticator(APIKey{Key: "", Scope: ScopeRead}); err == nil {
		t.Error("empty key accepted")
	}
	if _, err := NewAuthenticator(APIKey{Key: "k", Scope: "admin"}); err == nil {
		t.Error("bad scope accepted")
	}
	var zero *Authenticator
	if _, _, ok := zero.Lookup("x"); ok {
		t.Error("nil authenticator accepted a key")
	}
}

func TestParseAPIKeys(t *testing.T) {
	keys, err := ParseAPIKeys("ci:read:abc, admin:write:s:3cret ,")
	if err != nil || len(keys) != 2 {
		t.Fatalf("got %v, %v", keys, err)
	}
	if keys[1] != (APIKey{Name: "admin", Scope: ScopeWrite, Key: "s:3cret"}) {
		t.Errorf("keys[1] = %+v", keys[1])
	}
	if _, err := ParseAPIKeys("bad"); err == nil {
		t.Error("malformed entry accepted")
	}
}
