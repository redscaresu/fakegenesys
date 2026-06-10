// Package examples — auto-discovered provider smoke harness for fakegenesys.
//
// For every example dir under examples/{working,misconfigured,updates}/
// this test starts a fresh `fakegenesys` binary on a free port (no
// shared mock state across dirs), copies the example into a temp dir,
// rewrites `localhost:8083` in `providers.tf` to point at the per-test
// port, and runs the per-tree contract:
//
//	working/      apply → plan -detailed-exitcode (no diff) → destroy
//	misconfigured/ apply MUST fail (and if expected.txt is present, the
//	               output MUST contain that error fragment)
//	updates/      apply -var-file=v1.tfvars → plan no-op
//	              → apply -var-file=v2.tfvars → plan no-op → destroy
//
// Adding a directory to ANY of the three trees auto-registers — no
// per-example test wiring. Each subdir is its own t.Run sub-test.
//
// Gating:
//   - FAKEGENESYS_ENABLE_E2E=1 — shells out to `tofu` and spawns the
//     fakegenesys binary. Without the env var, the test t.Skip's with
//     a clear message.
//
// known_broken.yaml allowlist:
//   - examples/known_broken.yaml lists dirs whose idempotency gate is
//     currently expected to fail (each entry references a tracking
//     ticket). Entries skip the drift assertion but still run apply +
//     destroy. If a known-broken dir starts passing idempotency, the
//     test FAILS with "congratulations, remove this entry" —
//     ratchet-only-tighten.
//
// Mirrors fakegcp's per-test fresh-port pattern (state isolation
// across example dirs) rather than fakeaws's shared-server approach.
package examples_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

const (
	gateEnvVar              = "FAKEGENESYS_ENABLE_E2E"
	defaultFakegenesysHost  = "127.0.0.1"
	bootBudget              = 5 * time.Second
	commandBudget           = 8 * time.Minute
	knownBrokenFileName     = "known_broken.yaml"
)

// brokenEntry mirrors examples/known_broken.yaml schema.
type brokenEntry struct {
	Dir     string `yaml:"dir"`
	Symptom string `yaml:"symptom"`
	Ticket  string `yaml:"ticket"`
}

type brokenList struct {
	Entries []brokenEntry `yaml:"entries"`
}

type brokenIndex map[string]brokenEntry

var (
	fakegenesysBinaryOnce sync.Once
	fakegenesysBinaryPath string
	fakegenesysBinaryErr  error
)

func TestKnownBrokenAllowlistSummary(t *testing.T) {
	root := repoRoot(t)
	broken := loadKnownBroken(t, root)
	t.Logf("known_broken summary: %d entries", len(broken))
	keys := make([]string, 0, len(broken))
	for k := range broken {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		e := broken[k]
		t.Logf("known_broken: %s — %s (ticket %s)", k, e.Symptom, e.Ticket)
	}
}

func TestProviderSmokeWorking(t *testing.T) {
	requireE2EGate(t)
	requireTofu(t)
	root := repoRoot(t)
	bin := buildFakegenesysOnce(t, root)
	broken := loadKnownBroken(t, root)
	dir := filepath.Join(root, "examples", "working")
	walkExamplesAndRun(t, dir, "working", broken, bin, runWorkingExample)
}

func TestProviderSmokeMisconfigured(t *testing.T) {
	requireE2EGate(t)
	requireTofu(t)
	root := repoRoot(t)
	bin := buildFakegenesysOnce(t, root)
	broken := loadKnownBroken(t, root)
	dir := filepath.Join(root, "examples", "misconfigured")
	walkExamplesAndRun(t, dir, "misconfigured", broken, bin, runMisconfiguredExample)
}

func TestProviderSmokeUpdates(t *testing.T) {
	requireE2EGate(t)
	requireTofu(t)
	root := repoRoot(t)
	bin := buildFakegenesysOnce(t, root)
	broken := loadKnownBroken(t, root)
	dir := filepath.Join(root, "examples", "updates")
	walkExamplesAndRun(t, dir, "updates", broken, bin, runUpdatesExample)
}

// ----- discovery -----

type runFunc func(t *testing.T, dir, tree string, broken brokenIndex, bin, fakegenesysURL string)

