package handlers_test

import (
	"bufio"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/redscaresu/fakegenesys/handlers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// spawnAppWithMITM boots a fresh Application on a random free TCP port
// (plain HTTP) and a separate random free TCP port (CONNECT proxy / TLS
// MITM). Returns both addrs + cleanup.
func spawnAppWithMITM(t *testing.T) (httpAddr, mitmAddr string, app *handlers.Application) {
	t.Helper()
	a, err := handlers.NewApplication(":memory:", false)
	require.NoError(t, err, "NewApplication")
	t.Cleanup(func() { _ = a.Close() })

	// Random ports.
	hl, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err, "listen http")
	httpAddr = hl.Addr().String()
	go func() { _ = http.Serve(hl, a.Router()) }()
	t.Cleanup(func() { _ = hl.Close() })

	ml, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err, "listen mitm")
	mitmAddr = ml.Addr().String()
	_ = ml.Close() // ListenAndServe re-binds
	go func() { _ = a.MITM().ListenAndServe(mitmAddr) }()

	// Tiny wait so the goroutines bind their listeners.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		c, err := net.Dial("tcp", mitmAddr)
		if err == nil {
			c.Close()
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	return httpAddr, mitmAddr, a
}

// TestMITM_CACertEndpoint asserts /mock/ca-cert returns a PEM-encoded
// certificate that parses as a real x509 cert.
func TestMITM_CACertEndpoint(t *testing.T) {
	httpAddr, _, _ := spawnAppWithMITM(t)
	resp, err := http.Get("http://" + httpAddr + "/mock/ca-cert")
	require.NoError(t, err, "get ca-cert")
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	body, _ := io.ReadAll(resp.Body)
	require.True(t, strings.HasPrefix(string(body), "-----BEGIN CERTIFICATE-----"),
		"body does not look like PEM:\n%s", body)
	pool := x509.NewCertPool()
	assert.True(t, pool.AppendCertsFromPEM(body), "CA cert PEM did not parse")
}

// TestMITM_HTTPSProxyEndToEnd is the load-bearing test: with the CA cert
// trusted, an http.Client routed via HTTPS_PROXY can reach an arbitrary
// target hostname (api.mypurecloud.com) and have its request handled by
// the fakegenesys router as if the call went directly.
func TestMITM_HTTPSProxyEndToEnd(t *testing.T) {
	httpAddr, mitmAddr, _ := spawnAppWithMITM(t)

	// Fetch the CA cert and build a CertPool the client will trust.
	resp, err := http.Get("http://" + httpAddr + "/mock/ca-cert")
	require.NoError(t, err, "get ca-cert")
	caPEM, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	pool := x509.NewCertPool()
	require.True(t, pool.AppendCertsFromPEM(caPEM), "ca cert pool")

	// HTTPS_PROXY-style HTTP client.
	proxyURL, _ := url.Parse("http://" + mitmAddr)
	client := &http.Client{
		Transport: &http.Transport{
			Proxy:           http.ProxyURL(proxyURL),
			TLSClientConfig: &tls.Config{RootCAs: pool},
		},
		Timeout: 5 * time.Second,
	}

	// Mint a token at /login/oauth/token via the MITM. The provider
	// uses this exact path for client_credentials auth.
	form := strings.NewReader("grant_type=client_credentials&client_id=x&client_secret=y")
	tokReq, _ := http.NewRequest(http.MethodPost,
		"https://api.mypurecloud.com/login/oauth/token", form)
	tokReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	tokResp, err := client.Do(tokReq)
	require.NoError(t, err, "POST /login/oauth/token via mitm")
	defer tokResp.Body.Close()
	if tokResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(tokResp.Body)
		t.Fatalf("status %d, body=%s", tokResp.StatusCode, body)
	}
	var tok struct {
		AccessToken string `json:"access_token"`
		TokenType   string `json:"token_type"`
		ExpiresIn   int    `json:"expires_in"`
	}
	require.NoError(t, json.NewDecoder(tokResp.Body).Decode(&tok), "decode token")
	require.NotEmpty(t, tok.AccessToken, "empty access_token")
	require.Equal(t, "bearer", tok.TokenType, "token_type")

	// Use the token to hit a real API endpoint via the same MITM.
	usersReq, _ := http.NewRequest(http.MethodGet,
		"https://api.mypurecloud.com/api/v2/users", nil)
	usersReq.Header.Set("Authorization", "Bearer "+tok.AccessToken)
	usersResp, err := client.Do(usersReq)
	require.NoError(t, err, "GET /api/v2/users via mitm")
	defer usersResp.Body.Close()
	if usersResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(usersResp.Body)
		t.Fatalf("status %d, body=%s", usersResp.StatusCode, body)
	}
}

// TestMITM_NonConnectMethod returns 405 with a clear message — the proxy
// doesn't model upstream forwarding.
func TestMITM_NonConnectMethod(t *testing.T) {
	_, mitmAddr, _ := spawnAppWithMITM(t)
	conn, err := net.Dial("tcp", mitmAddr)
	require.NoError(t, err, "dial")
	defer conn.Close()
	fmt.Fprintf(conn, "GET / HTTP/1.1\r\nHost: example.com\r\n\r\n")
	br := bufio.NewReader(conn)
	resp, err := http.ReadResponse(br, nil)
	require.NoError(t, err, "read response")
	resp.Body.Close()
	assert.Equal(t, http.StatusMethodNotAllowed, resp.StatusCode)
}
