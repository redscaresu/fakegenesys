package handlers_test

import (
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
