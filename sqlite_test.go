package flags

import (
	"errors"
	"path/filepath"
	"reflect"
	"testing"
)

func newSQLite(t *testing.T) *SQLiteStore {
	t.Helper()
	s, err := OpenSQLite(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestSQLiteCRUD(t *testing.T) {
	s := newSQLite(t)
	if err := s.Create("a", true); err != nil {
		t.Fatal(err)
	}
	if err := s.Create("a", false); !errors.Is(err, ErrAlreadyExists) {
		t.Fatalf("want ErrAlreadyExists, got %v", err)
	}
	f, err := s.Get("a")
	if err != nil || !f.Enabled || f.Rollout != 100 || f.Rules != nil {
		t.Fatalf("unexpected flag %+v, %v", f, err)
	}
	if !s.IsEnabled("a") {
		t.Fatal("a should be enabled")
	}
	if err := s.Set("a", false); err != nil {
		t.Fatal(err)
	}
	if s.IsEnabled("a") {
		t.Fatal("a should be disabled")
	}
	_ = s.Create("b", true)
	if got := s.List(); len(got) != 2 || got[0].Key != "a" || got[1].Key != "b" {
		t.Fatalf("List = %+v", got)
	}
	if err := s.Delete("a"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Get("a"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

func TestSQLiteNotFound(t *testing.T) {
	s := newSQLite(t)
	for name, err := range map[string]error{
		"Set":        s.Set("x", true),
		"Delete":     s.Delete("x"),
		"SetRollout": s.SetRollout("x", 5),
		"AddRule":    s.AddRule("x", Rule{Attribute: "a", Operator: OpEquals, Values: []string{"b"}}),
		"ClearRules": s.ClearRules("x"),
	} {
		if !errors.Is(err, ErrNotFound) {
			t.Errorf("%s: want ErrNotFound, got %v", name, err)
		}
	}
	if s.IsEnabled("x") || s.Evaluate("x", Context{}) {
		t.Error("unknown flag must be off")
	}
}

func TestSQLiteRolloutValidation(t *testing.T) {
	s := newSQLite(t)
	_ = s.Create("a", true)
	if err := s.SetRollout("a", 101); !errors.Is(err, ErrInvalidPercentage) {
		t.Fatalf("got %v", err)
	}
	if err := s.SetRollout("a", 40); err != nil {
		t.Fatal(err)
	}
	if s.IsEnabled("a") {
		t.Fatal("partial rollout is not IsEnabled")
	}
}

func TestSQLiteRulesAndEvaluate(t *testing.T) {
	s := newSQLite(t)
	_ = s.Create("a", true)
	_ = s.SetRollout("a", 0)
	if err := s.AddRule("a", Rule{Attribute: "bad"}); !errors.Is(err, ErrInvalidRule) {
		t.Fatalf("got %v", err)
	}
	rule := Rule{Attribute: "email", Operator: OpEndsWith, Values: []string{"@acme.com"}}
	if err := s.AddRule("a", rule); err != nil {
		t.Fatal(err)
	}
	f, _ := s.Get("a")
	if !reflect.DeepEqual(f.Rules, []Rule{rule}) {
		t.Fatalf("rules = %+v", f.Rules)
	}
	in := Context{UserID: "u", Attributes: map[string]string{"email": "x@acme.com"}}
	out := Context{UserID: "u", Attributes: map[string]string{"email": "x@other.com"}}
	if !s.Evaluate("a", in) || s.Evaluate("a", out) {
		t.Fatal("rule targeting wrong")
	}
	_ = s.Set("a", false)
	if s.Evaluate("a", in) {
		t.Fatal("disabled flag must be off")
	}
	_ = s.Set("a", true)
	_ = s.ClearRules("a")
	if s.Evaluate("a", in) {
		t.Fatal("rules should be cleared")
	}
}

func TestSQLiteMatchesMemoryStore(t *testing.T) {
	mem, db := NewStore(), newSQLite(t)
	for _, st := range []FlagStore{mem, db} {
		_ = st.Create("f", true)
		_ = st.SetRollout("f", 30)
	}
	for i := 0; i < 200; i++ {
		u := string(rune('a'+i%26)) + string(rune('0'+i%10)) + string(rune('A'+i/26))
		if mem.IsEnabledFor("f", u) != db.IsEnabledFor("f", u) {
			t.Fatalf("backends disagree for %q", u)
		}
	}
}

func TestSQLitePersistsAcrossReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "flags.db")
	s, err := OpenSQLite(path)
	if err != nil {
		t.Fatal(err)
	}
	_ = s.Create("keep", true)
	_ = s.SetRollout("keep", 55)
	rule := Rule{Attribute: AttrUserID, Operator: OpEquals, Values: []string{"u1", "u2"}}
	_ = s.AddRule("keep", rule)
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	s2, err := OpenSQLite(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()
	f, err := s2.Get("keep")
	if err != nil {
		t.Fatal(err)
	}
	want := Flag{Key: "keep", Enabled: true, Rollout: 55, Rules: []Rule{rule}}
	if !reflect.DeepEqual(f, want) {
		t.Fatalf("got %+v want %+v", f, want)
	}
}
