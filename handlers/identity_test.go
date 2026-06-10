package handlers_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/redscaresu/fakegenesys/testutil"
)

// Helper: stand up a fakegenesys test server with bearer token wired.
func newIdentitySrv(t *testing.T) *testutil.TestServer {
	t.Helper()
	return testutil.NewTestServer(t)
}

// --- users -----------------------------------------------------------

func TestUser_Lifecycle(t *testing.T) {
	ts := newIdentitySrv(t)

	var created map[string]any
	resp := ts.PostJSON(t, "/api/v2/users",
		map[string]any{"name": "Alice", "email": "alice@example.com"},
		&created)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create: status %d", resp.StatusCode)
	}
	id, _ := created["id"].(string)
	if id == "" {
		t.Fatalf("create: missing id")
	}

	var got map[string]any
	resp = ts.GetJSON(t, "/api/v2/users/"+id, &got)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get: status %d", resp.StatusCode)
	}
	if got["email"] != "alice@example.com" {
		t.Fatalf("email round-trip: %v", got["email"])
	}

	var updated map[string]any
	resp = ts.PatchJSON(t, "/api/v2/users/"+id,
		map[string]any{"title": "Engineer"}, &updated)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("update: status %d", resp.StatusCode)
	}
	if updated["title"] != "Engineer" {
		t.Fatalf("title not patched: %v", updated["title"])
	}

	resp = ts.DeleteJSON(t, "/api/v2/users/"+id)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("delete: status %d", resp.StatusCode)
	}

	// Soft delete: subsequent GET returns 200 with state=deleted.
	var after map[string]any
	resp = ts.GetJSON(t, "/api/v2/users/"+id, &after)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get-after-delete: status %d (want 200 for soft-delete)", resp.StatusCode)
	}
	if after["state"] != "deleted" {
		t.Fatalf("state after delete: %v (want deleted)", after["state"])
	}
}

func TestUser_DuplicateEmailConflict(t *testing.T) {
	ts := newIdentitySrv(t)
	ts.PostJSON(t, "/api/v2/users",
		map[string]any{"name": "A", "email": "dup@example.com"}, nil)
	resp := ts.PostJSON(t, "/api/v2/users",
		map[string]any{"name": "B", "email": "dup@example.com"}, nil)
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("status = %d, want 409", resp.StatusCode)
	}
}

func TestUser_MissingRequiredFields_400(t *testing.T) {
	ts := newIdentitySrv(t)
	resp := ts.PostJSON(t, "/api/v2/users",
		map[string]any{"name": "no-email"}, nil)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

func TestUser_Get404(t *testing.T) {
	ts := newIdentitySrv(t)
	resp := ts.GetJSON(t, "/api/v2/users/does-not-exist", nil)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
}

func TestUser_ListPagination(t *testing.T) {
	ts := newIdentitySrv(t)
	for i := 0; i < 30; i++ {
		ts.PostJSON(t, "/api/v2/users",
			map[string]any{
				"name":  "u" + iToStr(i),
				"email": "u" + iToStr(i) + "@example.com",
			}, nil)
	}
	var listing struct {
		Entities   []map[string]any `json:"entities"`
		PageCount  int              `json:"pageCount"`
		PageNumber int              `json:"pageNumber"`
		PageSize   int              `json:"pageSize"`
		Total      int              `json:"total"`
	}
	resp := ts.GetJSON(t, "/api/v2/users?pageNumber=1&pageSize=10", &listing)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if listing.Total != 30 {
		t.Fatalf("total = %d, want 30", listing.Total)
	}
	if listing.PageSize != 10 {
		t.Fatalf("pageSize = %d, want 10", listing.PageSize)
	}
	if len(listing.Entities) != 10 {
		t.Fatalf("entities = %d, want 10", len(listing.Entities))
	}
	if listing.PageCount != 3 {
		t.Fatalf("pageCount = %d, want 3", listing.PageCount)
	}
}

// --- groups ----------------------------------------------------------

func TestGroup_Lifecycle(t *testing.T) {
	ts := newIdentitySrv(t)
	var created map[string]any
	resp := ts.PostJSON(t, "/api/v2/groups",
		map[string]any{"name": "Engineering", "type": "official"},
		&created)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create: status %d", resp.StatusCode)
	}
	id, _ := created["id"].(string)

	resp = ts.PutJSON(t, "/api/v2/groups/"+id,
		map[string]any{"description": "Eng group"}, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("update: status %d", resp.StatusCode)
	}

	resp = ts.DeleteJSON(t, "/api/v2/groups/"+id)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("delete: status %d", resp.StatusCode)
	}
	resp = ts.GetJSON(t, "/api/v2/groups/"+id, nil)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("get-after-delete: status %d, want 404", resp.StatusCode)
	}
}

