package handlers

import (
	"net/http"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// tokenTTL is the lifetime of an issued Bearer token. Mirrors the
// Genesys Cloud public default of 3600 seconds. Tests that exercise
// expiry override this via the package-level testHookNow.
const tokenTTL = 3600 * time.Second

// tokenStore is an in-process Bearer token cache. Satisfies
// repository.Cache so /mock/reset purges all live tokens.
type tokenStore struct {
	mu     sync.Mutex
	tokens map[string]time.Time // token -> expiry
}

func newTokenStore() *tokenStore {
	return &tokenStore{tokens: make(map[string]time.Time)}
}

// Name implements repository.Cache.
func (s *tokenStore) Name() string { return "oauth.tokens" }

// Reset wipes the store.
func (s *tokenStore) Reset() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tokens = make(map[string]time.Time)
	return nil
}

// Snapshot is a no-op — tokens are deliberately not persisted across
// snapshot/restore. Test-side e2e flows re-mint tokens after restore.
func (s *tokenStore) Snapshot(_ string) error { return nil }

// Restore is a no-op for the same reason as Snapshot.
func (s *tokenStore) Restore(_ string) error { return nil }

// Issue mints a fresh token and stores it with TTL.
func (s *tokenStore) Issue() string {
	tok := uuid.NewString()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tokens[tok] = time.Now().Add(tokenTTL)
	return tok
}

// Valid reports whether the token is in the store and not expired.
// Expired entries are lazily pruned on lookup.
func (s *tokenStore) Valid(tok string) bool {
	if tok == "" {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	exp, ok := s.tokens[tok]
	if !ok {
		return false
	}
	if time.Now().After(exp) {
		delete(s.tokens, tok)
		return false
	}
	return true
}

// registerOAuthRoutes attaches the OAuth token endpoint at every path
// the Genesys Go SDK and Terraform provider may hit. The provider
// uses /login/oauth/token (the real login.mypurecloud.com subdomain
// path), while bare REST callers use /oauth/token. fakegenesys
// collapses both into the same handler since it can't distinguish
// subdomains on one port.
//
// All token routes live outside the bearer-auth group — they're the
// bootstrap endpoints.
func (app *Application) registerOAuthRoutes(r chi.Router) {
	r.Post("/oauth/token", app.handleOAuthToken)
	r.Post("/login/oauth/token", app.handleOAuthToken)
}

// handleOAuthToken implements the client_credentials grant per
// RFC 6749 § 4.4. Form-encoded request, JSON response.
//
// Request:
//
//	POST /oauth/token
//	Content-Type: application/x-www-form-urlencoded
//	grant_type=client_credentials&client_id=<id>&client_secret=<secret>
//
// Response (200):
//
//	{"access_token":"<uuid>","token_type":"bearer","expires_in":3600}
//
// fakegenesys deliberately accepts any client_id / client_secret pair
// — the harness's job is to validate wire shape, not credentials. Real
// Genesys obviously enforces credentials; tests that need that
// behavior can wrap the handler.
func (app *Application) handleOAuthToken(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		writeError(w, http.StatusBadRequest, "invalid.request",
			"form decode: "+err.Error())
		return
	}
	if r.PostForm.Get("grant_type") != "client_credentials" {
		writeError(w, http.StatusBadRequest, "unsupported_grant_type",
			"only client_credentials is supported")
		return
	}
	// CRITICAL[oauth-token-basic-auth]: RFC 6749 § 2.3.1 allows HTTP
	// Basic auth as an alternative to posting client_id/client_secret
	// in the form body. The Genesys Cloud Go SDK uses Basic auth —
	// without this branch the form params are empty and we 400 every
	// provider token mint. (S116c follow-up.) Locked in by
	// TestContract_oauth_token_basic_auth.
	clientID := r.PostForm.Get("client_id")
	clientSecret := r.PostForm.Get("client_secret")
	if clientID == "" || clientSecret == "" {
		if u, p, ok := r.BasicAuth(); ok {
			clientID = u
			clientSecret = p
		}
	}
	if clientID == "" || clientSecret == "" {
		writeError(w, http.StatusBadRequest, "invalid_client",
			"client_id and client_secret are required (form params or HTTP Basic auth)")
		return
	}
	tok := app.tokens.Issue()
	writeJSONStatus(w, http.StatusOK, map[string]any{
		"access_token": tok,
		"token_type":   "bearer",
		"expires_in":   int(tokenTTL.Seconds()),
	})
}
