package handlers_test

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAllContractsHaveTests is the durable, CI-enforced enforcement of
// the CRITICAL[<id>]: / TestContract_<id> convention introduced in S123.
//
// Goal: a wire-shape invariant the consuming Terraform provider depends
// on must NOT live as a comment alone. Every CRITICAL[<id>]: or
// MUST[<id>]: docstring in handlers/*.go MUST have a paired
// TestContract_<id> in the same package (kebab-case → snake_case), and
// every TestContract_<id> MUST have at least one source [<id>] tag.
// Drift becomes a failed `go test`, not a missed code review.
//
// Adding a contract:
//
//  1. Add `// CRITICAL[<kebab-case-id>]: <invariant + why it matters>`
//     above the handler (or `MUST[<id>]:` if the constraint is in a
//     specific code path rather than the function preamble).
//  2. Add `func TestContract_<id_with_underscores>(t *testing.T)` to a
//     test file in this package. The test must assert the invariant
//     (revert the fix → test fails).
//  3. Append a row to docs/contract-matrix-s123.md.
//
// This convention rolls out across mockway, fakegcp, and fakeaws in
// S127's cross-repo sweep. See feedback_oss_mature_day_one.md item 14.
//
// The audit is empty-contracts-safe: a package with zero CRITICAL[id]
// docstrings AND zero TestContract_ tests passes trivially. This lets
// sibling fakes adopt the file before they've fully swept their
// existing CRITICAL: notes — the file is permission-to-use without
// imposing immediate inventory.

const (
	handlersGlob  = "*.go"      // non-test source: handler*.go
	testFileGlob  = "*_test.go" // test files
	contractIDRe  = `(?:CRITICAL|MUST)\[([a-z0-9][a-z0-9-]*)\]`
	testFuncRe    = `func\s+(TestContract_[A-Za-z0-9_]+)\s*\(`
	contractIDFmt = "CRITICAL[%s]: or MUST[%s]:"
)

var (
	contractRe = regexp.MustCompile(contractIDRe)
	testRe     = regexp.MustCompile(testFuncRe)
)

func TestAllContractsHaveTests(t *testing.T) {
	dir, err := os.Getwd()
	require.NoError(t, err, "getwd")

	sourceIDs, sourceLocs := scanContractIDs(t, dir)
	testIDs, testLocs := scanTestContractFuncs(t, dir)

	// Both empty → pass (sibling-adoption-safe).
	if len(sourceIDs) == 0 && len(testIDs) == 0 {
		t.Log("contract audit: zero contracts in this package — pass (empty-state is the bootstrap case)")
		return
	}

	missingTests := setDiff(sourceIDs, testIDs)
	orphanTests := setDiff(testIDs, sourceIDs)

	if len(missingTests) == 0 && len(orphanTests) == 0 {
		t.Logf("contract audit: %d contracts, all paired with tests", len(sourceIDs))
		return
	}

	if len(missingTests) > 0 {
		t.Errorf("contract audit: %d contract docstring(s) lack a paired TestContract_<id>:", len(missingTests))
		for _, id := range sortedKeys(missingTests) {
			t.Errorf("  - %s\n    declared at: %s\n    expected test: func TestContract_%s(t *testing.T)",
				fmt.Sprintf("CRITICAL[%s] / MUST[%s]", id, id),
				strings.Join(sourceLocs[id], ", "),
				kebabToSnake(id))
		}
	}
	if len(orphanTests) > 0 {
		t.Errorf("contract audit: %d test(s) lack a paired CRITICAL[<id>]: or MUST[<id>]: docstring:", len(orphanTests))
		for _, id := range sortedKeys(orphanTests) {
			t.Errorf("  - test: TestContract_%s\n    declared at: %s\n    expected docstring: %s in a handler",
				kebabToSnake(id),
				strings.Join(testLocs[id], ", "),
				fmt.Sprintf(contractIDFmt, id, id))
		}
	}

	t.Log("To fix: add the missing test(s) AND/OR docstring(s), OR demote the orphan to a plain comment.")
}

// scanContractIDs walks dir/*.go (excluding *_test.go) and extracts
// the set of contract IDs from CRITICAL[<id>]: / MUST[<id>]: tokens.
// Returns the ID set plus a map of id → []file:line locations.
func scanContractIDs(t *testing.T, dir string) (map[string]struct{}, map[string][]string) {
	t.Helper()
	files := globGo(t, dir, handlersGlob, true)
	ids := map[string]struct{}{}
	locs := map[string][]string{}
	for _, path := range files {
		raw, err := os.ReadFile(path)
		require.NoError(t, err, "read %s", path)
		base := filepath.Base(path)
		for line, text := range strings.Split(string(raw), "\n") {
			for _, m := range contractRe.FindAllStringSubmatch(text, -1) {
				id := m[1]
				ids[id] = struct{}{}
				locs[id] = append(locs[id], fmt.Sprintf("%s:%d", base, line+1))
			}
		}
	}
	return ids, locs
}

