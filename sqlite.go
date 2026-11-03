package flags

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	_ "modernc.org/sqlite" // pure-Go SQLite driver
)

// ErrAlreadyExists is returned when creating a flag whose key is taken.
var ErrAlreadyExists = errors.New("flag already exists")

// FlagStore is the behaviour shared by the in-memory Store and SQLiteStore.
type FlagStore interface {
	Create(key string, enabled bool) error
	Get(key string) (Flag, error)
	Set(key string, enabled bool) error
	Delete(key string) error
	List() []Flag
	IsEnabled(key string) bool
	SetRollout(key string, percent int) error
	AddRule(key string, r Rule) error
	ClearRules(key string) error
	Evaluate(key string, ctx Context) bool
	IsEnabledFor(key, userID string) bool
}

var (
	_ FlagStore = (*Store)(nil)
	_ FlagStore = (*SQLiteStore)(nil)
)

const schema = `
CREATE TABLE IF NOT EXISTS flags (
	key     TEXT PRIMARY KEY,
	enabled INTEGER NOT NULL,
	rollout INTEGER NOT NULL DEFAULT 100,
	rules   TEXT NOT NULL DEFAULT '[]'
)`

// SQLiteStore is a flag store persisted in a SQLite database.
type SQLiteStore struct {
	db *sql.DB
}

// OpenSQLite opens (creating if needed) the SQLite database at path and
// ensures the schema exists. Use ":memory:" for a throwaway database.
func OpenSQLite(path string) (*SQLiteStore, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	// SQLite allows a single writer; one connection also keeps ":memory:"
	// databases coherent and serialises read-modify-write updates.
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("init schema: %w", err)
	}
	return &SQLiteStore{db: db}, nil
}

// Close releases the underlying database.
func (s *SQLiteStore) Close() error { return s.db.Close() }

// Create adds a new flag with a 100% rollout.
func (s *SQLiteStore) Create(key string, enabled bool) error {
	res, err := s.db.Exec(`INSERT INTO flags (key, enabled, rollout, rules) VALUES (?, ?, 100, '[]')
		ON CONFLICT(key) DO NOTHING`, key, enabled)
	if err != nil {
		return err
	}
	if n, err := res.RowsAffected(); err != nil {
		return err
	} else if n == 0 {
		return ErrAlreadyExists
	}
	return nil
}

func scanFlag(sc interface{ Scan(...any) error }) (Flag, error) {
	var (
		f     Flag
		rules string
	)
	if err := sc.Scan(&f.Key, &f.Enabled, &f.Rollout, &rules); err != nil {
		return Flag{}, err
	}
	if err := json.Unmarshal([]byte(rules), &f.Rules); err != nil {
		return Flag{}, fmt.Errorf("decode rules for %q: %w", f.Key, err)
	}
	if len(f.Rules) == 0 {
		f.Rules = nil
	}
	return f, nil
}

// Get returns the flag for key, or ErrNotFound.
func (s *SQLiteStore) Get(key string) (Flag, error) {
	f, err := scanFlag(s.db.QueryRow(`SELECT key, enabled, rollout, rules FROM flags WHERE key = ?`, key))
	if errors.Is(err, sql.ErrNoRows) {
		return Flag{}, ErrNotFound
	}
	return f, err
}

func (s *SQLiteStore) exec(query string, args ...any) error {
	res, err := s.db.Exec(query, args...)
	if err != nil {
		return err
	}
	if n, err := res.RowsAffected(); err != nil {
		return err
	} else if n == 0 {
		return ErrNotFound
	}
	return nil
}

// Set updates whether a flag is enabled.
func (s *SQLiteStore) Set(key string, enabled bool) error {
	return s.exec(`UPDATE flags SET enabled = ? WHERE key = ?`, enabled, key)
}

// Delete removes a flag.
func (s *SQLiteStore) Delete(key string) error {
	return s.exec(`DELETE FROM flags WHERE key = ?`, key)
}

// List returns all flags ordered by key. On a database error it returns nil.
func (s *SQLiteStore) List() []Flag {
	rows, err := s.db.Query(`SELECT key, enabled, rollout, rules FROM flags ORDER BY key`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	out := []Flag{}
	for rows.Next() {
		f, err := scanFlag(rows)
		if err != nil {
			return nil
		}
		out = append(out, f)
	}
	return out
}

// IsEnabled reports whether a flag is fully on for everyone.
func (s *SQLiteStore) IsEnabled(key string) bool {
	f, err := s.Get(key)
	return err == nil && f.Enabled && f.Rollout >= 100
}

// SetRollout sets the rollout percentage (0-100).
func (s *SQLiteStore) SetRollout(key string, percent int) error {
	if percent < 0 || percent > 100 {
		return ErrInvalidPercentage
	}
	return s.exec(`UPDATE flags SET rollout = ? WHERE key = ?`, percent, key)
}

// AddRule appends a targeting rule to a flag.
func (s *SQLiteStore) AddRule(key string, r Rule) error {
	if err := r.validate(); err != nil {
		return err
	}
	f, err := s.Get(key)
	if err != nil {
		return err
	}
	rules := append(cloneRules(f.Rules), Rule{Attribute: r.Attribute, Operator: r.Operator, Values: append([]string(nil), r.Values...)})
	return s.saveRules(key, rules)
}

// ClearRules removes all targeting rules from a flag.
func (s *SQLiteStore) ClearRules(key string) error {
	return s.saveRules(key, nil)
}

func (s *SQLiteStore) saveRules(key string, rules []Rule) error {
	if rules == nil {
		rules = []Rule{}
	}
	b, err := json.Marshal(rules)
	if err != nil {
		return err
	}
	return s.exec(`UPDATE flags SET rules = ? WHERE key = ?`, string(b), key)
}

// Evaluate decides whether a flag is on for ctx; see Store.Evaluate.
func (s *SQLiteStore) Evaluate(key string, ctx Context) bool {
	f, err := s.Get(key)
	if err != nil {
		return false
	}
	return evaluateFlag(f, ctx)
}

// IsEnabledFor evaluates a flag for a specific user ID.
func (s *SQLiteStore) IsEnabledFor(key, userID string) bool {
	return s.Evaluate(key, Context{UserID: userID})
}
