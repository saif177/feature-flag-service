package flags

import (
	"errors"
	"sort"
	"sync"
)

// Default environment names created by NewEnvironments when none are given.
const (
	EnvDev     = "dev"
	EnvStaging = "staging"
	EnvProd    = "prod"
)

// ErrUnknownEnvironment is returned when an environment name isn't registered.
var ErrUnknownEnvironment = errors.New("unknown environment")

// Environments holds one independent Store per named environment, so a flag
// can be on in dev while still off in prod.
type Environments struct {
	mu     sync.RWMutex
	stores map[string]*Store
}

// NewEnvironments creates the given environments, each with an empty Store.
// With no names it creates dev, staging and prod.
func NewEnvironments(names ...string) *Environments {
	if len(names) == 0 {
		names = []string{EnvDev, EnvStaging, EnvProd}
	}
	e := &Environments{stores: make(map[string]*Store, len(names))}
	for _, n := range names {
		e.stores[n] = NewStore()
	}
	return e
}

// Env returns the Store for an environment, or ErrUnknownEnvironment.
func (e *Environments) Env(name string) (*Store, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	s, ok := e.stores[name]
	if !ok {
		return nil, ErrUnknownEnvironment
	}
	return s, nil
}

// Names returns the registered environment names in sorted order.
func (e *Environments) Names() []string {
	e.mu.RLock()
	defer e.mu.RUnlock()

	out := make([]string, 0, len(e.stores))
	for n := range e.stores {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// Promote copies a flag's full configuration (enabled state, rollout, rules)
// from one environment to another, creating or overwriting it in the target.
func (e *Environments) Promote(key, from, to string) error {
	src, err := e.Env(from)
	if err != nil {
		return err
	}
	dst, err := e.Env(to)
	if err != nil {
		return err
	}
	f, err := src.Get(key)
	if err != nil {
		return err
	}
	dst.put(f)
	return nil
}

// put stores a copy of f, replacing any existing flag with the same key.
func (s *Store) put(f Flag) {
	f.Rules = cloneRules(f.Rules)

	s.mu.Lock()
	defer s.mu.Unlock()
	s.flags[f.Key] = f
}

func cloneRules(rules []Rule) []Rule {
	if rules == nil {
		return nil
	}
	out := make([]Rule, len(rules))
	for i, r := range rules {
		r.Values = append([]string(nil), r.Values...)
		out[i] = r
	}
	return out
}
