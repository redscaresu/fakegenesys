package handlers_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
	require.NoError(t, err, "NewRequest")
	req.Header.Set("Authorization", "Bearer "+tok)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err, "Do")
	return resp
}

func TestOrganizationsMe_BasicShape(t *testing.T) {
	_, srv := newTestApp(t)
	tok := mintToken(t, srv)
	resp := authedGet(t, srv.URL, "/api/v2/organizations/me", tok)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	var body map[string]any
	require.NoError(t, decodeJSON(resp, &body), "decode")
	for _, k := range []string{"id", "name", "thirdPartyOrgName", "defaultLanguage", "defaultCountryCode", "domain", "version", "state", "selfUri"} {
		_, ok := body[k]
		assert.True(t, ok, "response missing %q field", k)
	}
}

// TestContract_authorization_products_total_int is the load-bearing
// regression for the S116c crash. The Terraform provider's
// `getAuthorizationProducts` does:
//
//	products := make([]string, *productEntities.Total)
//
// at provider.go:224. A nil `Total` segfaults the plugin. Keep `total`
// present in the response forever. Paired with
// CRITICAL[authorization-products-total-int] in handlers/organization.go.
func TestContract_authorization_products_total_int(t *testing.T) {
	_, srv := newTestApp(t)
	tok := mintToken(t, srv)
	resp := authedGet(t, srv.URL, "/api/v2/authorization/products", tok)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	var body map[string]any
	require.NoError(t, decodeJSON(resp, &body), "decode")
	total, ok := body["total"]
	require.True(t, ok, "response missing 'total' (segfaults genesyscloud provider)")
	totalF, ok := total.(float64)
	require.True(t, ok, "'total' = %v (%T), want numeric", total, total)
	entities, ok := body["entities"].([]any)
	require.True(t, ok, "'entities' missing or not an array: %v", body["entities"])
	assert.Equal(t, len(entities), int(totalF), "total vs len(entities) (must match)")
}

func TestAuthorizationDivisions_Home(t *testing.T) {
	_, srv := newTestApp(t)
	tok := mintToken(t, srv)
	resp := authedGet(t, srv.URL, "/api/v2/authorization/divisions/home", tok)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	var body map[string]any
	require.NoError(t, decodeJSON(resp, &body), "decode")
	v, _ := body["homeDivision"].(bool)
	assert.True(t, v, "homeDivision = %v, want true", body["homeDivision"])
	_, ok := body["id"].(string)
	assert.True(t, ok, "id missing or not a string")
}

func TestTokensMe(t *testing.T) {
	_, srv := newTestApp(t)
	tok := mintToken(t, srv)
	resp := authedGet(t, srv.URL, "/api/v2/tokens/me", tok)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

// authedGetRaw is the byte-level cousin of authedGet — caller gets the
// raw bytes back. Required by TestContract_tokens_me_oauthclient_pascal_case
// which inspects literal JSON keys (Go's default json.Unmarshal is
// case-insensitive and would silently hide a camelCase/PascalCase
// regression).
func authedGetRaw(t *testing.T, srvURL, path, tok string) (*http.Response, []byte) {
	t.Helper()
	resp := authedGet(t, srvURL, path, tok)
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	require.NoError(t, err, "read body")
	return resp, raw
}

func authedPostJSONStatus(t *testing.T, srvURL, path, tok string, body any) (*http.Response, []byte) {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		require.NoError(t, json.NewEncoder(&buf).Encode(body), "marshal body")
	}
	req, err := http.NewRequest(http.MethodPost, srvURL+path, &buf)
	require.NoError(t, err, "NewRequest")
	req.Header.Set("Authorization", "Bearer "+tok)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err, "Do")
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	require.NoError(t, err, "read body")
	return resp, raw
}

// TestContract_tokens_me_oauthclient_pascal_case asserts the
// /tokens/me response uses the PascalCase JSON key "OAuthClient"
// (NOT camelCase "oAuthClient") with OAuthClient.organization.id ==
// "purecloud-builtin". Without this exact key the genesyscloud SDK's
// custom UnmarshalJSON does TokeninfoMap["OAuthClient"] verbatim, sees
// a missing key, and leaves OAuthClient nil; then the provider's
// updateTerraformUserWithRole at oauth_client provider.go:213
// dereferences *tokenInfo.OAuthClient.Organization.Id and segfaults
// the plugin on every oauth_client create. We inspect raw bytes
// because Go's json.Unmarshal IS case-insensitive — decoding alone
// would silently hide the regression.
//
// Paired with CRITICAL[tokens-me-oauthclient-pascal-case] in
// handlers/organization.go.
func TestContract_tokens_me_oauthclient_pascal_case(t *testing.T) {
	_, srv := newTestApp(t)
	tok := mintToken(t, srv)
	resp, raw := authedGetRaw(t, srv.URL, "/api/v2/tokens/me", tok)
	require.Equal(t, http.StatusOK, resp.StatusCode, "body: %s", raw)
	// Raw-byte case-sensitive check first — Go's json.Unmarshal is
	// case-insensitive and would mask the regression.
	require.True(t, bytes.Contains(raw, []byte(`"OAuthClient"`)),
		"response body missing literal PascalCase key \"OAuthClient\"; got: %s", raw)
	require.False(t, bytes.Contains(raw, []byte(`"oAuthClient"`)),
		"response body contains camelCase \"oAuthClient\" key — SDK requires PascalCase; got: %s", raw)
	var body map[string]any
	require.NoError(t, json.Unmarshal(raw, &body), "decode")
	oauthClient, ok := body["OAuthClient"].(map[string]any)
	require.True(t, ok, "OAuthClient is not a JSON object: %T (%v)", body["OAuthClient"], body["OAuthClient"])
	org, ok := oauthClient["organization"].(map[string]any)
	require.True(t, ok, "OAuthClient.organization is not a JSON object: %T (%v)", oauthClient["organization"], oauthClient["organization"])
	id, _ := org["id"].(string)
	assert.Equal(t, "purecloud-builtin", id,
		"OAuthClient.organization.id (literal value routes provider down safe role-assignment branch)")
}

