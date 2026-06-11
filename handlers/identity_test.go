package handlers_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/redscaresu/fakegenesys/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
	require.Equal(t, http.StatusCreated, resp.StatusCode, "create")
	id, _ := created["id"].(string)
	require.NotEmpty(t, id, "create: missing id")

	var got map[string]any
	resp = ts.GetJSON(t, "/api/v2/users/"+id, &got)
	require.Equal(t, http.StatusOK, resp.StatusCode, "get")
	assert.Equal(t, "alice@example.com", got["email"], "email round-trip")

	var updated map[string]any
	resp = ts.PatchJSON(t, "/api/v2/users/"+id,
		map[string]any{"title": "Engineer"}, &updated)
	require.Equal(t, http.StatusOK, resp.StatusCode, "update")
	assert.Equal(t, "Engineer", updated["title"], "title not patched")

	resp = ts.DeleteJSON(t, "/api/v2/users/"+id)
	require.Equal(t, http.StatusNoContent, resp.StatusCode, "delete")

	// Soft delete: subsequent GET returns 200 with state=deleted.
	var after map[string]any
	resp = ts.GetJSON(t, "/api/v2/users/"+id, &after)
	require.Equal(t, http.StatusOK, resp.StatusCode, "get-after-delete (want 200 for soft-delete)")
	assert.Equal(t, "deleted", after["state"], "state after delete")
}

func TestUser_DuplicateEmailConflict(t *testing.T) {
	ts := newIdentitySrv(t)
	ts.PostJSON(t, "/api/v2/users",
		map[string]any{"name": "A", "email": "dup@example.com"}, nil)
	resp := ts.PostJSON(t, "/api/v2/users",
		map[string]any{"name": "B", "email": "dup@example.com"}, nil)
	assert.Equal(t, http.StatusConflict, resp.StatusCode)
}

func TestUser_MissingRequiredFields_400(t *testing.T) {
	ts := newIdentitySrv(t)
	resp := ts.PostJSON(t, "/api/v2/users",
		map[string]any{"name": "no-email"}, nil)
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestUser_Get404(t *testing.T) {
	ts := newIdentitySrv(t)
	resp := ts.GetJSON(t, "/api/v2/users/does-not-exist", nil)
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
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
	require.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, 30, listing.Total)
	assert.Equal(t, 10, listing.PageSize)
	assert.Len(t, listing.Entities, 10)
	assert.Equal(t, 3, listing.PageCount)
}

// --- groups ----------------------------------------------------------

func TestGroup_Lifecycle(t *testing.T) {
	ts := newIdentitySrv(t)
	var created map[string]any
	resp := ts.PostJSON(t, "/api/v2/groups",
		map[string]any{"name": "Engineering", "type": "official"},
		&created)
	require.Equal(t, http.StatusCreated, resp.StatusCode, "create")
	id, _ := created["id"].(string)

	resp = ts.PutJSON(t, "/api/v2/groups/"+id,
		map[string]any{"description": "Eng group"}, nil)
	assert.Equal(t, http.StatusOK, resp.StatusCode, "update")

	resp = ts.DeleteJSON(t, "/api/v2/groups/"+id)
	assert.Equal(t, http.StatusNoContent, resp.StatusCode, "delete")
	resp = ts.GetJSON(t, "/api/v2/groups/"+id, nil)
	assert.Equal(t, http.StatusNotFound, resp.StatusCode, "get-after-delete")
}

// --- locations -------------------------------------------------------

