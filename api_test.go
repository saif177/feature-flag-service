package flags

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func do(t *testing.T, h http.Handler, method, path, body string) (int, map[string]any) {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	var out map[string]any
	if rec.Body.Len() > 0 {
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatalf("%s %s: bad JSON %q: %v", method, path, rec.Body.String(), err)
		}
	}
	return rec.Code, out
}

func TestAPICRUD(t *testing.T) {
	h := NewServer(NewEnvironments())
	base := "/v1/envs/dev/flags"

	code, out := do(t, h, "POST", base, `{"key":"checkout","enabled":true,"rollout":50}`)
	if code != 201 || out["rollout"] != float64(50) || out["enabled"] != true {
		t.Fatalf("create: %d %v", code, out)
	}
	if code, _ = do(t, h, "POST", base, `{"key":"checkout"}`); code != 409 {
		t.Fatalf("duplicate create = %d, want 409", code)
	}
	if code, out = do(t, h, "GET", base+"/checkout", ""); code != 200 || out["key"] != "checkout" {
		t.Fatalf("get: %d %v", code, out)
	}
	code, out = do(t, h, "PATCH", base+"/checkout", `{"enabled":false,"rules":[{"attribute":"email","operator":"ends_with","values":["@acme.com"]}]}`)
	if code != 200 || out["enabled"] != false || len(out["rules"].([]any)) != 1 || out["rollout"] != float64(50) {
		t.Fatalf("patch: %d %v", code, out)
	}
	_, out = do(t, h, "GET", base, "")
	if len(out["flags"].([]any)) != 1 {
		t.Fatalf("list: %v", out)
	}
	if code, _ = do(t, h, "DELETE", base+"/checkout", ""); code != 204 {
		t.Fatalf("delete = %d", code)
	}
	if code, _ = do(t, h, "GET", base+"/checkout", ""); code != 404 {
		t.Fatalf("get after delete = %d", code)
	}
}

func TestAPIEvaluate(t *testing.T) {
	h := NewServer(NewEnvironments())
	do(t, h, "POST", "/v1/envs/prod/flags", `{"key":"f","enabled":true,"rollout":0,"rules":[{"attribute":"email","operator":"ends_with","values":["@acme.com"]}]}`)
	ev := "/v1/envs/prod/flags/f/evaluate"

	_, out := do(t, h, "POST", ev, `{"user_id":"u1","attributes":{"email":"a@acme.com"}}`)
	if out["enabled"] != true {
		t.Fatalf("rule match should enable: %v", out)
	}
	_, out = do(t, h, "POST", ev, `{"user_id":"u2","attributes":{"email":"a@other.com"}}`)
	if out["enabled"] != false {
		t.Fatalf("0%% rollout, no match should be off: %v", out)
	}
	if code, _ := do(t, h, "POST", "/v1/envs/prod/flags/missing/evaluate", `{}`); code != 404 {
		t.Fatalf("evaluate unknown flag = %d", code)
	}
}

func TestAPIEnvironmentsIndependent(t *testing.T) {
	h := NewServer(NewEnvironments())
	do(t, h, "POST", "/v1/envs/dev/flags", `{"key":"f","enabled":true}`)
	if code, _ := do(t, h, "GET", "/v1/envs/prod/flags/f", ""); code != 404 {
		t.Fatalf("flag leaked across envs: %d", code)
	}
	if code, _ := do(t, h, "GET", "/v1/envs/nope/flags", ""); code != 404 {
		t.Fatalf("unknown env = %d", code)
	}
}

func TestAPIValidation(t *testing.T) {
	h := NewServer(NewEnvironments())
	base := "/v1/envs/dev/flags"
	cases := []struct{ name, method, path, body string }{
		{"missing key", "POST", base, `{"enabled":true}`},
		{"bad json", "POST", base, `{`},
		{"unknown field", "POST", base, `{"key":"x","bogus":1}`},
		{"rollout too high", "POST", base, `{"key":"x","rollout":101}`},
		{"bad rule", "POST", base, `{"key":"x","rules":[{"attribute":"a","operator":"nope","values":["v"]}]}`},
	}
	for _, c := range cases {
		if code, out := do(t, h, c.method, c.path, c.body); code != 400 || out["error"] == nil {
			t.Errorf("%s: got %d %v, want 400 with error", c.name, code, out)
		}
	}
	if code, _ := do(t, h, "GET", base+"/x", ""); code != 404 {
		t.Errorf("invalid create left a flag behind: %d", code)
	}

	do(t, h, "POST", base, `{"key":"y","enabled":true}`)
	if code, _ := do(t, h, "PATCH", base+"/y", `{"enabled":false,"rollout":200}`); code != 400 {
		t.Errorf("patch bad rollout = %d", code)
	}
	if _, out := do(t, h, "GET", base+"/y", ""); out["enabled"] != true {
		t.Errorf("failed patch partially applied: %v", out)
	}
}

func TestAPIWithSQLiteBackend(t *testing.T) {
	db := newSQLite(t)
	h := NewServerFunc(func(string) (FlagStore, error) { return db, nil })
	if code, _ := do(t, h, "POST", "/v1/envs/x/flags", `{"key":"f","enabled":true,"rollout":10}`); code != 201 {
		t.Fatalf("create on sqlite = %d", code)
	}
	if code, _ := do(t, h, "POST", "/v1/envs/x/flags", `{"key":"f"}`); code != 409 {
		t.Fatalf("duplicate on sqlite = %d, want 409", code)
	}
}
