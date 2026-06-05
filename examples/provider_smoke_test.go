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
			workDir := copyExample(t, dir, url)
			run(t, workDir, tree, broken, bin, url)
		})
	}
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
