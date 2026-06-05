// Package examples — spec cross-reference test.
//
// The fidelity strategy (AGENTS.md § "Fidelity strategy") makes
// specs/genesys-openapi.json the primary source of truth for handler
// wire shapes. This test walks the handler route map (post-S109/110/111
// it covers all 15 resources) and asserts every implemented route exists
// in the spec.
//
// In S108 the route map exposed to this test is empty (the route map
// gets populated as resources land in S109+). The test is wired up
// from day one so the first added route is checked immediately.
package examples_test

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/redscaresu/fakegenesys/handlers"
)

// TestSpecCrossReference walks the live chi route tree on a fresh
// fakegenesys Application and asserts every implemented `/api/v2/...`
// route exists in `specs/genesys-openapi.json`.
//
// S112 finding #2 fixed the earlier silent-no-op: before this slice
// the test relied on a package-level `ImplementedRoutes` that nothing
// populated. Walking the router via chi.Walk means every PR that adds
// a route is automatically gated against the spec without per-handler
// registration glue.
//
// `/api/v2/flows/actions/*` action endpoints are excluded from the
// strict-match because Genesys exposes them as POST handlers that take
// the resource id as a query param — the spec entries don't have a
// distinct path per action verb in some Genesys versions. The check
// still surfaces every CRUD route.
func TestSpecCrossReference(t *testing.T) {
	root := repoRoot(t)
	specPath := filepath.Join(root, "specs", "genesys-openapi.json")
	raw, err := os.ReadFile(specPath)
	if err != nil {
		t.Skipf("spec not present at %s: %v (run `make specs-refresh`)", specPath, err)
	}
	var doc struct {
		Paths map[string]map[string]any `json:"paths"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("parse spec: %v", err)
	}
	if len(doc.Paths) == 0 {
		t.Fatalf("spec has no paths — file may be malformed")
	}

	app, err := handlers.NewApplication(":memory:", false)
	if err != nil {
		t.Fatalf("NewApplication: %v", err)
	}
	defer app.Close()

	missing := []string{}
	checked := 0
	err = chi.Walk(app.Router().(chi.Routes), func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		if !strings.HasPrefix(route, "/api/v2/") {
			return nil
		}
		// Skip the wildcard catch-all that exists only to anchor the
		// bearer middleware (`/api/v2/*` from RegisterRoutes).
		if strings.HasSuffix(route, "/*") {
			return nil
		}
		ops, ok := doc.Paths[route]
		if !ok {
			missing = append(missing, method+" "+route)
			return nil
		}
		if _, ok := ops[strings.ToLower(method)]; !ok {
			missing = append(missing, method+" "+route)
		}
		checked++
		return nil
	})
	if err != nil {
		t.Fatalf("chi.Walk: %v", err)
	}
	if checked == 0 {
		t.Fatalf("walked 0 /api/v2 routes — the test is non-functional")
	}
	if len(missing) > 0 {
		t.Errorf("%d implemented routes missing from spec:\n  %s",
			len(missing), strings.Join(missing, "\n  "))
	}
	t.Logf("spec cross-reference: %d routes checked", checked)
}
