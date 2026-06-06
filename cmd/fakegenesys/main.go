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
	tlsPort := flag.Int("tls-port", 8443, "TLS MITM CONNECT-proxy port (S116). Set to 0 to disable.")
	dbPath := flag.String("db", ":memory:", "SQLite path; ':memory:' for ephemeral, file path for persistent")
	echo := flag.Bool("echo", false, "log every request method+path (useful for discovering unimplemented endpoints)")
	flag.Parse()

	app, err := handlers.NewApplication(*dbPath, *echo)
	if err != nil {
		log.Fatalf("fakegenesys: init: %v", err)
	}
	defer app.Close()

	if *tlsPort > 0 {
		go func() {
			addr := fmt.Sprintf(":%d", *tlsPort)
			if err := app.MITM().ListenAndServe(addr); err != nil {
				log.Printf("fakegenesys-tls: serve: %v", err)
			}
		}()
	}

	addr := fmt.Sprintf(":%d", *port)
	log.Printf("fakegenesys: listening on %s (db=%s, echo=%v, tls-port=%d)",
		addr, *dbPath, *echo, *tlsPort)
	if err := http.ListenAndServe(addr, app.Router()); err != nil {
		log.Fatalf("fakegenesys: serve: %v", err)
	}
}