func walkExamplesAndRun(t *testing.T, parent, tree string, broken brokenIndex, bin string, run runFunc) {
	t.Helper()
	entries, err := os.ReadDir(parent)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			t.Logf("skipping %s — directory does not exist yet", parent)
			return
		}
		t.Fatalf("read %s: %v", parent, err)
	}
	for _, ent := range entries {
		if !ent.IsDir() {
			continue
		}
		dir := filepath.Join(parent, ent.Name())
		t.Run(ent.Name(), func(t *testing.T) {
			port, stop := spawnFakegenesys(t, bin)
			defer stop()
			url := fmt.Sprintf("http://%s:%d", defaultFakegenesysHost, port)
			setupGenesysProviderEnv(t, url)
			workDir := copyExample(t, dir, url)
			run(t, workDir, tree, broken, bin, url)
		})
	}
}

// setupGenesysProviderEnv mirrors what infrafactory's
// internal/cli/test_command.go::buildScenarioEnv does for cloud:genesys
// scenarios. The genesyscloud Go SDK ignores GENESYSCLOUD_GATEWAY_* env
// vars and hardcodes login.<region>.pure.cloud, so we route every
// outbound HTTPS call through fakegenesys's TLS MITM CONNECT proxy
// (default :8443 — mapped from the API port + 360 by convention) and
// trust the boot-time CA via SSL_CERT_FILE.
//
// Without this S136 bridge the smoke test fails on every example with
// "Auth Error: 400 - invalid_client" because the provider never reaches
// fakegenesys's /oauth/token endpoint at all (the genesyscloud SDK
// requires credentials in env even when HTTPS_PROXY routes the call
// through our MITM). See docs/plans/fakegenesys-example-drift-fix-plan.md
// (in infrafactory) § S136.
//
// All env vars set via t.Setenv so Go's test runner reverts them
// automatically when the sub-test ends.
func setupGenesysProviderEnv(t *testing.T, fakegenesysURL string) {
	t.Helper()

	// 1. Provider credentials. fakegenesys accepts any client_id /
	//    client_secret per its CRITICAL[oauth-token-basic-auth] contract,
	//    but the SDK still demands they be present in env.
	t.Setenv("GENESYSCLOUD_OAUTHCLIENT_ID", "fake-client-id")
	t.Setenv("GENESYSCLOUD_OAUTHCLIENT_SECRET", "fake-client-secret")
	t.Setenv("GENESYSCLOUD_REGION", "us-east-1")

	// 2. HTTPS_PROXY routes login.<region>.pure.cloud (and every other
	//    api.<region>.pure.cloud call) through fakegenesys's MITM. The
	//    MITM listens on (api_port + 360) by convention — :8083 -> :8443
	//    on the default port. spawnFakegenesys boots both ports; here we
	//    only need the proxy URL.
	proxyURL, ok := derivedTLSProxyURL(fakegenesysURL)
	if !ok {
		t.Fatalf("setupGenesysProviderEnv: cannot derive TLS proxy URL from %q", fakegenesysURL)
	}
	t.Setenv("HTTPS_PROXY", proxyURL)
	t.Setenv("HTTP_PROXY", proxyURL)

	// 3. NO_PROXY: keep tofu's external network paths (registry, GitHub,
	//    HashiCorp release CDN, etc.) off the MITM. Without this, tofu
	//    init fails with "certificate signed by unknown authority"
	//    because our MITM presents leaf certs signed by fakegenesys's
	//    CA for registry.opentofu.org. Standard Go-net/http NO_PROXY
	//    semantics: comma-separated, "*.foo.com" matches subdomains.
	noProxy := strings.Join([]string{
		"registry.opentofu.org",
		"registry.terraform.io",
		"releases.hashicorp.com",
		"github.com",
		"127.0.0.1",
		"localhost",
		".opentofu.org",
		".terraform.io",
		".hashicorp.com",
		".amazonaws.com",
		".githubusercontent.com",
		".github.com",
		".windows.net",
	}, ",")
	t.Setenv("NO_PROXY", noProxy)
	t.Setenv("no_proxy", noProxy) // some clients consult lowercase

	// 4. SSL_CERT_FILE: trust the boot-time CA so the provider's TLS
	//    client accepts the MITM leaf certs. Go's TLS stack walks
	//    SSL_CERT_FILE in addition to the system trust store — our CA
	//    is added ON TOP of system roots so registry.opentofu.org etc.
	//    still validate via the public trust store.
	if certPath, ok := writeCACertToTempFile(t, fakegenesysURL); ok {
		t.Setenv("SSL_CERT_FILE", certPath)
	} else {
		t.Logf("setupGenesysProviderEnv: failed to fetch CA from /mock/ca-cert; TLS verification will fail")
	}

	// 5. FAKEGENESYS_UPLOAD_HOST: tell the flow upload-job handler what
	//    host:port to embed in its presignedUrl response. Without this,
	//    when the provider calls /api/v2/flows/jobs through the MITM,
	//    r.Host comes back as the upstream Genesys domain and the
	//    follow-up PUT routes back through HTTP_PROXY and 405s. The
	//    per-test listener URL is just the fakegenesysURL minus the
	//    scheme — that's the host:port the PUT should land on (and
	//    NO_PROXY=localhost,127.0.0.1 ensures the PUT bypasses the
	//    proxy entirely).
	if parsed, err := url.Parse(fakegenesysURL); err == nil {
		t.Setenv("FAKEGENESYS_UPLOAD_HOST", parsed.Host)
	}
}

