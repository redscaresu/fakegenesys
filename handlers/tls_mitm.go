package handlers

import (
	"bufio"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"log"
	"math/big"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// tlsMITM is a TLS-terminating HTTP CONNECT proxy. It exists to make the
// `mypurecloud/genesyscloud` Terraform provider hit fakegenesys for
// auth + API calls without modifying the provider.
//
// Wire flow:
//  1. Client uses HTTPS_PROXY=http://localhost:8443.
//  2. Client sends "CONNECT api.mypurecloud.com:443 HTTP/1.1" to this listener.
//  3. tlsMITM replies "200 Connection established".
//  4. tlsMITM wraps the (still-open) connection in a TLS server using a
//     dynamically-issued leaf cert with SAN=api.mypurecloud.com, signed
//     by our boot-time CA.
//  5. Client completes TLS handshake (trusts our CA because the harness
//     wrote it to SSL_CERT_FILE).
//  6. Decrypted HTTP traffic flows through the same chi router the regular
//     :8083 listener uses — /api/v2/*, /login/oauth/token, /mock/state,
//     /healthz. No upstream forwarding; we ARE the upstream.
//
// The CA cert is exposed via GET /mock/ca-cert on the plain :8083 port so
// infrafactory (and any other harness) can fetch it at runtime and trust
// it without bundling a pre-baked cert.
type tlsMITM struct {
	// ca is the boot-time self-signed CA used to sign every leaf.
	ca       *x509.Certificate
	caPEM    []byte
	caKey    *rsa.PrivateKey
	caKeyPEM []byte

	// leafCache maps hostname -> ready-to-serve *tls.Certificate.
	// We generate leaves lazily on first sight of each hostname and
	// cache forever (process lifetime). Hostnames are bounded — Genesys
	// has a handful of regional login subdomains plus api.mypurecloud.com.
	leafMu    sync.Mutex
	leafCache map[string]*tls.Certificate

	// httpHandler is the same chi router that serves the plain :8083
	// listener. We attach it to the TLS server after the MITM handshake.
	httpHandler http.Handler
}

// newTLSMITM builds the proxy. Generates a fresh CA at boot unless one
// is already cached on disk via newTLSMITMWithCADir.
func newTLSMITM(handler http.Handler) (*tlsMITM, error) {
	return newTLSMITMWithCADir(handler, "")
}

// newTLSMITMWithCADir is the persistence-aware constructor. If caDir
// is non-empty AND contains a readable ca-cert.pem + ca-key.pem pair,
// the existing CA is reused so the user's keychain trust survives
// across fakegenesys restarts. Otherwise a fresh CA is generated and
// (if caDir is non-empty) written to caDir for subsequent boots.
//
// caDir == "" preserves the original ephemeral behavior — useful for
// tests where each fakegenesys instance wants its own CA.
func newTLSMITMWithCADir(handler http.Handler, caDir string) (*tlsMITM, error) {
	var (
		ca       *x509.Certificate
		caPEM    []byte
		caKey    *rsa.PrivateKey
		caKeyPEM []byte
	)
	if caDir != "" {
		if loaded, err := loadCAFromDir(caDir); err == nil {
			ca, caPEM, caKey, caKeyPEM = loaded.cert, loaded.certPEM, loaded.key, loaded.keyPEM
		}
	}
	if ca == nil {
		var err error
		ca, caPEM, caKey, caKeyPEM, err = generateCA()
		if err != nil {
			return nil, fmt.Errorf("generate CA: %w", err)
		}
		if caDir != "" {
			if err := saveCAToDir(caDir, caPEM, caKeyPEM); err != nil {
				log.Printf("fakegenesys-tls: save CA to %s: %v (continuing with ephemeral CA)", caDir, err)
			}
		}
	}
	return &tlsMITM{
		ca:          ca,
		caPEM:       caPEM,
		caKey:       caKey,
		caKeyPEM:    caKeyPEM,
		leafCache:   make(map[string]*tls.Certificate),
		httpHandler: handler,
	}, nil
}

type loadedCA struct {
	cert    *x509.Certificate
	certPEM []byte
	key     *rsa.PrivateKey
	keyPEM  []byte
}

func loadCAFromDir(dir string) (*loadedCA, error) {
	certPath := filepath.Join(dir, "ca-cert.pem")
	keyPath := filepath.Join(dir, "ca-key.pem")
	certPEM, err := os.ReadFile(certPath)
	if err != nil {
		return nil, err
	}
	keyPEM, err := os.ReadFile(keyPath)
	if err != nil {
		return nil, err
	}
	certBlock, _ := pem.Decode(certPEM)
	if certBlock == nil {
		return nil, fmt.Errorf("decode %s: invalid PEM", certPath)
	}
	cert, err := x509.ParseCertificate(certBlock.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", certPath, err)
	}
	keyBlock, _ := pem.Decode(keyPEM)
	if keyBlock == nil {
		return nil, fmt.Errorf("decode %s: invalid PEM", keyPath)
	}
	key, err := x509.ParsePKCS1PrivateKey(keyBlock.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", keyPath, err)
	}
	return &loadedCA{cert: cert, certPEM: certPEM, key: key, keyPEM: keyPEM}, nil
}

func saveCAToDir(dir string, certPEM, keyPEM []byte) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "ca-cert.pem"), certPEM, 0o644); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "ca-key.pem"), keyPEM, 0o600)
}

