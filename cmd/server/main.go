// Command server runs the feature-flag HTTP API on -addr (default :8080).
package main

import (
	"flag"
	"log"
	"net/http"

	flags "github.com/saif177/feature-flag-service"
)

func main() {
	addr := flag.String("addr", ":8080", "listen address")
	flag.Parse()

	srv := flags.NewServer(flags.NewEnvironments())
	log.Printf("feature-flag-service listening on %s", *addr)
	log.Fatal(http.ListenAndServe(*addr, srv))
}
