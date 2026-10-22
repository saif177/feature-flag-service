package flags

import (
	"errors"
	"hash/fnv"
	"sync"
)

// ErrNotFound is returned when a flag key doesn't exist in the store.
var ErrNotFound = errors.New("flag not found")

// ErrInvalidPercentage is returned when a rollout percentage is outside 0-100.
var ErrInvalidPercentage = errors.New("rollout percentage must be between 0 and 100")

// Flag is a feature flag. When Enabled is true, Rollout (0-100) controls the
// percentage of users who see it as on; new flags default to 100.
type Flag struct {
	Key     string
	Enabled bool
	Rollout int
}

// Store is a thread-safe in-memory flag store.
type Store struct {
	mu    sync.RWMutex
	flags map[string]Flag
}

func NewStore() *Store {
	return &Store{flags: make(map[string]Flag)}
}

// Create adds a new flag. Returns an error if the key already exists.
func (s *Store) Create(key string, enabled bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.flags[key]; exists {
		return errors.New("flag already exists")
	}
	s.flags[key] = Flag{Key: key, Enabled: enabled, Rollout: 100}
	return nil
}

// Get returns the flag for key, or ErrNotFound.
func (s *Store) Get(key string) (Flag, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	f, ok := s.flags[key]
	if !ok {
		return Flag{}, ErrNotFound
	}
	return f, nil
}

// Set updates whether a flag is enabled. Returns ErrNotFound if it doesn't exist.
func (s *Store) Set(key string, enabled bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	f, ok := s.flags[key]
	if !ok {
		return ErrNotFound
	}
	f.Enabled = enabled
	s.flags[key] = f
	return nil
}

// Delete removes a flag. Returns ErrNotFound if it doesn't exist.
func (s *Store) Delete(key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.flags[key]; !ok {
		return ErrNotFound
	}
	delete(s.flags, key)
	return nil
}

// List returns all flags currently in the store.
func (s *Store) List() []Flag {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]Flag, 0, len(s.flags))
	for _, f := range s.flags {
		out = append(out, f)
	}
	return out
}

// IsEnabled reports whether a flag is enabled for everyone. Unknown flags and
// flags with a partial rollout are treated as disabled; use IsEnabledFor to
// evaluate a partial rollout for a specific user.
func (s *Store) IsEnabled(key string) bool {
	f, err := s.Get(key)
	if err != nil {
		return false
	}
	return f.Enabled && f.Rollout >= 100
}

// SetRollout sets the percentage (0-100) of users for whom an enabled flag is on.
func (s *Store) SetRollout(key string, percent int) error {
	if percent < 0 || percent > 100 {
		return ErrInvalidPercentage
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	f, ok := s.flags[key]
	if !ok {
		return ErrNotFound
	}
	f.Rollout = percent
	s.flags[key] = f
	return nil
}

// IsEnabledFor evaluates a flag for a specific user. The result is
// deterministic: the same flag and user ID always yield the same answer, and
// raising the rollout percentage never turns the flag off for a user who
// already had it. Unknown or disabled flags are off.
func (s *Store) IsEnabledFor(key, userID string) bool {
	f, err := s.Get(key)
	if err != nil || !f.Enabled {
		return false
	}
	return inRollout(f.Key, userID, f.Rollout)
}

// inRollout buckets (key, userID) into 0-99 and checks it against percent.
func inRollout(key, userID string, percent int) bool {
	if percent >= 100 {
		return true
	}
	if percent <= 0 {
		return false
	}
	h := fnv.New32a()
	h.Write([]byte(key))
	h.Write([]byte{0})
	h.Write([]byte(userID))
	return int(h.Sum32()%100) < percent
}
