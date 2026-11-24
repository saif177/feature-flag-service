// Command server runs the feature-flag HTTP API on -addr (default :8080).
//
// Authentication is enabled by setting FLAGS_API_KEYS to a comma-separated
// list of name:scope:key entries (scope is "read" or "write"), e.g.
//
//	FLAGS_API_KEYS="ci:read:abc123,admin:write:s3cret"
//
// Clients send the key as "Authorization: Bearer <key>" or "X-API-Key: <key>".
// If unset, the API is open (suitable for local development only).
package main

import (
	"flag"
	"log"
	"net/http"
	"os"
	"sync"
	"time"

	flags "github.com/saif177/feature-flag-service"
)

func main() {
	addr := flag.String("addr", ":8080", "listen address")
	cacheTTL := flag.Duration("cache-ttl", 30*time.Second, "TTL for the evaluation cache; 0 caches until a write invalidates, negative disables")
	flag.Parse()

	envs := flags.NewEnvironments()
	var srv *flags.Server
	if *cacheTTL < 0 {
		srv = flags.NewServer(envs)
	} else {
		var mu sync.Mutex
		cached := map[string]flags.FlagStore{}
		srv = flags.NewServerFunc(func(name string) (flags.FlagStore, error) {
			mu.Lock()
			defer mu.Unlock()
			if c, ok := cached[name]; ok {
				return c, nil
			}
			st, err := envs.Env(name)
			if err != nil {
				return nil, err
			}
			c := flags.NewCachedStore(st, *cacheTTL)
			cached[name] = c
			return c, nil
		})
	}
	defer srv.Webhooks().Close()
	var handler http.Handler = srv
	if spec := os.Getenv("FLAGS_API_KEYS"); spec != "" {
		keys, err := flags.ParseAPIKeys(spec)
		if err != nil {
			log.Fatal(err)
		}
		auth, err := flags.NewAuthenticator(keys...)
		if err != nil {
			log.Fatal(err)
		}
		handler = auth.Middleware(handler)
		log.Printf("API key auth enabled (%d keys)", len(keys))
	} else {
		log.Printf("WARNING: FLAGS_API_KEYS not set; API is unauthenticated")
	}
	log.Printf("feature-flag-service listening on %s", *addr)
	log.Fatal(http.ListenAndServe(*addr, handler))
}