// TestContract_users_me_synthetic_tf_user asserts /users/me returns a
// synthetic terraform admin user with a stable, non-empty id and a
// non-empty division.id. After the oauth_client chain routes through
// /tokens/me, the provider's updateTerraformUserWithRole calls
// /users/me to fetch the terraform user. If the id changes between
// reads OR division.id is empty, the GetTerraformUser path either
// loses identity or nil-derefs Division.Id.
//
// Paired with CRITICAL[users-me-synthetic-tf-user] in
// handlers/organization.go.
func TestContract_users_me_synthetic_tf_user(t *testing.T) {
	_, srv := newTestApp(t)
	tok := mintToken(t, srv)

	first := authedGetJSON(t, srv.URL, "/api/v2/users/me", tok)
	id, _ := first["id"].(string)
	require.NotEmpty(t, id, "users/me: missing id")
	div, _ := first["division"].(map[string]any)
	require.NotNil(t, div, "users/me: missing division (provider derefs *user.Division.Id)")
	divID, _ := div["id"].(string)
	require.NotEmpty(t, divID, "users/me: division.id is empty (segfaults the SDK)")

	second := authedGetJSON(t, srv.URL, "/api/v2/users/me", tok)
	gotID, _ := second["id"].(string)
	assert.Equal(t, id, gotID,
		"users/me id is not stable across calls (terraform-user identity must persist)")
}

// TestContract_authorization_subject_grants_non_nil asserts the
// /authorization/subjects/{id} endpoint echoes the requested id,
// returns a non-empty name, and a non-nil grants array. The
// genesyscloud_user_roles flatten path iterates the grants slice
// during diff computation; a nil grants causes the provider to panic
// during the role-merge step.
//
// Paired with CRITICAL[authorization-subject-grants-non-nil] in
// handlers/organization.go.
func TestContract_authorization_subject_grants_non_nil(t *testing.T) {
	_, srv := newTestApp(t)
	tok := mintToken(t, srv)
	body := authedGetJSON(t, srv.URL, "/api/v2/authorization/subjects/sub-xyz", tok)
	got, _ := body["id"].(string)
	assert.Equal(t, "sub-xyz", got, "subject.id")
	name, _ := body["name"].(string)
	assert.NotEmpty(t, name, "subject.name is empty")
	// "grants" key MUST be present and decode to a slice (empty is fine;
	// nil/missing is not — provider iterates the slice).
	rawGrants, present := body["grants"]
	require.True(t, present, "subject body missing 'grants' key (provider iterates it)")
	require.NotNil(t, rawGrants, "subject 'grants' is JSON null — must be an array (provider iterates)")
	_, ok := rawGrants.([]any)
	assert.True(t, ok, "subject 'grants' is not an array: %T (%v)", rawGrants, rawGrants)
}

// TestContract_subjects_bulkadd_bulkremove_204 asserts both /bulkadd
// and /bulkremove return HTTP 204 with any body. The provider gates
// only on the status code; body persistence is intentionally not
// asserted (the smoke harness validates apply success, not runtime
// RBAC behavior).
//
// Paired with CRITICAL[subjects-bulkadd-bulkremove-204] in
// handlers/user_subresources.go.
func TestContract_subjects_bulkadd_bulkremove_204(t *testing.T) {
	_, srv := newTestApp(t)
	tok := mintToken(t, srv)

	grants := map[string]any{
		"grants": []any{
			map[string]any{
				"roleId":      "r1",
				"divisionIds": []any{"d1"},
			},
		},
	}

	for _, path := range []string{
		"/api/v2/authorization/subjects/sub1/bulkadd",
		"/api/v2/authorization/subjects/sub1/bulkremove",
	} {
		resp, raw := authedPostJSONStatus(t, srv.URL, path, tok, grants)
		require.Equal(t, http.StatusNoContent, resp.StatusCode,
			"POST %s; body: %s", path, raw)
		if len(raw) > 0 {
			assert.Empty(t, strings.TrimSpace(string(raw)),
				"POST %s: 204 must return empty body, got %q", path, raw)
		}
	}
}

// authedGetJSON is a small local helper that wraps authedGet + json
// decode. Mirrors the testutil pattern but stays in the handlers_test
// package's existing low-level style for raw-byte parity with
// authedGetRaw.
func authedGetJSON(t *testing.T, srvURL, path, tok string) map[string]any {
	t.Helper()
	resp, raw := authedGetRaw(t, srvURL, path, tok)
	require.Equal(t, http.StatusOK, resp.StatusCode, "GET %s; body: %s", path, raw)
	var body map[string]any
	require.NoError(t, json.Unmarshal(raw, &body), "decode; body: %s", raw)
	return body
}
