// Package main is the fakegenesys entry point.
//
// fakegenesys is a local Go-based mock of the Genesys Cloud CCaaS HTTP
// API surface. It boots a chi router holding one *Application struct,
// which holds one *Repository, which holds one SQLite handle. Adding a
// resource is one Go file. See AGENTS.md and README.md for the full
// picture.
package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"

	"github.com/redscaresu/fakegenesys/handlers"
)

func main() {
	port := flag.Int("port", 8083, "HTTP listen port (default 8083; mockway uses 8080, fakegcp 8081, fakeaws 8082)")
	dbPath := flag.String("db", ":memory:", "SQLite path; ':memory:' for ephemeral, file path for persistent")
	echo := flag.Bool("echo", false, "log every request method+path (useful for discovering unimplemented endpoints)")
	flag.Parse()

	app, err := handlers.NewApplication(*dbPath, *echo)
	if err != nil {
		log.Fatalf("fakegenesys: init: %v", err)
	}
	defer app.Close()

	addr := fmt.Sprintf(":%d", *port)
	log.Printf("fakegenesys: listening on %s (db=%s, echo=%v)", addr, *dbPath, *echo)
	if err := http.ListenAndServe(addr, app.Router()); err != nil {
		log.Fatalf("fakegenesys: serve: %v", err)
	}
}