// --- locations -------------------------------------------------------

func TestLocation_Lifecycle(t *testing.T) {
	ts := newIdentitySrv(t)
	var created map[string]any
	resp := ts.PostJSON(t, "/api/v2/locations",
		map[string]any{"name": "HQ"}, &created)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create: status %d", resp.StatusCode)
	}
	id, _ := created["id"].(string)

	resp = ts.PatchJSON(t, "/api/v2/locations/"+id,
		map[string]any{"notes": "main office"}, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("update: status %d", resp.StatusCode)
	}
	resp = ts.DeleteJSON(t, "/api/v2/locations/"+id)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("delete: status %d", resp.StatusCode)
	}
}

// --- auth roles ------------------------------------------------------

func TestAuthRole_Lifecycle(t *testing.T) {
	ts := newIdentitySrv(t)
	var created map[string]any
	resp := ts.PostJSON(t, "/api/v2/authorization/roles",
		map[string]any{
			"name":        "queue-supervisor",
			"permissions": []any{"routing:queue:edit", "routing:queue:view"},
		}, &created)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create: status %d", resp.StatusCode)
	}
	id, _ := created["id"].(string)

	resp = ts.PutJSON(t, "/api/v2/authorization/roles/"+id,
		map[string]any{
			"name":        "queue-supervisor",
			"permissions": []any{"routing:queue:edit"},
		}, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("update: status %d", resp.StatusCode)
	}

	resp = ts.DeleteJSON(t, "/api/v2/authorization/roles/"+id)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("delete: status %d", resp.StatusCode)
	}
}

func TestAuthRole_DuplicateNameConflict(t *testing.T) {
	ts := newIdentitySrv(t)
	ts.PostJSON(t, "/api/v2/authorization/roles",
		map[string]any{"name": "dup", "permissions": []any{"a"}}, nil)
	resp := ts.PostJSON(t, "/api/v2/authorization/roles",
		map[string]any{"name": "dup", "permissions": []any{"a"}}, nil)
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("status = %d, want 409", resp.StatusCode)
	}
}

// --- oauth clients ---------------------------------------------------

func TestOAuthClient_RevealOnceSecret(t *testing.T) {
	ts := newIdentitySrv(t)
	var created map[string]any
	resp := ts.PostJSON(t, "/api/v2/oauth/clients",
		map[string]any{
			"name":                "integration-bot",
			"authorizedGrantType": "CLIENT_CREDENTIALS",
		}, &created)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create: status %d", resp.StatusCode)
	}
	if s, _ := created["secret"].(string); s == "" {
		t.Fatalf("create response missing secret (reveal-once)")
	}
	id, _ := created["id"].(string)

	var fetched map[string]any
	resp = ts.GetJSON(t, "/api/v2/oauth/clients/"+id, &fetched)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get: status %d", resp.StatusCode)
	}
	if s, ok := fetched["secret"]; ok && s != nil {
		t.Fatalf("get response leaked secret: %v (reveal-once violated)", s)
	}

	resp = ts.DeleteJSON(t, "/api/v2/oauth/clients/"+id)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("delete: status %d", resp.StatusCode)
	}
}

