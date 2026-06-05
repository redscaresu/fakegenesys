// Package testutil is the test-side helper layer for fakegenesys.
//
// Every fakegenesys endpoint speaks REST/JSON (no protocol multiplex
// the way fakeaws does), so this package exposes a single JSON-shaped
// Do* surface. NewTestServer mints a fresh Bearer token and pre-loads
// it into the Authorization header on every helper.
//
// Path/endpoint builders live next to their per-resource tests.
package testutil

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/redscaresu/fakegenesys/handlers"
)

// TestServer is the per-test fakegenesys instance plus pre-minted
// Bearer token. Tests should call testutil.NewTestServer(t) at the top.
type TestServer struct {
	HTTP    *httptest.Server
	App     *handlers.Application
	Token   string
	Cleanup func()
}

// NewTestServer boots an in-memory fakegenesys on a random local port,
// mints a fresh Bearer token, and returns it for use in handler tests.
// t.Cleanup wires down close.
func NewTestServer(t *testing.T) *TestServer {
	t.Helper()
	app, err := handlers.NewApplication(":memory:", false)
	if err != nil {
		t.Fatalf("testutil: NewTestServer: %v", err)
	}
	srv := httptest.NewServer(app.Router())
	token := app.Tokens().Issue()
	cleanup := func() {
		srv.Close()
		_ = app.Close()
	}
	t.Cleanup(cleanup)
	return &TestServer{HTTP: srv, App: app, Token: token, Cleanup: cleanup}
}

// DoRaw fires a request against the test server. Caller-supplied path
// is rewritten against srv.URL; Authorization header is auto-set to
// the test's Bearer token unless already present.
func (ts *TestServer) DoRaw(t *testing.T, method, path string, body []byte, headers http.Header) (*http.Response, []byte) {
	t.Helper()
	var br io.Reader
	if body != nil {
		br = bytes.NewReader(body)
	}
	req, err := http.NewRequest(method, ts.HTTP.URL+path, br)
	if err != nil {
		t.Fatalf("testutil.DoRaw: NewRequest: %v", err)
	}
	for k, vs := range headers {
		req.Header[k] = vs
	}
	if req.Header.Get("Authorization") == "" {
		req.Header.Set("Authorization", "Bearer "+ts.Token)
	}
	if body != nil && req.Header.Get("Content-Type") == "" {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := ts.HTTP.Client().Do(req)
	if err != nil {
		t.Fatalf("testutil.DoRaw: %s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	out, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("testutil.DoRaw: read body: %v", err)
	}
	return resp, out
}

// DoJSON marshals body to JSON, fires the request, and (if into != nil)
// decodes the response body into it. Returns the response for status
// inspection.
func (ts *TestServer) DoJSON(t *testing.T, method, path string, body, into any) *http.Response {
	t.Helper()
	var raw []byte
	if body != nil {
		var err error
		raw, err = json.Marshal(body)
		if err != nil {
			t.Fatalf("testutil.DoJSON: marshal: %v", err)
		}
	}
	resp, out := ts.DoRaw(t, method, path, raw, nil)
	if into != nil && len(out) > 0 {
		if err := json.Unmarshal(out, into); err != nil {
			t.Fatalf("testutil.DoJSON: unmarshal: %v\nbody: %s", err, out)
		}
	}
	return resp
}

// MustForm posts form-encoded data (used by the OAuth token endpoint).
func (ts *TestServer) MustForm(t *testing.T, path, body string) *http.Response {
	t.Helper()
	resp, _ := ts.DoRaw(t, http.MethodPost, path,
		[]byte(body),
		http.Header{"Content-Type": []string{"application/x-www-form-urlencoded"}})
	return resp
}

// PostJSON / GetJSON / PutJSON / PatchJSON / DeleteJSON are thin
// convenience wrappers around DoJSON for the most common verbs.
func (ts *TestServer) PostJSON(t *testing.T, path string, body, into any) *http.Response {
	return ts.DoJSON(t, http.MethodPost, path, body, into)
}
func (ts *TestServer) GetJSON(t *testing.T, path string, into any) *http.Response {
	return ts.DoJSON(t, http.MethodGet, path, nil, into)
}
func (ts *TestServer) PutJSON(t *testing.T, path string, body, into any) *http.Response {
	return ts.DoJSON(t, http.MethodPut, path, body, into)
}
func (ts *TestServer) PatchJSON(t *testing.T, path string, body, into any) *http.Response {
	return ts.DoJSON(t, http.MethodPatch, path, body, into)
}
func (ts *TestServer) DeleteJSON(t *testing.T, path string) *http.Response {
	return ts.DoJSON(t, http.MethodDelete, path, nil, nil)
}

// MustOK asserts the response is 2xx and t.Fatals with body otherwise.
func MustOK(t *testing.T, resp *http.Response, body []byte) {
	t.Helper()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		t.Fatalf("status = %d, want 2xx; body: %s", resp.StatusCode, body)
	}
}

// MustStatus asserts a specific status code.
func MustStatus(t *testing.T, resp *http.Response, want int, body []byte) {
	t.Helper()
	if resp.StatusCode != want {
		t.Fatalf("status = %d, want %d; body: %s", resp.StatusCode, want, body)
	}
}

// trim is a small helper used in formatted error logs.
func trim(s string) string { return strings.TrimSpace(s) }

var _ = trim // reserved for future helper use
