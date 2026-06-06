package handlers_test

import (
	"net/http"
	"testing"
)

// S116c regression tests for the post-auth SDK probe endpoints.
//
// The Genesys Terraform provider's ConfigureProvider calls these
// endpoints immediately after token issuance. A missing field or wrong
// shape in any of them crashes the provider plugin with "Plugin did
// not respond" — by the time the user sees that error, the SDK has
// already passed through several layers of plugin/gRPC indirection
// and the proximate cause is hidden. These tests fence each endpoint
// at the wire-shape boundary so a regression surfaces here, not at
// `tofu plan` time.

func authedGet(t *testing.T, srvURL, path, tok string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, srvURL+path, nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	return resp
}

func TestOrganizationsMe_BasicShape(t *testing.T) {
	_, srv := newTestApp(t)
	tok := mintToken(t, srv)
	resp := authedGet(t, srv.URL, "/api/v2/organizations/me", tok)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var body map[string]any
	if err := decodeJSON(resp, &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	for _, k := range []string{"id", "name", "thirdPartyOrgName", "defaultLanguage", "defaultCountryCode", "domain", "version", "state", "selfUri"} {
		if _, ok := body[k]; !ok {
			t.Errorf("response missing %q field", k)
		}
	}
}

// TestAuthorizationProducts_HasTotalField is the load-bearing
// regression for the S116c crash. The Terraform provider's
// `getAuthorizationProducts` does:
//
//	products := make([]string, *productEntities.Total)
//
// at provider.go:224. A nil `Total` segfaults the plugin. Keep `total`
// present in the response forever.
func TestAuthorizationProducts_HasTotalField(t *testing.T) {
	_, srv := newTestApp(t)
	tok := mintToken(t, srv)
	resp := authedGet(t, srv.URL, "/api/v2/authorization/products", tok)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var body map[string]any
	if err := decodeJSON(resp, &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	total, ok := body["total"]
	if !ok {
		t.Fatalf("response missing 'total' (segfaults genesyscloud provider)")
	}
	totalF, ok := total.(float64)
	if !ok {
		t.Fatalf("'total' = %v (%T), want numeric", total, total)
	}
	entities, ok := body["entities"].([]any)
	if !ok {
		t.Fatalf("'entities' missing or not an array: %v", body["entities"])
	}
	if int(totalF) != len(entities) {
		t.Errorf("total=%d but len(entities)=%d (must match)", int(totalF), len(entities))
	}
}

func TestAuthorizationDivisions_Home(t *testing.T) {
	_, srv := newTestApp(t)
	tok := mintToken(t, srv)
	resp := authedGet(t, srv.URL, "/api/v2/authorization/divisions/home", tok)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var body map[string]any
	if err := decodeJSON(resp, &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if v, _ := body["homeDivision"].(bool); !v {
		t.Errorf("homeDivision = %v, want true", body["homeDivision"])
	}
	if _, ok := body["id"].(string); !ok {
		t.Errorf("id missing or not a string")
	}
}

func TestTokensMe(t *testing.T) {
	_, srv := newTestApp(t)
	tok := mintToken(t, srv)
	resp := authedGet(t, srv.URL, "/api/v2/tokens/me", tok)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
}
