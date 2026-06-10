package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
)

// organization endpoint — minimal stub for the post-auth org probe the
// Genesys SDK performs immediately after issuing a token. Without it,
// every API call (including the first GET /api/v2/users) fails with
// "API Error: 501" because the SDK can't establish the tenant context.
//
// Returns a fixed organization document. Real Genesys's response is
// richer; we model just the fields the SDK reads at probe time.

const (
	fakegenesysOrgID          = "fakegenesys-org-00000000-0000-0000-0000-000000000000"
	fakegenesysHomeDivisionID = "fakegenesys-home-div-00000000-0000-0000-0000-000000000000"
)

func (app *Application) registerOrganizationRoutes(r chi.Router) {
	r.Get("/organizations/me", app.handleOrganizationsMe)
	// S116c: post-auth SDK probes. The Go SDK calls a handful of org-
	// info endpoints immediately after token issuance to set up the
	// client context. Each gets a minimal stub.
	r.Get("/authorization/products", app.handleAuthorizationProducts)
	r.Get("/authorization/divisions", app.handleAuthorizationDivisions)
	r.Get("/authorization/divisions/home", app.handleAuthorizationDivisionsHome)
	r.Get("/tokens/me", app.handleTokensMe)
	// CRITICAL[users-me-synthetic-tf-user]: S122d. GetTerraformUser
	// path. The genesyscloud provider's updateTerraformUserWithRole,
	// when it sees /tokens/me return oAuthClient.organization.id =
	// "purecloud-builtin", proceeds to fetch the "terraform user" via
	// GET /users/me to read their roles and assign new ones. Returning
	// a synthetic admin user with a stable non-empty id + a non-empty
	// division.id satisfies the call chain; the subsequent /users/{id}/
	// roles GET/PUT also needs handlers but those route to the existing
	// /users/{id}/* SDK surface so they may already be handled by the
	// standard user resource registrations (see /tokens/me docstring).
	// Locked in by TestContract_users_me_synthetic_tf_user.
	r.Get("/users/me", app.handleUsersMe)
	// CRITICAL[authorization-subject-grants-non-nil]: S122f.
	// GetAuthorizationSubject. The genesyscloud_user_roles resource's
	// flattenSubjectRoles / updateSubjectRoles paths fetch the existing
	// grants for a subject (user) via this endpoint before computing
	// the diff to PUT. The response must include the requested id, a
	// non-empty name, and a non-nil grants array (empty array is fine;
	// nil is not — provider iterates). Without it, every user_roles
	// apply 501s and aborts. Locked in by
	// TestContract_authorization_subject_grants_non_nil.
	r.Get("/authorization/subjects/{subjectId}", app.handleAuthorizationSubject)
}

