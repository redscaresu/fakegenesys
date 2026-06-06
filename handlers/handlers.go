// Package handlers wires the HTTP surface for fakegenesys.
//
// One *Application owns one chi router and one repository handle.
// Per-resource handlers (handlers/user.go, handlers/group.go, etc.)
// attach their routes inside RegisterRoutes. Single binary, single
// process — no plugin layer.
package handlers

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/redscaresu/fakegenesys/models"
	"github.com/redscaresu/fakegenesys/repository"
)

// Application is the top-level wiring struct. Holds the chi router,
// the repository handle, the OAuth token store, and the TLS MITM proxy.
type Application struct {
	router *chi.Mux
	repo   *repository.Repository
	tokens *tokenStore
	mitm   *tlsMITM
	echo   bool
	dbPath string

	// queueWrapupAssoc tracks {queueID -> [codeID]} in-process. The
	// Genesys API exposes queue ↔ wrapup-code associations as a
	// subresource (POST /queues/{id}/wrapupcodes, etc.). Kept off the
	// SQLite layer for now — lossy across restarts but sufficient
	// within a single infrafactory run. See routing_queue.go for usage.
	wrapupMu      sync.Mutex
	queueWrapups  map[string][]string

	// subresMu guards the per-user subresource maps (routingskills,
	// routinglanguages). Same pattern + same caveats as queueWrapups.
	subresMu      sync.Mutex
	userSkills    map[string][]userProficiencyRef
	userLanguages map[string][]userProficiencyRef
}

func (app *Application) queueWrapupCodes(queueID string) []string {
	app.wrapupMu.Lock()
	defer app.wrapupMu.Unlock()
	out := make([]string, len(app.queueWrapups[queueID]))
	copy(out, app.queueWrapups[queueID])
	return out
}

func (app *Application) addQueueWrapupCode(queueID, codeID string) {
	app.wrapupMu.Lock()
	defer app.wrapupMu.Unlock()
	if app.queueWrapups == nil {
		app.queueWrapups = map[string][]string{}
	}
	for _, existing := range app.queueWrapups[queueID] {
		if existing == codeID {
			return
		}
	}
	app.queueWrapups[queueID] = append(app.queueWrapups[queueID], codeID)
}

func (app *Application) removeQueueWrapupCode(queueID, codeID string) {
	app.wrapupMu.Lock()
	defer app.wrapupMu.Unlock()
	filtered := app.queueWrapups[queueID][:0]
	for _, c := range app.queueWrapups[queueID] {
		if c != codeID {
			filtered = append(filtered, c)
		}
	}
	app.queueWrapups[queueID] = filtered
}

// NewApplication boots an Application with an ephemeral TLS MITM CA
// (regenerated each boot). Use NewApplicationWithCADir to persist the
// CA across boots so the user's keychain trust survives.
func NewApplication(dbPath string, echo bool) (*Application, error) {
	return NewApplicationWithCADir(dbPath, echo, "")
}

// NewApplicationWithCADir boots an Application using a persisted CA
// (if caDir is non-empty AND contains ca-cert.pem + ca-key.pem) or
// generates a fresh one and saves it to caDir for subsequent boots.
// caDir == "" preserves the original ephemeral behavior.
func NewApplicationWithCADir(dbPath string, echo bool, caDir string) (*Application, error) {
	repo, err := repository.New(dbPath)
	if err != nil {
		return nil, err
	}
	tokens := newTokenStore()
	repo.RegisterCache(tokens)
	app := &Application{
		router: chi.NewRouter(),
		repo:   repo,
		tokens: tokens,
		echo:   echo,
		dbPath: dbPath,
	}

	app.router.Use(middleware.Recoverer)
	if echo {
		app.router.Use(echoMiddleware)
	}
	app.RegisterRoutes(app.router)

	// S116: TLS MITM proxy for HTTPS_PROXY-based provider redirection.
	// S116b: CA persisted to caDir (when set) so keychain trust
	// survives restarts.
	mitm, err := newTLSMITMWithCADir(app.router, caDir)
	if err != nil {
		_ = repo.Close()
		return nil, fmt.Errorf("init tls mitm: %w", err)
	}
	app.mitm = mitm
	return app, nil
}

// MITM exposes the TLS proxy for the cmd binary (to call
// ListenAndServeTLS) and for tests.
func (app *Application) MITM() *tlsMITM { return app.mitm }

// Router returns the chi router for serving HTTP traffic.
func (app *Application) Router() http.Handler { return app.router }

// Repository exposes the underlying repository handle. Used by tests
// and per-resource helpers.
func (app *Application) Repository() *repository.Repository { return app.repo }

// Tokens exposes the in-process OAuth token store. Used by tests that
// want to pre-seed a token or assert its lifecycle.
func (app *Application) Tokens() *tokenStore { return app.tokens }

