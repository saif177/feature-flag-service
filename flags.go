package flags

import (
	"errors"
	"sync"
)

// ErrNotFound is returned when a flag key doesn't exist in the store.
var ErrNotFound = errors.New("flag not found")

// Flag is a simple on/off feature flag.
type Flag struct {
	Key     string
	Enabled bool
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
	s.flags[key] = Flag{Key: key, Enabled: enabled}
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

// IsEnabled reports whether a flag is enabled. Unknown flags are treated as disabled.
func (s *Store) IsEnabled(key string) bool {
	f, err := s.Get(key)
	if err != nil {
		return false
	}
	return f.Enabled
}