func (app *Application) handleAuthorizationSubject(w http.ResponseWriter, r *http.Request) {
	subjectID := chi.URLParam(r, "subjectId")
	body := map[string]any{
		"id":     subjectID,
		"name":   "fakegenesys subject " + subjectID,
		"grants": []any{},
		"selfUri": "/api/v2/authorization/subjects/" + subjectID,
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(body)
}

const fakegenesysTerraformUserID = "fakegenesys-tf-user-0000-0000-0000-000000000000"

func (app *Application) handleUsersMe(w http.ResponseWriter, _ *http.Request) {
	body := map[string]any{
		"id":      fakegenesysTerraformUserID,
		"name":    "fakegenesys terraform user",
		"email":   "terraform@fakegenesys.local",
		"state":   "active",
		"version": 1,
		"selfUri": "/api/v2/users/" + fakegenesysTerraformUserID,
		"division": map[string]any{
			"id":   fakegenesysHomeDivisionID,
			"name": "Home",
		},
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(body)
}

// handleAuthorizationProducts — list of Genesys product entitlements
// the org has. SDK probes this to gate feature paths. Returning a
// broad list is safe; the provider only checks for the presence of
// specific products it needs.
//
// CRITICAL[authorization-products-total-int]: the response MUST include
// "total" (int). The Genesys Terraform provider's getAuthorizationProducts
// at provider.go:224 does `make([]string, *productEntities.Total)` — a
// nil Total nukes the plugin with a segfault before the first resource
// is created. Locked in by TestContract_authorization_products_total_int.
func (app *Application) handleAuthorizationProducts(w http.ResponseWriter, _ *http.Request) {
	entities := []map[string]any{
		{"id": "useCustomerEngagement", "name": "Customer Engagement"},
		{"id": "useDirectory", "name": "Directory"},
		{"id": "useRouting", "name": "Routing"},
		{"id": "useArchitect", "name": "Architect"},
		{"id": "useResponseManagement", "name": "Response Management"},
		{"id": "useOAuth", "name": "OAuth"},
	}
	body := map[string]any{
		"entities":   entities,
		"total":      len(entities),
		"pageCount":  1,
		"pageNumber": 1,
		"pageSize":   25,
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(body)
}

// handleAuthorizationDivisions — divisions are Genesys's RBAC scopes.
// Stub returns one Home division which most queries default to.
func (app *Application) handleAuthorizationDivisions(w http.ResponseWriter, _ *http.Request) {
	homeID := fakegenesysHomeDivisionID
	body := map[string]any{
		"entities": []map[string]any{
			{
				"id":          homeID,
				"name":        "Home",
				"description": "Home division",
				"homeDivision": true,
				"selfUri":      "/api/v2/authorization/divisions/" + homeID,
			},
		},
		"pageCount": 1, "pageNumber": 1, "pageSize": 25, "total": 1,
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(body)
}

// handleAuthorizationDivisionsHome — the SDK uses this shortcut to
// resolve the Home division id during provider config.
func (app *Application) handleAuthorizationDivisionsHome(w http.ResponseWriter, _ *http.Request) {
	homeID := fakegenesysHomeDivisionID
	body := map[string]any{
		"id":           homeID,
		"name":         "Home",
		"description":  "Home division",
		"homeDivision": true,
		"selfUri":      "/api/v2/authorization/divisions/" + homeID,
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(body)
}

// handleTokensMe — returns the current token's owner. SDK probes
// for permissions during connection setup. Stub returns a synthetic
// admin user with broad permissions.
//
// CRITICAL[tokens-me-oauthclient-pascal-case]: the response MUST include
// oAuthClient.organization.id. The Terraform provider's createOAuthClient
// calls updateTerraformUserWithRole at resource_genesyscloud_oauth_client.go:213
// which does `if *tokenInfo.OAuthClient.Organization.Id != "purecloud-builtin"`
// — a nil OAuthClient or Organization segfaults the plugin during
// EVERY oauth_client create. Returning "purecloud-builtin" routes the
// provider down its safe role-assignment path; any other value
// triggers an additional /users/me + role-assignment probe chain that
// we'd also need to mock. The key case-sensitivity invariant is covered
// in the body comment below. Locked in by
// TestContract_tokens_me_oauthclient_pascal_case.
func (app *Application) handleTokensMe(w http.ResponseWriter, _ *http.Request) {
	body := map[string]any{
		"authorizedScope": []string{},
		"homeOrganization": map[string]any{
			"id":   fakegenesysOrgID,
			"name": "fakegenesys",
		},
		"organization": map[string]any{
			"id":   fakegenesysOrgID,
			"name": "fakegenesys",
		},
		// MUST[tokens-me-oauthclient-pascal-case]: oauth_client crash fix
		// (S119/S122c). KEY MUST BE PascalCase "OAuthClient", not
		// camelCase "oAuthClient" — the Genesys platform-client-sdk-go's
		// Tokeninfo.UnmarshalJSON has a custom implementation that does
		// `TokeninfoMap["OAuthClient"]` directly, bypassing Go's default
		// case-insensitive matching. camelCase keys are silently dropped,
		// leaving OAuthClient nil, and updateTerraformUserWithRole at
		// provider.go:213 dereferences `*tokenInfo.OAuthClient.Organization.Id`
		// straight into a segfault. Shares the test paired with the
		// function-level CRITICAL[tokens-me-oauthclient-pascal-case] note.
		"OAuthClient": map[string]any{
			"id":   "fakegenesys-oauth-builtin",
			"name": "fakegenesys terraform client",
			"organization": map[string]any{
				"id":   "purecloud-builtin",
				"name": "fakegenesys",
			},
		},
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(body)
}

func (app *Application) handleOrganizationsMe(w http.ResponseWriter, _ *http.Request) {
	body := map[string]any{
		"id":                fakegenesysOrgID,
		"name":              "fakegenesys",
		"thirdPartyOrgName": "fakegenesys",
		"defaultLanguage":   "en-us",
		"defaultCountryCode": "US",
		"domain":            "mypurecloud.com",
		"version":           1,
		"state":             "active",
		"selfUri":           "/api/v2/organizations/" + fakegenesysOrgID,
		"features":          map[string]any{},
		"productPlatform":   "purecloud_voice",
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(body)
}