// Close releases any resources the Application holds. Safe to call
// multiple times.
func (app *Application) Close() error {
	if app.repo == nil {
		return nil
	}
	err := app.repo.Close()
	app.repo = nil
	return err
}

// RegisterRoutes attaches every handler to the router.
//
// Route topology:
//
//	POST   /oauth/token         (unauthenticated — issues Bearer tokens)
//	*      /mock/*              (unauthenticated — admin lifecycle)
//	GET    /healthz             (unauthenticated)
//	(everything else)           (Bearer-required via bearerAuth middleware)
//
// New resources add a single call here (e.g., app.registerUserRoutes(r))
// inside the bearer-required group.
func (app *Application) RegisterRoutes(r chi.Router) {
	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})

	// Unauthenticated routes: token mint + admin lifecycle.
	app.registerOAuthRoutes(r)
	app.registerAdminRoutes(r)

	// Authenticated API surface, mounted under /api/v2 so the bearer
	// middleware runs even when no per-resource handler matches (chi
	// routes middleware via subrouter prefix matching; root-level
	// NotFound would bypass the auth chain).
	r.Route("/api/v2", func(ar chi.Router) {
		ar.Use(app.bearerAuth)
		// S116c: org probe — SDK calls /api/v2/organizations/me
		// immediately after auth.
		app.registerOrganizationRoutes(ar)
		// S109 identity:
		app.registerUserRoutes(ar)
		app.registerUserSubresourceRoutes(ar)
		app.registerGroupRoutes(ar)
		app.registerLocationRoutes(ar)
		app.registerAuthRoleRoutes(ar)
		app.registerOAuthClientRoutes(ar)
		// S110 routing:
		app.registerRoutingQueueRoutes(ar)
		app.registerRoutingSkillRoutes(ar)
		app.registerRoutingWrapupcodeRoutes(ar)
		app.registerRoutingLanguageRoutes(ar)
		app.registerRoutingUtilizationRoutes(ar)
		// S116c: voicemail userpolicies — provider read-after-create
		// hits /api/v2/voicemail/userpolicies/{userId}.
		app.registerVoicemailRoutes(ar)
		// S111 architect / responsemanagement / IDP.
		// Datatable routes register before flow routes so chi matches
		// the more-specific /flows/datatables prefix first.
		app.registerArchitectDatatableRoutes(ar)
		app.registerArchitectUserPromptRoutes(ar)
		app.registerFlowRoutes(ar)
		app.registerResponseManagementRoutes(ar)
		app.registerIDPGenericRoutes(ar)
		// Wildcard catch-all so unmatched /api/v2/* paths still run
		// through the bearer middleware (chi requires at least one
		// registered pattern for the subrouter prefix to dispatch).
		ar.Handle("/*", http.HandlerFunc(unimplementedHandler))
	})

	// Catch-all for paths outside /api/v2/* so the next caller sees
	// what's missing (e.g. typo in API base or wrong port).
	r.NotFound(unimplementedHandler)
	r.MethodNotAllowed(unimplementedHandler)
}

// bearerAuth verifies the Authorization: Bearer <token> header against
// the in-process token store. Returns 401 on missing/invalid tokens.
func (app *Application) bearerAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		// RFC 6750 § 2.1: the auth-scheme token is case-insensitive.
		// Real Genesys accepts both "Bearer" and "bearer" — we do too.
		// (S112 finding #13.)
		if len(auth) < len("Bearer ") || !strings.EqualFold(auth[:len("Bearer ")], "Bearer ") {
			writeError(w, http.StatusUnauthorized, "authentication.required",
				"Bearer token required")
			return
		}
		token := auth[len("Bearer "):]
		if !app.tokens.Valid(token) {
			writeError(w, http.StatusUnauthorized, "authentication.invalid",
				"Bearer token invalid or expired")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// unimplementedHandler returns 501 and logs the request. No silent 200s.
func unimplementedHandler(w http.ResponseWriter, r *http.Request) {
	log.Printf("UNIMPLEMENTED: %s %s", r.Method, r.URL.Path)
	writeError(w, http.StatusNotImplemented, "not.implemented",
		"fakegenesys does not yet model this endpoint; see logs")
}

func echoMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log.Printf("echo: %s %s", r.Method, r.URL.Path)
		next.ServeHTTP(w, r)
	})
}

// writeError serializes a Genesys-shaped error body. contextID is
// generated by the caller via uuid.NewString() when present; tests
// typically don't assert on it.
func writeError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(models.ErrorResponse{
		Status:  status,
		Code:    code,
		Message: message,
	})
}

// writeJSONStatus is a convenience helper for handlers and admin routes.
func writeJSONStatus(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