// CACertPEM returns the PEM-encoded CA cert. Served by GET /mock/ca-cert.
func (m *tlsMITM) CACertPEM() []byte { return m.caPEM }

// ListenAndServe is the public entry point cmd/fakegenesys/main.go uses
// to spin up the proxy. Renamed from listenAndServe so it's visible
// outside the package.
func (m *tlsMITM) ListenAndServe(addr string) error { return m.listenAndServe(addr) }

// listenAndServe runs the CONNECT proxy on addr until ctx is canceled.
// Blocks; caller runs it in its own goroutine.
func (m *tlsMITM) listenAndServe(addr string) error {
	l, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listen %s: %w", addr, err)
	}
	defer l.Close()
	log.Printf("fakegenesys-tls: CONNECT proxy listening on %s (HTTPS_PROXY-compatible)", addr)
	for {
		conn, err := l.Accept()
		if err != nil {
			// Listener closed during shutdown — exit cleanly.
			if errors.Is(err, net.ErrClosed) {
				return nil
			}
			log.Printf("fakegenesys-tls: accept: %v", err)
			continue
		}
		go m.handleConn(conn)
	}
}

// handleConn handles one client connection. Reads the CONNECT request,
// responds 200, then wraps the connection in TLS and dispatches to the
// chi router via http.Server.
func (m *tlsMITM) handleConn(conn net.Conn) {
	defer conn.Close()
	br := bufio.NewReader(conn)
	req, err := http.ReadRequest(br)
	if err != nil {
		if !errors.Is(err, io.EOF) {
			log.Printf("fakegenesys-tls: read request: %v", err)
		}
		return
	}
	if req.Method != http.MethodConnect {
		// Some clients (curl with --proxy http://) may send a regular
		// GET/POST through the proxy. We don't model upstream forwarding
		// — return 405 with a clear message.
		_, _ = conn.Write([]byte("HTTP/1.1 405 Method Not Allowed\r\nContent-Length: 0\r\nConnection: close\r\n\r\n"))
		return
	}

	// req.Host is the "api.mypurecloud.com:443" target. Strip the port.
	host := req.Host
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	if host == "" {
		_, _ = conn.Write([]byte("HTTP/1.1 400 Bad Request\r\nContent-Length: 0\r\nConnection: close\r\n\r\n"))
		return
	}

	// Send the 200 Connection Established response on the plain connection.
	if _, err := conn.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n")); err != nil {
		return
	}

	// Mint or fetch a leaf cert for this hostname.
	leaf, err := m.leafFor(host)
	if err != nil {
		log.Printf("fakegenesys-tls: leaf for %s: %v", host, err)
		return
	}

	// Wrap the connection in TLS server mode.
	tlsConn := tls.Server(conn, &tls.Config{
		Certificates: []tls.Certificate{*leaf},
		MinVersion:   tls.VersionTLS12,
	})
	if err := tlsConn.Handshake(); err != nil {
		log.Printf("fakegenesys-tls: handshake with %s: %v", host, err)
		return
	}

	// Now serve HTTP over the TLS-terminated stream using a one-shot
	// http.Server. This wires the chi router (the same one :8083 uses)
	// onto the MITM'd connection. The single-connection listener pattern
	// is the standard way to bridge a manually-wrapped net.Conn into
	// net/http.
	hcl := &oneConnListener{conn: tlsConn, addr: conn.LocalAddr()}
	server := &http.Server{
		Handler:           m.httpHandler,
		ReadHeaderTimeout: 30 * time.Second,
	}
	// Serve returns when the underlying connection closes; we drop the
	// returned error because EOF on a single-conn listener is normal.
	_ = server.Serve(hcl)
}