// --- helpers ---------------------------------------------------------

func iToStr(i int) string {
	if i < 10 {
		return string(rune('0' + i))
	}
	return string(rune('0'+(i/10))) + string(rune('0'+(i%10)))
}

// --- S123 contract tests ---------------------------------------------
//
// Each TestContract_<id> below pairs with a CRITICAL[<id>]: docstring
// in the matching handler. See docs/contract-matrix-s123.md and the
// S127 contract audit for the enforcement.

// TestContract_users_create_default_division asserts the wire-shape
// invariant that POST /api/v2/users with no `division` field still
// round-trips a non-empty `division.id` on the read-after-create.
// Without it, the genesyscloud provider's `readUser` dereferences
// *currentUser.Division.Id at resource_genesyscloud_user.go:166 and
// segfaults the plugin.
//
// Paired with CRITICAL[users-create-default-division] in handlers/user.go.
func TestContract_users_create_default_division(t *testing.T) {
	ts := newIdentitySrv(t)
	var created map[string]any
	// Deliberately omit "division" from the body.
	resp := ts.PostJSON(t, "/api/v2/users",
		map[string]any{"name": "Defaulted", "email": "defaulted@example.com"},
		&created)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create: status %d", resp.StatusCode)
	}
	assertNonEmptyDivisionID(t, created, "create response")

	id, _ := created["id"].(string)
	var got map[string]any
	resp = ts.GetJSON(t, "/api/v2/users/"+id, &got)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("read-after-create: status %d", resp.StatusCode)
	}
	assertNonEmptyDivisionID(t, got, "read-after-create response")
}

// TestContract_users_search_results_key asserts POST /api/v2/users/search
// uses the paged-list key `results` (NOT `entities`). The provider's
// getDeletedUserId reads Usersearchresponse.Results; if we returned
// the more-common `entities` key, the soft-delete verification matches
// zero users and the destroy retry-loops to timeout.
//
// We inspect raw bytes because Go's json.Unmarshal is case-insensitive
// AND will silently let an unexpected key slide if the target struct
// uses `Results`. Raw inspection prevents both blind spots.
//
// Paired with CRITICAL[users-search-results-key] in handlers/user_subresources.go.
func TestContract_users_search_results_key(t *testing.T) {
	ts := newIdentitySrv(t)
	// Create + soft-delete a user so the search has something to match.
	var created map[string]any
	ts.PostJSON(t, "/api/v2/users",
		map[string]any{"name": "Ghost", "email": "ghost@example.com"},
		&created)
	id, _ := created["id"].(string)
	ts.DeleteJSON(t, "/api/v2/users/"+id)

	resp, raw := ts.DoRaw(t, http.MethodPost, "/api/v2/users/search",
		mustJSON(t, map[string]any{
			"query": []any{
				map[string]any{
					"type":   "EXACT",
					"fields": []any{"email"},
					"value":  "ghost@example.com",
				},
				map[string]any{
					"type":   "EXACT",
					"fields": []any{"state"},
					"value":  "deleted",
				},
			},
		}),
		http.Header{"Content-Type": []string{"application/json"}})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("search: status %d; body: %s", resp.StatusCode, raw)
	}
	if !bytesContains(raw, `"results"`) {
		t.Fatalf("search response missing literal 'results' key (provider reads Usersearchresponse.Results); body: %s", raw)
	}
	if bytesContains(raw, `"entities"`) {
		t.Fatalf("search response uses 'entities' instead of 'results' (provider will return zero matches); body: %s", raw)
	}
	// Also verify it decoded as expected.
	for _, key := range []string{"results", "total", "pageCount", "pageNumber", "pageSize"} {
		if !bytesContains(raw, `"`+key+`"`) {
			t.Errorf("search response missing %q key", key)
		}
	}
}