// derivedTLSProxyURL maps the fakegenesys API URL (e.g.
// http://127.0.0.1:8083) to its TLS MITM proxy URL (http://127.0.0.1:8443).
// Convention: tls_port = api_port + 360 when api_port is 8083; otherwise
// fall back to :8443 (the binary's --tls-port default).
func derivedTLSProxyURL(httpURL string) (string, bool) {
	parsed, err := url.Parse(httpURL)
	if err != nil || parsed.Host == "" {
		return "", false
	}
	host := parsed.Hostname()
	port := parsed.Port()
	if port == "" {
		return "", false
	}
	tlsPort := "8443"
	if port != "8083" {
		// Per-test fakegenesys binds a random API port but the TLS port
		// stays at 8443 (single shared MITM port — only one per-test
		// instance can hold it at a time; the smoke harness runs tests
		// serially).
		if envTLS := strings.TrimSpace(os.Getenv("FAKEGENESYS_TLS_PORT")); envTLS != "" {
			tlsPort = envTLS
		}
	}
	return "http://" + host + ":" + tlsPort, true
}

// writeCACertToTempFile fetches /mock/ca-cert from the per-test
// fakegenesys, writes the PEM to a tempfile, and returns the path.
// The tempfile lives in t.TempDir() so it's auto-cleaned.
func writeCACertToTempFile(t *testing.T, fakegenesysURL string) (string, bool) {
	t.Helper()
	httpClient := &http.Client{Timeout: 5 * time.Second}
	resp, err := httpClient.Get(strings.TrimRight(fakegenesysURL, "/") + "/mock/ca-cert")
	if err != nil {
		t.Logf("writeCACertToTempFile: GET /mock/ca-cert: %v", err)
		return "", false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Logf("writeCACertToTempFile: GET /mock/ca-cert: status %d", resp.StatusCode)
		return "", false
	}
	pem, err := io.ReadAll(resp.Body)
	if err != nil || len(pem) == 0 {
		t.Logf("writeCACertToTempFile: read body: %v (len=%d)", err, len(pem))
		return "", false
	}
	path := filepath.Join(t.TempDir(), "fakegenesys-ca.pem")
	if err := os.WriteFile(path, pem, 0o600); err != nil {
		t.Logf("writeCACertToTempFile: write %s: %v", path, err)
		return "", false
	}
	return path, true
}

// ----- per-tree contracts -----

func runWorkingExample(t *testing.T, dir, tree string, broken brokenIndex, _, _ string) {
	t.Helper()
	tofuInit(t, dir)
	tofuApply(t, dir, nil)
	key := tree + "/" + filepath.Base(dir)
	if _, isBroken := broken[key]; isBroken {
		// S112 finding #3: ratchet-only-tightens. If a known-broken
		// dir now passes plan-no-op, fail — the entry can be removed.
		if tofuPlanIsNoOp(t, dir, nil) {
			t.Fatalf("known_broken entry %q now passes idempotency — "+
				"congratulations, remove this entry from examples/known_broken.yaml", key)
		}
		t.Logf("known_broken: %s — drift expected, skipping strict assertion", key)
	} else {
		tofuPlanNoOp(t, dir, nil)
	}
	tofuDestroy(t, dir, nil)
}