func TestLocation_Lifecycle(t *testing.T) {
	ts := newIdentitySrv(t)
	var created map[string]any
	resp := ts.PostJSON(t, "/api/v2/locations",
		map[string]any{"name": "HQ"}, &created)
	require.Equal(t, http.StatusCreated, resp.StatusCode, "create")
	id, _ := created["id"].(string)

	resp = ts.PatchJSON(t, "/api/v2/locations/"+id,
		map[string]any{"notes": "main office"}, nil)
	assert.Equal(t, http.StatusOK, resp.StatusCode, "update")
	resp = ts.DeleteJSON(t, "/api/v2/locations/"+id)
	assert.Equal(t, http.StatusNoContent, resp.StatusCode, "delete")
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
	require.Equal(t, http.StatusCreated, resp.StatusCode, "create")
	id, _ := created["id"].(string)

	resp = ts.PutJSON(t, "/api/v2/authorization/roles/"+id,
		map[string]any{
			"name":        "queue-supervisor",
			"permissions": []any{"routing:queue:edit"},
		}, nil)
	assert.Equal(t, http.StatusOK, resp.StatusCode, "update")

	resp = ts.DeleteJSON(t, "/api/v2/authorization/roles/"+id)
	assert.Equal(t, http.StatusNoContent, resp.StatusCode, "delete")
}

