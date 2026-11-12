package flags

import (
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"fmt"
	"net/http"
	"strings"
)

// Scope is the level of access an API key grants.
type Scope string

const (
	// ScopeRead allows reads and flag evaluation.
	ScopeRead Scope = "read"
	// ScopeWrite allows everything ScopeRead does plus create/update/delete.
	ScopeWrite Scope = "write"
)

// APIKey is a named credential with a scope.
type APIKey struct {
	Name  string
	Key   string
	Scope Scope
}

// Authenticator validates API keys. The zero value rejects everything.
type Authenticator struct {
	keys []apiKeyEntry
}

type apiKeyEntry struct {
	name  string
	hash  [32]byte
	scope Scope
}

// NewAuthenticator builds an Authenticator from keys. Keys must be non-empty
// and have a valid scope. Only SHA-256 digests are kept in memory.
func NewAuthenticator(keys ...APIKey) (*Authenticator, error) {
	a := &Authenticator{}
	for _, k := range keys {
		if k.Key == "" {
			return nil, errors.New("auth: empty API key")
		}
		if k.Scope != ScopeRead && k.Scope != ScopeWrite {
			return nil, fmt.Errorf("auth: invalid scope %q for key %q", k.Scope, k.Name)
		}
		a.keys = append(a.keys, apiKeyEntry{k.Name, sha256.Sum256([]byte(k.Key)), k.Scope})
	}
	return a, nil
}

// ParseAPIKeys parses a comma-separated list of "name:scope:key" entries,
// e.g. "ci:read:abc123,admin:write:s3cret". The key may itself contain colons.
func ParseAPIKeys(spec string) ([]APIKey, error) {
	var out []APIKey
	for _, part := range strings.Split(spec, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		f := strings.SplitN(part, ":", 3)
		if len(f) != 3 {
			return nil, fmt.Errorf("auth: entry %q is not name:scope:key", part)
		}
		out = append(out, APIKey{Name: f[0], Scope: Scope(f[1]), Key: f[2]})
	}
	return out, nil
}

// Lookup returns the name and scope for token, comparing digests in constant
// time across all keys.
func (a *Authenticator) Lookup(token string) (name string, scope Scope, ok bool) {
	if a == nil || token == "" {
		return "", "", false
	}
	sum := sha256.Sum256([]byte(token))
	for _, e := range a.keys {
		if subtle.ConstantTimeCompare(sum[:], e.hash[:]) == 1 {
			name, scope, ok = e.name, e.scope, true
		}
	}
	return name, scope, ok
}

// tokenFromRequest extracts a key from "Authorization: Bearer <key>" or
// the "X-API-Key" header.
func tokenFromRequest(r *http.Request) string {
	if h := r.Header.Get("Authorization"); h != "" {
		if len(h) > 7 && strings.EqualFold(h[:7], "bearer ") {
			return strings.TrimSpace(h[7:])
		}
		return ""
	}
	return strings.TrimSpace(r.Header.Get("X-API-Key"))
}

// requiredScope: reads and evaluation need read; everything else needs write.
func requiredScope(r *http.Request) Scope {
	switch r.Method {
	case http.MethodGet, http.MethodHead:
		return ScopeRead
	case http.MethodPost:
		if strings.HasSuffix(r.URL.Path, "/evaluate") {
			return ScopeRead
		}
	}
	return ScopeWrite
}

// Middleware wraps next, answering 401 for a missing/unknown key and 403 when
// the key's scope is too weak for the request.
func (a *Authenticator) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, scope, ok := a.Lookup(tokenFromRequest(r))
		if !ok {
			w.Header().Set("WWW-Authenticate", `Bearer realm="feature-flag-service"`)
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "missing or invalid API key"})
			return
		}
		if requiredScope(r) == ScopeWrite && scope != ScopeWrite {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "API key is read-only"})
			return
		}
		next.ServeHTTP(w, r)
	})
}