// scanTestContractFuncs walks dir/*_test.go and extracts the set of
// contract IDs (kebab-case form) from TestContract_<id_with_underscores>
// function declarations.
func scanTestContractFuncs(t *testing.T, dir string) (map[string]struct{}, map[string][]string) {
	t.Helper()
	files := globGo(t, dir, testFileGlob, false)
	ids := map[string]struct{}{}
	locs := map[string][]string{}
	for _, path := range files {
		raw, err := os.ReadFile(path)
		require.NoError(t, err, "read %s", path)
		base := filepath.Base(path)
		for line, text := range strings.Split(string(raw), "\n") {
			for _, m := range testRe.FindAllStringSubmatch(text, -1) {
				funcName := m[1]
				kebab := snakeToKebab(strings.TrimPrefix(funcName, "TestContract_"))
				ids[kebab] = struct{}{}
				locs[kebab] = append(locs[kebab], fmt.Sprintf("%s:%d", base, line+1))
			}
		}
	}
	return ids, locs
}

// globGo returns all .go files in dir matching the given suffix-glob,
// filtering to either non-test source (excludeTest=true) or test files
// only (excludeTest=false). The audit's own machinery files are
// excluded from BOTH scans — their fixture strings contain literal
// "CRITICAL[foo-bar]:" and "func TestContract_foo_bar(" that would
// otherwise be parsed as real contract declarations.
func globGo(t *testing.T, dir, pattern string, excludeTest bool) []string {
	t.Helper()
	all, err := filepath.Glob(filepath.Join(dir, pattern))
	require.NoError(t, err, "glob %q", pattern)
	out := make([]string, 0, len(all))
	for _, p := range all {
		base := filepath.Base(p)
		if isAuditMachineryFile(base) {
			continue
		}
		isTest := strings.HasSuffix(base, "_test.go")
		if excludeTest && isTest {
			continue
		}
		if !excludeTest && !isTest {
			continue
		}
		out = append(out, p)
	}
	return out
}

// isAuditMachineryFile reports whether the file name belongs to the
// contract-audit infrastructure itself. Those files contain fixture
// strings (e.g. `func TestContract_foo_bar(`) that would otherwise be
// double-counted as real contract declarations.
func isAuditMachineryFile(base string) bool {
	return base == "contract_audit_test.go"
}

// setDiff returns the elements of a not in b.
func setDiff(a, b map[string]struct{}) map[string]struct{} {
	out := map[string]struct{}{}
	for k := range a {
		if _, ok := b[k]; !ok {
			out[k] = struct{}{}
		}
	}
	return out
}

// sortedKeys returns the keys of m in lexicographic order for stable
// test output.
func sortedKeys(m map[string]struct{}) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func kebabToSnake(s string) string { return strings.ReplaceAll(s, "-", "_") }
func snakeToKebab(s string) string { return strings.ReplaceAll(s, "_", "-") }

// TestContractAuditTest_Self validates the audit's own logic with
// known-good and known-bad fixtures. Without this, a bug in the audit
// could silently let real contracts drift.
func TestContractAuditTest_Self(t *testing.T) {
	tmp := t.TempDir()

	// Known-good: matching CRITICAL[id] + TestContract_id.
	mustWrite(t, filepath.Join(tmp, "src.go"),
		"package x\n// CRITICAL[foo-bar]: invariant\nfunc handle() {}\n")
	mustWrite(t, filepath.Join(tmp, "src_test.go"),
		"package x\nimport \"testing\"\nfunc TestContract_foo_bar(t *testing.T) {}\n")

	src, _ := scanContractIDsAt(t, tmp)
	tst, _ := scanTestContractFuncsAt(t, tmp)
	assert.Empty(t, setDiff(src, tst), "known-good: setDiff(src, tst)")
	assert.Empty(t, setDiff(tst, src), "known-good: setDiff(tst, src)")

	// Known-bad-missing-test: CRITICAL[id] but no test.
	tmp2 := t.TempDir()
	mustWrite(t, filepath.Join(tmp2, "src.go"),
		"package x\n// CRITICAL[orphan-doc]: invariant\nfunc handle() {}\n")
	src2, _ := scanContractIDsAt(t, tmp2)
	tst2, _ := scanTestContractFuncsAt(t, tmp2)
	assert.Len(t, setDiff(src2, tst2), 1, "known-bad-missing-test: setDiff")

	// Known-bad-orphan-test: TestContract_id but no docstring.
	tmp3 := t.TempDir()
	mustWrite(t, filepath.Join(tmp3, "src_test.go"),
		"package x\nimport \"testing\"\nfunc TestContract_orphan_test(t *testing.T) {}\n")
	src3, _ := scanContractIDsAt(t, tmp3)
	tst3, _ := scanTestContractFuncsAt(t, tmp3)
	assert.Len(t, setDiff(tst3, src3), 1, "known-bad-orphan-test: setDiff(tst, src)")

	// Sanity: kebab/snake conversion is symmetric.
	assert.Equal(t, "a-b-c-d", snakeToKebab(kebabToSnake("a-b-c-d")), "kebab/snake round-trip")
}

// scanContractIDsAt / scanTestContractFuncsAt are dir-overrideable
// wrappers used by the self-test fixture. The production versions
// in this file derive dir from os.Getwd() which would always be the
// handlers package directory — useless for fixture testing.
func scanContractIDsAt(t *testing.T, dir string) (map[string]struct{}, map[string][]string) {
	t.Helper()
	return scanContractIDs(t, dir)
}
func scanTestContractFuncsAt(t *testing.T, dir string) (map[string]struct{}, map[string][]string) {
	t.Helper()
	return scanTestContractFuncs(t, dir)
}

func mustWrite(t *testing.T, path, contents string) {
	t.Helper()
	require.NoError(t, os.WriteFile(path, []byte(contents), 0o644), "write %s", path)
}