// tofuPlanIsNoOp reports whether `tofu plan -detailed-exitcode` exits
// 0 (no diff). Used by the known_broken ratchet so a flapping → clean
// dir surfaces immediately.
func tofuPlanIsNoOp(t *testing.T, dir string, extraArgs []string) bool {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), commandBudget)
	defer cancel()
	args := append([]string{"plan", "-detailed-exitcode", "-input=false"}, extraArgs...)
	cmd := exec.CommandContext(ctx, "tofu", args...)
	cmd.Dir = dir
	err := cmd.Run()
	if err == nil {
		return true
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return ee.ExitCode() == 0
	}
	return false
}

func runMisconfiguredExample(t *testing.T, dir, _ string, _ brokenIndex, _, _ string) {
	t.Helper()
	expectedPath := filepath.Join(dir, "expected.txt")
	expectedString := ""
	if b, err := os.ReadFile(expectedPath); err == nil {
		expectedString = strings.TrimSpace(string(b))
	}

	tofuInit(t, dir)
	out, err := tofuApplyExpectingFailure(t, dir)
	if err == nil {
		t.Fatalf("misconfigured example: tofu apply UNEXPECTEDLY succeeded; expected failure")
	}
	if expectedString != "" && !strings.Contains(out, expectedString) {
		t.Fatalf("misconfigured example: tofu apply failed but output does not contain expected error %q\noutput:\n%s",
			expectedString, out)
	}
}

func runUpdatesExample(t *testing.T, dir, _ string, _ brokenIndex, _, _ string) {
	t.Helper()
	v1 := filepath.Join(dir, "v1.tfvars")
	v2 := filepath.Join(dir, "v2.tfvars")
	for _, p := range []string{v1, v2} {
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("updates example missing %s: %v", p, err)
		}
	}

	tofuInit(t, dir)
	tofuApply(t, dir, []string{"-var-file=" + v1})
	tofuPlanNoOp(t, dir, []string{"-var-file=" + v1})
	tofuApply(t, dir, []string{"-var-file=" + v2})
	tofuPlanNoOp(t, dir, []string{"-var-file=" + v2})
	tofuDestroy(t, dir, []string{"-var-file=" + v2})
}

// ----- fakegenesys spawn -----

func buildFakegenesysOnce(t *testing.T, root string) string {
	t.Helper()
	fakegenesysBinaryOnce.Do(func() {
		out := filepath.Join(os.TempDir(), "fakegenesys-smoke-"+strings.ReplaceAll(filepath.Base(root), "/", "_"))
		cmd := exec.Command("go", "build", "-o", out, "./cmd/fakegenesys")
		cmd.Dir = root
		if b, err := cmd.CombinedOutput(); err != nil {
			fakegenesysBinaryErr = fmt.Errorf("build fakegenesys: %v\n%s", err, b)
			return
		}
		fakegenesysBinaryPath = out
	})
	if fakegenesysBinaryErr != nil {
		t.Fatalf("buildFakegenesysOnce: %v", fakegenesysBinaryErr)
	}
	return fakegenesysBinaryPath
}

func spawnFakegenesys(t *testing.T, bin string) (int, func()) {
	t.Helper()
	port := freePort(t)
	cmd := exec.Command(bin, "-port", fmt.Sprint(port), "-db", ":memory:")
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	// FAKEGENESYS_UPLOAD_HOST tells the flow upload-job handler what
	// host:port to embed in its presignedUrl. Inherited from the parent
	// would be too late — by the time t.Setenv runs in
	// setupGenesysProviderEnv, this subprocess has already captured its
	// env. So we explicitly inject it on cmd.Env at spawn time.
	uploadHost := fmt.Sprintf("%s:%d", defaultFakegenesysHost, port)
	cmd.Env = append(os.Environ(), "FAKEGENESYS_UPLOAD_HOST="+uploadHost)
	if err := cmd.Start(); err != nil {
		t.Fatalf("spawn fakegenesys: %v", err)
	}
	if err := waitForBoot(port); err != nil {
		_ = cmd.Process.Kill()
		t.Fatalf("fakegenesys did not boot: %v", err)
	}
	stop := func() {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
	}
	return port, stop
}