// TestContract_user_roles_get_version_and_roles asserts GET
// /api/v2/users/{userId}/roles returns a Userauthorization body with
// a non-nil `version` and a non-nil `roles` array. The provider's
// genesyscloud_user_roles flatten path iterates `roles`; a missing
// or null `roles` field panics during the diff step.
//
// Paired with CRITICAL[user-roles-get-version-and-roles] in
// handlers/user_subresources.go.
func TestContract_user_roles_get_version_and_roles(t *testing.T) {
	ts := newIdentitySrv(t)
	var body map[string]any
	resp := ts.GetJSON(t, "/api/v2/users/any-user-id/roles", &body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d", resp.StatusCode)
	}
	if _, ok := body["version"]; !ok {
		t.Fatalf("response missing 'version' (provider reads Userauthorization.Version)")
	}
	roles, present := body["roles"]
	if !present {
		t.Fatalf("response missing 'roles' (provider iterates Userauthorization.Roles)")
	}
	if roles == nil {
		t.Fatalf("response 'roles' is JSON null — must be array (provider iterates)")
	}
	if _, ok := roles.([]any); !ok {
		t.Fatalf("response 'roles' is not an array: %T", roles)
	}
}

// TestContract_user_roles_put_echoes_ids asserts PUT
// /api/v2/users/{userId}/roles with a JSON array of role-id strings
// echoes each id back in the response `roles[].id` shape with selfUri.
//
// Paired with CRITICAL[user-roles-put-echoes-ids] in
// handlers/user_subresources.go.
func TestContract_user_roles_put_echoes_ids(t *testing.T) {
	ts := newIdentitySrv(t)
	var body map[string]any
	resp := ts.PutJSON(t, "/api/v2/users/u1/roles",
		[]any{"role-a", "role-b"}, &body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d", resp.StatusCode)
	}
	roles, ok := body["roles"].([]any)
	if !ok {
		t.Fatalf("response 'roles' missing or not an array: %T", body["roles"])
	}
	if len(roles) != 2 {
		t.Fatalf("roles length = %d, want 2", len(roles))
	}
	want := []string{"role-a", "role-b"}
	for i, r := range roles {
		entry, _ := r.(map[string]any)
		id, _ := entry["id"].(string)
		if id != want[i] {
			t.Errorf("roles[%d].id = %q, want %q", i, id, want[i])
		}
		if uri, _ := entry["selfUri"].(string); uri == "" {
			t.Errorf("roles[%d].selfUri is empty", i)
		}
	}
}

// TestContract_user_password_204 asserts POST
// /api/v2/users/{userId}/password returns 204 for any JSON body.
// Real Genesys validates against the org's password policy; fakegenesys
// accepts any payload — the provider gates on the status code only.
//
// Paired with CRITICAL[user-password-204] in handlers/user_subresources.go.
func TestContract_user_password_204(t *testing.T) {
	ts := newIdentitySrv(t)
	resp, raw := ts.DoRaw(t, http.MethodPost, "/api/v2/users/u1/password",
		mustJSON(t, map[string]any{"newPassword": "x"}),
		http.Header{"Content-Type": []string{"application/json"}})
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("status = %d, want 204; body: %s", resp.StatusCode, raw)
	}
	if len(raw) > 0 {
		t.Errorf("204 response must be empty; got %q", raw)
	}
}