// leafFor returns a cached leaf cert for host or mints a new one.
func (m *tlsMITM) leafFor(host string) (*tls.Certificate, error) {
	m.leafMu.Lock()
	defer m.leafMu.Unlock()
	if c, ok := m.leafCache[host]; ok {
		return c, nil
	}
	c, err := generateLeaf(m.ca, m.caKey, host)
	if err != nil {
		return nil, err
	}
	m.leafCache[host] = c
	return c, nil
}

// ---------- CA + leaf generation ----------

func generateCA() (*x509.Certificate, []byte, *rsa.PrivateKey, []byte, error) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	serial, err := rand.Int(rand.Reader, big.NewInt(1<<62))
	if err != nil {
		return nil, nil, nil, nil, err
	}
	template := &x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			CommonName:   "fakegenesys-mitm-ca",
			Organization: []string{"fakegenesys"},
		},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(10 * 365 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature | x509.KeyUsageCRLSign,
		IsCA:                  true,
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(key),
	})
	return cert, certPEM, key, keyPEM, nil
}

func generateLeaf(ca *x509.Certificate, caKey *rsa.PrivateKey, host string) (*tls.Certificate, error) {
	leafKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, err
	}
	serial, err := rand.Int(rand.Reader, big.NewInt(1<<62))
	if err != nil {
		return nil, err
	}
	dnsNames := []string{host}
	// Genesys uses regional subdomains (login.use1.pure.cloud,
	// login.mypurecloud.com, etc.). Including just `host` covers the
	// hostname the client SNI'd; that's enough since we mint a fresh
	// leaf per hostname.
	if !strings.HasPrefix(host, "*.") {
		dnsNames = append(dnsNames, "*."+host)
	}
	template := &x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			CommonName:   host,
			Organization: []string{"fakegenesys-leaf"},
		},
		NotBefore:   time.Now().Add(-time.Hour),
		NotAfter:    time.Now().Add(365 * 24 * time.Hour),
		KeyUsage:    x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
		DNSNames:    dnsNames,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, ca, &leafKey.PublicKey, caKey)
	if err != nil {
		return nil, err
	}
	return &tls.Certificate{
		Certificate: [][]byte{der, ca.Raw},
		PrivateKey:  leafKey,
		Leaf:        nil,
	}, nil
}

// ---------- one-shot listener for http.Server.Serve ----------

// oneConnListener bridges a single already-established net.Conn into
// the net.Listener interface http.Server.Serve expects. After the conn
// is handed out, subsequent Accept calls block until Close is called.
//
// This is the standard idiom for serving HTTP over a manually-wrapped
// connection (e.g., after CONNECT handshake).
type oneConnListener struct {
	conn   net.Conn
	addr   net.Addr
	once   sync.Once
	closed chan struct{}
}

func (l *oneConnListener) Accept() (net.Conn, error) {
	if l.closed == nil {
		l.closed = make(chan struct{})
	}
	var c net.Conn
	l.once.Do(func() { c = l.conn })
	if c != nil {
		return c, nil
	}
	<-l.closed
	return nil, net.ErrClosed
}

func (l *oneConnListener) Close() error {
	if l.closed != nil {
		select {
		case <-l.closed:
		default:
			close(l.closed)
		}
	}
	return l.conn.Close()
}

func (l *oneConnListener) Addr() net.Addr { return l.addr }
