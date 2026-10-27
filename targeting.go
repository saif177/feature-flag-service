package flags

import (
	"errors"
	"strings"
)

// ErrInvalidRule is returned when a targeting rule is malformed.
var ErrInvalidRule = errors.New("invalid targeting rule")

// AttrUserID is the built-in attribute name that resolves to Context.UserID.
const AttrUserID = "user_id"

// Operator is how a Rule compares an attribute against its values.
type Operator string

const (
	OpEquals     Operator = "equals"      // attribute equals any of Values
	OpEndsWith   Operator = "ends_with"   // attribute ends with any of Values (e.g. "@acme.com")
	OpStartsWith Operator = "starts_with" // attribute starts with any of Values
	OpContains   Operator = "contains"    // attribute contains any of Values
)

// Rule targets users by attribute. A rule matches when the named attribute is
// present and satisfies Operator against at least one of Values.
type Rule struct {
	Attribute string
	Operator  Operator
	Values    []string
}

// Context describes who a flag is being evaluated for.
type Context struct {
	UserID     string
	Attributes map[string]string
}

func (r Rule) validate() error {
	if r.Attribute == "" || len(r.Values) == 0 {
		return ErrInvalidRule
	}
	switch r.Operator {
	case OpEquals, OpEndsWith, OpStartsWith, OpContains:
		return nil
	}
	return ErrInvalidRule
}

func (r Rule) matches(ctx Context) bool {
	var got string
	if r.Attribute == AttrUserID {
		got = ctx.UserID
	} else {
		v, ok := ctx.Attributes[r.Attribute]
		if !ok {
			return false
		}
		got = v
	}
	for _, want := range r.Values {
		switch r.Operator {
		case OpEquals:
			if got == want {
				return true
			}
		case OpEndsWith:
			if strings.HasSuffix(got, want) {
				return true
			}
		case OpStartsWith:
			if strings.HasPrefix(got, want) {
				return true
			}
		case OpContains:
			if strings.Contains(got, want) {
				return true
			}
		}
	}
	return false
}

// AddRule appends a targeting rule to a flag. Rules are only consulted while
// the flag is enabled; a user matching any rule gets the flag on regardless of
// the rollout percentage.
func (s *Store) AddRule(key string, r Rule) error {
	if err := r.validate(); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	f, ok := s.flags[key]
	if !ok {
		return ErrNotFound
	}
	// Copy so callers holding an earlier Flag value never see the slice change.
	rules := make([]Rule, len(f.Rules), len(f.Rules)+1)
	copy(rules, f.Rules)
	r.Values = append([]string(nil), r.Values...)
	f.Rules = append(rules, r)
	s.flags[key] = f
	return nil
}

// ClearRules removes all targeting rules from a flag.
func (s *Store) ClearRules(key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	f, ok := s.flags[key]
	if !ok {
		return ErrNotFound
	}
	f.Rules = nil
	s.flags[key] = f
	return nil
}

// Evaluate decides whether a flag is on for ctx. Unknown and disabled flags are
// off. Otherwise a matching targeting rule turns the flag on; if none match,
// the percentage rollout is applied to ctx.UserID.
func (s *Store) Evaluate(key string, ctx Context) bool {
	f, err := s.Get(key)
	if err != nil || !f.Enabled {
		return false
	}
	for _, r := range f.Rules {
		if r.matches(ctx) {
			return true
		}
	}
	return inRollout(f.Key, ctx.UserID, f.Rollout)
}