func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("freePort: %v", err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

func waitForBoot(port int) error {
	url := fmt.Sprintf("http://127.0.0.1:%d/healthz", port)
	deadline := time.Now().Add(bootBudget)
	for time.Now().Before(deadline) {
		resp, err := http.Get(url)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	return fmt.Errorf("did not respond on %s within %s", url, bootBudget)
}

// copyExample copies dir → tempdir and rewrites localhost:8083 in
// providers.tf to the per-test fakegenesys URL.
func copyExample(t *testing.T, src, fakegenesysURL string) string {
	t.Helper()
	dst := t.TempDir()
	err := filepath.Walk(src, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, p)
		if err != nil {
			return err
		}
		out := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(out, 0o755)
		}
		raw, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		if filepath.Base(p) == "providers.tf" {
			raw = []byte(strings.ReplaceAll(string(raw),
				"http://localhost:8083", fakegenesysURL))
			raw = []byte(strings.ReplaceAll(string(raw),
				"http://127.0.0.1:8083", fakegenesysURL))
		}
		return os.WriteFile(out, raw, 0o600)
	})
	if err != nil {
		t.Fatalf("copyExample: %v", err)
	}
	return dst
}

// ----- gates + helpers -----

func requireE2EGate(t *testing.T) {
	t.Helper()
	if os.Getenv(gateEnvVar) != "1" {
		t.Skipf("smoke harness gated by %s=1", gateEnvVar)
	}
}

func requireTofu(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("tofu"); err != nil {
		t.Skipf("tofu not on PATH: %v", err)
	}
}

func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatalf("runtime.Caller: cannot resolve repo root")
	}
	return filepath.Dir(filepath.Dir(file))
}

func loadKnownBroken(t *testing.T, root string) brokenIndex {
	t.Helper()
	p := filepath.Join(root, "examples", knownBrokenFileName)
	raw, err := os.ReadFile(p)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return brokenIndex{}
		}
		t.Fatalf("read %s: %v", p, err)
	}
	var bl brokenList
	if err := yaml.Unmarshal(raw, &bl); err != nil {
		t.Fatalf("parse %s: %v", p, err)
	}
	idx := make(brokenIndex, len(bl.Entries))
	for _, e := range bl.Entries {
		idx[e.Dir] = e
	}
	return idx
}

// ----- tofu wrappers -----

func tofuInit(t *testing.T, dir string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), commandBudget)
	defer cancel()
	cmd := exec.CommandContext(ctx, "tofu", "init", "-input=false")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("tofu init: %v\n%s", err, out)
	}
}

func tofuApply(t *testing.T, dir string, extraArgs []string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), commandBudget)
	defer cancel()
	args := append([]string{"apply", "-auto-approve", "-input=false"}, extraArgs...)
	cmd := exec.CommandContext(ctx, "tofu", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("tofu apply: %v\n%s", err, out)
	}
}

func tofuApplyExpectingFailure(t *testing.T, dir string) (string, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), commandBudget)
	defer cancel()
	cmd := exec.CommandContext(ctx, "tofu", "apply", "-auto-approve", "-input=false")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func tofuPlanNoOp(t *testing.T, dir string, extraArgs []string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), commandBudget)
	defer cancel()
	args := append([]string{"plan", "-detailed-exitcode", "-input=false"}, extraArgs...)
	cmd := exec.CommandContext(ctx, "tofu", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		var ee *exec.ExitError
		if !errors.As(err, &ee) || ee.ExitCode() != 0 {
			t.Fatalf("tofu plan -detailed-exitcode (expected exit 0 = no diff): %v\n%s", err, out)
		}
	}
}

func tofuDestroy(t *testing.T, dir string, extraArgs []string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), commandBudget)
	defer cancel()
	args := append([]string{"destroy", "-auto-approve", "-input=false"}, extraArgs...)
	cmd := exec.CommandContext(ctx, "tofu", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("tofu destroy: %v\n%s", err, out)
	}
}

// drainPipe drains a pipe so the spawned fakegenesys doesn't deadlock
// on a full buffer. Reserved for future use.
func drainPipe(r io.Reader) { _, _ = io.Copy(io.Discard, r) }

var _ = drainPipe
