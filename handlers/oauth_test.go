package handlers_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/redscaresu/fakegenesys/handlers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func decodeJSON(resp *http.Response, into any) error {
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if len(b) == 0 {
		return nil
	}
	return json.Unmarshal(b, into)
}

func newTestApp(t *testing.T) (*handlers.Application, *httptest.Server) {
	t.Helper()
	app, err := handlers.NewApplication(":memory:", false)
	require.NoError(t, err, "NewApplication")
	srv := httptest.NewServer(app.Router())
	t.Cleanup(func() {
		srv.Close()
		_ = app.Close()
	})
	return app, srv
}

func postForm(t *testing.T, srv *httptest.Server, path, body string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, srv.URL+path, strings.NewReader(body))
	require.NoError(t, err, "NewRequest")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := srv.Client().Do(req)
	require.NoError(t, err, "Do")
	return resp
}

func mintToken(t *testing.T, srv *httptest.Server) string {
	t.Helper()
	resp := postForm(t, srv,
		"/oauth/token",
		"grant_type=client_credentials&client_id=any&client_secret=any")
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode, "mint")
	var body struct {
		AccessToken string `json:"access_token"`
		TokenType   string `json:"token_type"`
		ExpiresIn   int    `json:"expires_in"`
	}
	require.NoError(t, decodeJSON(resp, &body), "decode")
	require.NotEmpty(t, body.AccessToken, "empty access_token")
	require.Equal(t, "bearer", body.TokenType, "token_type")
	require.Equal(t, 3600, body.ExpiresIn, "expires_in")
	return body.AccessToken
}

func TestOAuthToken_ClientCredentials_Issues(t *testing.T) {
	_, srv := newTestApp(t)
	mintToken(t, srv)
}

func TestOAuthToken_RejectsUnsupportedGrantType(t *testing.T) {
	_, srv := newTestApp(t)
	resp := postForm(t, srv,
		"/oauth/token",
		"grant_type=password&username=x&password=y")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestOAuthToken_RejectsMissingClientFields(t *testing.T) {
	_, srv := newTestApp(t)
	resp := postForm(t, srv,
		"/oauth/token",
		"grant_type=client_credentials&client_id=&client_secret=")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

// TestContract_oauth_token_basic_auth — RFC 6749 § 2.3.1. The Genesys
// Go SDK uses HTTP Basic auth for client credentials instead of form
// params; before S116c we rejected those calls with 400 and the
// provider failed to configure. Asserts: form body has only
// grant_type=client_credentials (no client_id/client_secret); Basic
// Auth header carries the credentials; response is 200 with a
// non-empty access_token, token_type=bearer, positive integer
// expires_in. Both /oauth/token and the SDK's mirror endpoint
// /login/oauth/token must behave identically.
//
// Paired with CRITICAL[oauth-token-basic-auth] in handlers/oauth.go.
func TestContract_oauth_token_basic_auth(t *testing.T) {
	_, srv := newTestApp(t)
	for _, path := range []string{"/oauth/token", "/login/oauth/token"} {
		t.Run(path, func(t *testing.T) {
			req, _ := http.NewRequest(http.MethodPost, srv.URL+path,
				strings.NewReader("grant_type=client_credentials"))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			req.SetBasicAuth("client-via-basic", "secret-via-basic")
			resp, err := srv.Client().Do(req)
			require.NoError(t, err, "Do")
			defer resp.Body.Close()
			require.Equal(t, http.StatusOK, resp.StatusCode)
			var body struct {
				AccessToken string `json:"access_token"`
				TokenType   string `json:"token_type"`
				ExpiresIn   int    `json:"expires_in"`
			}
			require.NoError(t, decodeJSON(resp, &body), "decode")
			assert.NotEmpty(t, body.AccessToken, "empty access_token")
			assert.Equal(t, "bearer", body.TokenType, "token_type")
			assert.Positive(t, body.ExpiresIn, "expires_in")
		})
	}
}

func TestBearerAuth_RejectsMissingHeader(t *testing.T) {
	_, srv := newTestApp(t)
	resp, err := srv.Client().Get(srv.URL + "/api/v2/users")
	require.NoError(t, err, "Get")
	defer resp.Body.Close()
	// Without a resource handler registered yet, /api/v2/users returns
	// 501. The bearerAuth middleware should reject FIRST with 401 — so
	// 401 is the expected result in S108 (before S109 wires routes).
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

func TestBearerAuth_RejectsInvalidToken(t *testing.T) {
	_, srv := newTestApp(t)
	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/v2/users", nil)
	req.Header.Set("Authorization", "Bearer not-a-real-token")
	resp, err := srv.Client().Do(req)
	require.NoError(t, err, "Do")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

func TestMockReset_InvalidatesTokens(t *testing.T) {
	app, srv := newTestApp(t)
	tok := mintToken(t, srv)
	require.True(t, app.Tokens().Valid(tok), "expected token valid before reset")
	// Hit /mock/reset
	resp, err := srv.Client().Post(srv.URL+"/mock/reset", "application/json", nil)
	require.NoError(t, err, "Post reset")
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode, "reset status")
	assert.False(t, app.Tokens().Valid(tok), "expected token invalid after reset")
}