func TestAuthRole_DuplicateNameConflict(t *testing.T) {
	ts := newIdentitySrv(t)
	ts.PostJSON(t, "/api/v2/authorization/roles",
		map[string]any{"name": "dup", "permissions": []any{"a"}}, nil)
	resp := ts.PostJSON(t, "/api/v2/authorization/roles",
		map[string]any{"name": "dup", "permissions": []any{"a"}}, nil)
	assert.Equal(t, http.StatusConflict, resp.StatusCode)
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
	require.Equal(t, http.StatusCreated, resp.StatusCode, "create")
	secret, _ := created["secret"].(string)
	assert.NotEmpty(t, secret, "create response missing secret (reveal-once)")
	id, _ := created["id"].(string)

	var fetched map[string]any
	resp = ts.GetJSON(t, "/api/v2/oauth/clients/"+id, &fetched)
	require.Equal(t, http.StatusOK, resp.StatusCode, "get")
	if s, ok := fetched["secret"]; ok {
		assert.Nil(t, s, "get response leaked secret (reveal-once violated)")
	}

	resp = ts.DeleteJSON(t, "/api/v2/oauth/clients/"+id)
	assert.Equal(t, http.StatusNoContent, resp.StatusCode, "delete")
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
	require.Equal(t, http.StatusCreated, resp.StatusCode, "create")
	assertNonEmptyDivisionID(t, created, "create response")

	id, _ := created["id"].(string)
	var got map[string]any
	resp = ts.GetJSON(t, "/api/v2/users/"+id, &got)
	require.Equal(t, http.StatusOK, resp.StatusCode, "read-after-create")
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
	require.Equal(t, http.StatusOK, resp.StatusCode, "search; body: %s", raw)
	assert.True(t, bytesContains(raw, `"results"`),
		"search response missing literal 'results' key (provider reads Usersearchresponse.Results); body: %s", raw)
	assert.False(t, bytesContains(raw, `"entities"`),
		"search response uses 'entities' instead of 'results' (provider will return zero matches); body: %s", raw)
	// Also verify it decoded as expected.
	for _, key := range []string{"results", "total", "pageCount", "pageNumber", "pageSize"} {
		assert.True(t, bytesContains(raw, `"`+key+`"`), "search response missing %q key", key)
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
	require.Equal(t, http.StatusOK, resp.StatusCode)
	_, hasVersion := body["version"]
	require.True(t, hasVersion, "response missing 'version' (provider reads Userauthorization.Version)")
	roles, present := body["roles"]
	require.True(t, present, "response missing 'roles' (provider iterates Userauthorization.Roles)")
	require.NotNil(t, roles, "response 'roles' is JSON null — must be array (provider iterates)")
	_, isArr := roles.([]any)
	assert.True(t, isArr, "response 'roles' is not an array: %T", roles)
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
	require.Equal(t, http.StatusOK, resp.StatusCode)
	roles, ok := body["roles"].([]any)
	require.True(t, ok, "response 'roles' missing or not an array: %T", body["roles"])
	require.Len(t, roles, 2, "roles length")
	want := []string{"role-a", "role-b"}
	for i, r := range roles {
		entry, _ := r.(map[string]any)
		id, _ := entry["id"].(string)
		assert.Equal(t, want[i], id, "roles[%d].id", i)
		uri, _ := entry["selfUri"].(string)
		assert.NotEmpty(t, uri, "roles[%d].selfUri is empty", i)
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
	assert.Equal(t, http.StatusNoContent, resp.StatusCode, "body: %s", raw)
	assert.Empty(t, raw, "204 response must have empty body")
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
	require.Equal(t, http.StatusCreated, resp.StatusCode, "create group")
	gid, _ := created["id"].(string)

	resp = ts.PostJSON(t, "/api/v2/groups/"+gid+"/members",
		map[string]any{"memberIds": []any{"u1", "u2", "u3"}}, nil)
	require.Equal(t, http.StatusNoContent, resp.StatusCode, "members add")

	var listing map[string]any
	resp = ts.GetJSON(t, "/api/v2/groups/"+gid+"/individuals", &listing)
	require.Equal(t, http.StatusOK, resp.StatusCode, "individuals list")
	entities, _ := listing["entities"].([]any)
	require.Len(t, entities, 3, "entities count after add")
	got := make(map[string]bool)
	for _, e := range entities {
		m, _ := e.(map[string]any)
		if id, _ := m["id"].(string); id != "" {
			got[id] = true
		}
	}
	for _, want := range []string{"u1", "u2", "u3"} {
		assert.True(t, got[want], "individuals missing %q after add; got %v", want, got)
	}

	// DELETE removes only u1, u2 — leaves u3.
	resp = ts.DeleteJSON(t, "/api/v2/groups/"+gid+"/members?id=u1,u2")
	require.Equal(t, http.StatusNoContent, resp.StatusCode, "members delete")
	var after map[string]any
	resp = ts.GetJSON(t, "/api/v2/groups/"+gid+"/individuals", &after)
	require.Equal(t, http.StatusOK, resp.StatusCode, "individuals list after delete")
	remaining, _ := after["entities"].([]any)
	require.Len(t, remaining, 1, "entities after delete")
	last, _ := remaining[0].(map[string]any)
	lastID, _ := last["id"].(string)
	assert.Equal(t, "u3", lastID, "remaining entity id")
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
			require.Equal(t, http.StatusOK, resp.StatusCode, "GET %s (must not 501)", path)
			_, hasAlertTimeout := body["alertTimeoutSeconds"]
			assert.True(t, hasAlertTimeout, "GET %s: response missing alertTimeoutSeconds", path)

			var patched map[string]any
			resp = ts.PatchJSON(t, path,
				map[string]any{"alertTimeoutSeconds": 45}, &patched)
			require.Equal(t, http.StatusOK, resp.StatusCode, "PATCH %s (must not 501)", path)
			v, _ := patched["alertTimeoutSeconds"].(float64)
			assert.Equal(t, 45, int(v), "PATCH %s: alertTimeoutSeconds", path)
		})
	}
}

// --- contract-test helpers ------------------------------------------

func assertNonEmptyDivisionID(t *testing.T, body map[string]any, label string) {
	t.Helper()
	div, ok := body["division"].(map[string]any)
	require.True(t, ok, "%s: missing 'division' object (provider derefs *Division.Id)", label)
	require.NotNil(t, div, "%s: missing 'division' object (provider derefs *Division.Id)", label)
	id, _ := div["id"].(string)
	require.NotEmpty(t, id, "%s: division.id is empty (segfaults the SDK)", label)
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	raw, err := json.Marshal(v)
	require.NoError(t, err, "marshal")
	return raw
}

func bytesContains(b []byte, s string) bool { return bytes.Contains(b, []byte(s)) }