// TestContract_group_members_individuals_round_trip asserts the
// member-list round-trip the provider's readGroup polls until
// convergence: POST /groups/{id}/members → entities length matches;
// DELETE /groups/{id}/members?id=a,b → entities length drops to only
// the un-deleted users.
//
// Paired with CRITICAL[group-members-individuals-round-trip] in
// handlers/group.go.
func TestContract_group_members_individuals_round_trip(t *testing.T) {
	ts := newIdentitySrv(t)
	var created map[string]any
	resp := ts.PostJSON(t, "/api/v2/groups",
		map[string]any{"name": "Round-trip", "type": "official"}, &created)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create group: status %d", resp.StatusCode)
	}
	gid, _ := created["id"].(string)

	resp = ts.PostJSON(t, "/api/v2/groups/"+gid+"/members",
		map[string]any{"memberIds": []any{"u1", "u2", "u3"}}, nil)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("members add: status %d", resp.StatusCode)
	}

	var listing map[string]any
	resp = ts.GetJSON(t, "/api/v2/groups/"+gid+"/individuals", &listing)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("individuals list: status %d", resp.StatusCode)
	}
	entities, _ := listing["entities"].([]any)
	if len(entities) != 3 {
		t.Fatalf("entities count after add = %d, want 3", len(entities))
	}
	got := make(map[string]bool)
	for _, e := range entities {
		m, _ := e.(map[string]any)
		if id, _ := m["id"].(string); id != "" {
			got[id] = true
		}
	}
	for _, want := range []string{"u1", "u2", "u3"} {
		if !got[want] {
			t.Errorf("individuals missing %q after add; got %v", want, got)
		}
	}

	// DELETE removes only u1, u2 — leaves u3.
	resp = ts.DeleteJSON(t, "/api/v2/groups/"+gid+"/members?id=u1,u2")
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("members delete: status %d", resp.StatusCode)
	}
	var after map[string]any
	resp = ts.GetJSON(t, "/api/v2/groups/"+gid+"/individuals", &after)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("individuals list after delete: status %d", resp.StatusCode)
	}
	remaining, _ := after["entities"].([]any)
	if len(remaining) != 1 {
		t.Fatalf("entities after delete = %d, want 1", len(remaining))
	}
	last, _ := remaining[0].(map[string]any)
	if id, _ := last["id"].(string); id != "u3" {
		t.Errorf("remaining entity id = %q, want %q", id, "u3")
	}
}

// TestContract_group_voicemail_dual_paths asserts both the legacy
// /groups/{id}/voicemail path AND the modern
// /voicemail/groups/{id}/policy path respond to GET and PATCH. The
// provider's updateGroupVoicemailPolicy tries both URLs; a 501 on
// either is the failure mode this contract prevents.
//
// Paired with CRITICAL[group-voicemail-dual-paths] in handlers/group.go.
func TestContract_group_voicemail_dual_paths(t *testing.T) {
	ts := newIdentitySrv(t)
	for _, path := range []string{
		"/api/v2/groups/g1/voicemail",
		"/api/v2/voicemail/groups/g1/policy",
	} {
		t.Run(path, func(t *testing.T) {
			var body map[string]any
			resp := ts.GetJSON(t, path, &body)
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("GET %s: status %d (must not 501)", path, resp.StatusCode)
			}
			if _, ok := body["alertTimeoutSeconds"]; !ok {
				t.Errorf("GET %s: response missing alertTimeoutSeconds", path)
			}

			var patched map[string]any
			resp = ts.PatchJSON(t, path,
				map[string]any{"alertTimeoutSeconds": 45}, &patched)
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("PATCH %s: status %d (must not 501)", path, resp.StatusCode)
			}
			if v, _ := patched["alertTimeoutSeconds"].(float64); int(v) != 45 {
				t.Errorf("PATCH %s: alertTimeoutSeconds = %v, want 45", path, patched["alertTimeoutSeconds"])
			}
		})
	}
}

// --- contract-test helpers ------------------------------------------

func assertNonEmptyDivisionID(t *testing.T, body map[string]any, label string) {
	t.Helper()
	div, ok := body["division"].(map[string]any)
	if !ok || div == nil {
		t.Fatalf("%s: missing 'division' object (provider derefs *Division.Id)", label)
	}
	id, _ := div["id"].(string)
	if id == "" {
		t.Fatalf("%s: division.id is empty (segfaults the SDK)", label)
	}
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return raw
}

func bytesContains(b []byte, s string) bool { return bytes.Contains(b, []byte(s)) }
