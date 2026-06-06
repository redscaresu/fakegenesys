// Package main is the fakegenesys entry point.
//
// fakegenesys is a local Go-based mock of the Genesys Cloud CCaaS HTTP
// API surface. It boots a chi router holding one *Application struct,
// which holds one *Repository, which holds one SQLite handle. Adding a
// resource is one Go file. See AGENTS.md and README.md for the full
// picture.
//
// Default CA persistence: ~/.fakegenesys/ca-{cert,key}.pem (S116b).
// Trust installed via `make fakegenesys-trust-ca-darwin` (or
// SSL_CERT_FILE on Linux) survives across restarts. Override with
// --ca-dir=... or disable with --ca-dir="" (ephemeral CA per boot).
package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"

	"github.com/redscaresu/fakegenesys/handlers"
)

// defaultCADir returns ~/.fakegenesys or "" if the home dir can't be
// resolved (rare; the user can always pass --ca-dir explicitly).
func defaultCADir() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	return filepath.Join(home, ".fakegenesys")
}

func main() {
	port := flag.Int("port", 8083, "HTTP listen port (default 8083; mockway uses 8080, fakegcp 8081, fakeaws 8082)")
	tlsPort := flag.Int("tls-port", 8443, "TLS MITM CONNECT-proxy port (S116). Set to 0 to disable.")
	dbPath := flag.String("db", ":memory:", "SQLite path; ':memory:' for ephemeral, file path for persistent")
	echo := flag.Bool("echo", false, "log every request method+path (useful for discovering unimplemented endpoints)")
	caDir := flag.String("ca-dir", defaultCADir(), "Directory for persisted MITM CA (cert + key). Empty disables persistence. Default: ~/.fakegenesys")
	flag.Parse()

	app, err := handlers.NewApplicationWithCADir(*dbPath, *echo, *caDir)
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
