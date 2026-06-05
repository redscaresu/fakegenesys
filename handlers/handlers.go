// Package handlers wires the HTTP surface for fakegenesys.
//
// One *Application owns one chi router and one repository handle.
// Per-resource handlers (handlers/user.go, handlers/group.go, etc.)
// attach their routes inside RegisterRoutes. Single binary, single
// process — no plugin layer.
package handlers

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/redscaresu/fakegenesys/models"
	"github.com/redscaresu/fakegenesys/repository"
)

// Application is the top-level wiring struct. Holds the chi router,
// the repository handle, and the OAuth token store.
type Application struct {
	router *chi.Mux
	repo   *repository.Repository
	tokens *tokenStore
	echo   bool
	dbPath string
}

// NewApplication boots an Application. dbPath is ":memory:" for
// in-memory SQLite or a filesystem path for persistent storage. echo
// toggles per-request method+path logging — useful for discovering
// unimplemented endpoints during provider integration testing.
func NewApplication(dbPath string, echo bool) (*Application, error) {
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
	return app, nil
}

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
		// S109 identity:
		app.registerUserRoutes(ar)
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
		// Per-resource groups for later slices:
		// app.registerArchitectDatatableRoutes(ar) (S111)
		// ...
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
		const prefix = "Bearer "
		if !strings.HasPrefix(auth, prefix) {
			writeError(w, http.StatusUnauthorized, "authentication.required",
				"Bearer token required")
			return
		}
		token := strings.TrimPrefix(auth, prefix)
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
