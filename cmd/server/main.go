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

	flags "github.com/saif177/feature-flag-service"
)

func main() {
	addr := flag.String("addr", ":8080", "listen address")
	flag.Parse()

	srv := flags.NewServer(flags.NewEnvironments())
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
