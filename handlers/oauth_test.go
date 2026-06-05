package handlers_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/redscaresu/fakegenesys/handlers"
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
	if err != nil {
		t.Fatalf("NewApplication: %v", err)
	}
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
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	return resp
}

func mintToken(t *testing.T, srv *httptest.Server) string {
	t.Helper()
	resp := postForm(t, srv,
		"/oauth/token",
		"grant_type=client_credentials&client_id=any&client_secret=any")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("mint: status %d", resp.StatusCode)
	}
	var body struct {
		AccessToken string `json:"access_token"`
		TokenType   string `json:"token_type"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := decodeJSON(resp, &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.AccessToken == "" {
		t.Fatalf("empty access_token")
	}
	if body.TokenType != "bearer" {
		t.Fatalf("token_type = %q, want bearer", body.TokenType)
	}
	if body.ExpiresIn != 3600 {
		t.Fatalf("expires_in = %d, want 3600", body.ExpiresIn)
	}
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
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

func TestOAuthToken_RejectsMissingClientFields(t *testing.T) {
	_, srv := newTestApp(t)
	resp := postForm(t, srv,
		"/oauth/token",
		"grant_type=client_credentials&client_id=&client_secret=")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

func TestBearerAuth_RejectsMissingHeader(t *testing.T) {
	_, srv := newTestApp(t)
	resp, err := srv.Client().Get(srv.URL + "/api/v2/users")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	defer resp.Body.Close()
	// Without a resource handler registered yet, /api/v2/users returns
	// 501. The bearerAuth middleware should reject FIRST with 401 — so
	// 401 is the expected result in S108 (before S109 wires routes).
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}
}

func TestBearerAuth_RejectsInvalidToken(t *testing.T) {
	_, srv := newTestApp(t)
	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/v2/users", nil)
	req.Header.Set("Authorization", "Bearer not-a-real-token")
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}
}

func TestMockReset_InvalidatesTokens(t *testing.T) {
	app, srv := newTestApp(t)
	tok := mintToken(t, srv)
	if !app.Tokens().Valid(tok) {
		t.Fatalf("expected token valid before reset")
	}
	// Hit /mock/reset
	resp, err := srv.Client().Post(srv.URL+"/mock/reset", "application/json", nil)
	if err != nil {
		t.Fatalf("Post reset: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("reset status = %d, want 200", resp.StatusCode)
	}
	if app.Tokens().Valid(tok) {
		t.Fatalf("expected token invalid after reset")
	}
}
