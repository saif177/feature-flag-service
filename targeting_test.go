package flags

import (
	"errors"
	"testing"
)

func TestRuleEmailDomain(t *testing.T) {
	s := NewStore()
	_ = s.Create("beta", true)
	_ = s.SetRollout("beta", 0)
	if err := s.AddRule("beta", Rule{Attribute: "email", Operator: OpEndsWith, Values: []string{"@acme.com"}}); err != nil {
		t.Fatal(err)
	}

	if !s.Evaluate("beta", Context{UserID: "u1", Attributes: map[string]string{"email": "a@acme.com"}}) {
		t.Fatal("acme user should match rule")
	}
	if s.Evaluate("beta", Context{UserID: "u2", Attributes: map[string]string{"email": "a@other.com"}}) {
		t.Fatal("non-acme user should fall through to 0% rollout")
	}
	if s.Evaluate("beta", Context{UserID: "u3"}) {
		t.Fatal("missing attribute must not match")
	}
}

func TestRuleUserIDAllowList(t *testing.T) {
	s := NewStore()
	_ = s.Create("beta", true)
	_ = s.SetRollout("beta", 0)
	_ = s.AddRule("beta", Rule{Attribute: AttrUserID, Operator: OpEquals, Values: []string{"alice", "bob"}})

	if !s.IsEnabledFor("beta", "alice") || !s.IsEnabledFor("beta", "bob") {
		t.Fatal("allow-listed users should be on")
	}
	if s.IsEnabledFor("beta", "carol") {
		t.Fatal("carol should be off")
	}
}

func TestRuleOperators(t *testing.T) {
	ctx := Context{UserID: "u", Attributes: map[string]string{"plan": "enterprise-eu"}}
	tests := []struct {
		op   Operator
		val  string
		want bool
	}{
		{OpEquals, "enterprise-eu", true},
		{OpEquals, "enterprise", false},
		{OpStartsWith, "enterprise", true},
		{OpStartsWith, "eu", false},
		{OpContains, "prise-", true},
		{OpContains, "free", false},
		{OpEndsWith, "-eu", true},
		{OpEndsWith, "-us", false},
	}
	for _, tc := range tests {
		r := Rule{Attribute: "plan", Operator: tc.op, Values: []string{tc.val}}
		if got := r.matches(ctx); got != tc.want {
			t.Errorf("%s %q: got %v want %v", tc.op, tc.val, got, tc.want)
		}
	}
}

func TestRulesIgnoredWhenFlagDisabled(t *testing.T) {
	s := NewStore()
	_ = s.Create("off", false)
	_ = s.AddRule("off", Rule{Attribute: AttrUserID, Operator: OpEquals, Values: []string{"alice"}})
	if s.IsEnabledFor("off", "alice") {
		t.Fatal("disabled flag must stay off even for targeted users")
	}
}

func TestAddRuleErrors(t *testing.T) {
	s := NewStore()
	_ = s.Create("f", true)

	bad := []Rule{
		{Operator: OpEquals, Values: []string{"x"}},
		{Attribute: "a", Operator: OpEquals},
		{Attribute: "a", Operator: "regex", Values: []string{"x"}},
	}
	for _, r := range bad {
		if err := s.AddRule("f", r); !errors.Is(err, ErrInvalidRule) {
			t.Errorf("rule %+v: got %v, want ErrInvalidRule", r, err)
		}
	}
	good := Rule{Attribute: "a", Operator: OpEquals, Values: []string{"x"}}
	if err := s.AddRule("missing", good); !errors.Is(err, ErrNotFound) {
		t.Errorf("got %v, want ErrNotFound", err)
	}
}

func TestClearRulesAndFallthroughToRollout(t *testing.T) {
	s := NewStore()
	_ = s.Create("f", true)
	_ = s.SetRollout("f", 0)
	_ = s.AddRule("f", Rule{Attribute: AttrUserID, Operator: OpEquals, Values: []string{"alice"}})
	if !s.IsEnabledFor("f", "alice") {
		t.Fatal("alice should match")
	}
	if err := s.ClearRules("f"); err != nil {
		t.Fatal(err)
	}
	if s.IsEnabledFor("f", "alice") {
		t.Fatal("after clearing rules alice falls back to 0% rollout")
	}
	if err := s.ClearRules("nope"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("got %v, want ErrNotFound", err)
	}
}

func TestAddRuleDoesNotAliasCallerSlice(t *testing.T) {
	s := NewStore()
	_ = s.Create("f", true)
	_ = s.SetRollout("f", 0)
	vals := []string{"alice"}
	_ = s.AddRule("f", Rule{Attribute: AttrUserID, Operator: OpEquals, Values: vals})
	vals[0] = "mallory"
	if s.IsEnabledFor("f", "mallory") || !s.IsEnabledFor("f", "alice") {
		t.Fatal("stored rule must not change when caller mutates its slice")
	}
}
