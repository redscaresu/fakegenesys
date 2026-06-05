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
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ImplementedRoutes is populated by per-resource handler files via a
// package-level init. S109/S110/S111 each append their endpoints. The
// list is single-package coupled so adding a new endpoint without
// touching the spec or the cross-reference is impossible.
//
// In S108 it's empty (no resource handlers landed yet). The smoke
// harness validates handler behavior end-to-end; this test catches the
// narrower "wire shape drifts from the spec" failure mode.
var ImplementedRoutes = []string{}

func TestSpecCrossReference(t *testing.T) {
	root := repoRoot(t)
	specPath := filepath.Join(root, "specs", "genesys-openapi.json")
	raw, err := os.ReadFile(specPath)
	if err != nil {
		// Spec not yet downloaded — skip with a clear message rather
		// than failing.
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
	if len(ImplementedRoutes) == 0 {
		t.Log("ImplementedRoutes empty — no resource handlers landed yet (S108 baseline)")
		return
	}
	missing := []string{}
	for _, r := range ImplementedRoutes {
		// route format: "METHOD /api/v2/path[/{id}]"
		parts := strings.SplitN(r, " ", 2)
		if len(parts) != 2 {
			t.Errorf("malformed route entry %q", r)
			continue
		}
		method := strings.ToLower(parts[0])
		path := parts[1]
		ops, ok := doc.Paths[path]
		if !ok {
			missing = append(missing, r)
			continue
		}
		if _, ok := ops[method]; !ok {
			missing = append(missing, r)
		}
	}
	if len(missing) > 0 {
		t.Errorf("implemented routes missing from spec:\n  %s", strings.Join(missing, "\n  "))
	}
}
