package handlers_test

import (
	"net/http"
	"testing"

	"github.com/redscaresu/fakegenesys/testutil"
)

// --- routing_queue ---------------------------------------------------

func TestRoutingQueue_Lifecycle(t *testing.T) {
	ts := testutil.NewTestServer(t)
	var created map[string]any
	resp := ts.PostJSON(t, "/api/v2/routing/queues",
		map[string]any{"name": "Support"}, &created)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create: status %d", resp.StatusCode)
	}
	id, _ := created["id"].(string)
	if id == "" {
		t.Fatalf("create: missing id")
	}

	resp = ts.PutJSON(t, "/api/v2/routing/queues/"+id,
		map[string]any{"name": "Support", "description": "Tier-1 support queue"}, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("update: status %d", resp.StatusCode)
	}

	resp = ts.DeleteJSON(t, "/api/v2/routing/queues/"+id)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("delete: status %d", resp.StatusCode)
	}
	resp = ts.GetJSON(t, "/api/v2/routing/queues/"+id, nil)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("get-after-delete: status %d, want 404", resp.StatusCode)
	}
}

func TestRoutingQueue_DuplicateName409(t *testing.T) {
	ts := testutil.NewTestServer(t)
	ts.PostJSON(t, "/api/v2/routing/queues",
		map[string]any{"name": "dup-queue"}, nil)
	resp := ts.PostJSON(t, "/api/v2/routing/queues",
		map[string]any{"name": "dup-queue"}, nil)
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("status = %d, want 409", resp.StatusCode)
	}
}

func TestRoutingQueue_Members_IdempotentReplace(t *testing.T) {
	ts := testutil.NewTestServer(t)
	var queue map[string]any
	ts.PostJSON(t, "/api/v2/routing/queues",
		map[string]any{"name": "support-membership"}, &queue)
	qID := queue["id"].(string)
	var u1, u2, u3 map[string]any
	ts.PostJSON(t, "/api/v2/users",
		map[string]any{"name": "U1", "email": "u1@example.com"}, &u1)
	ts.PostJSON(t, "/api/v2/users",
		map[string]any{"name": "U2", "email": "u2@example.com"}, &u2)
	ts.PostJSON(t, "/api/v2/users",
		map[string]any{"name": "U3", "email": "u3@example.com"}, &u3)
	u1ID, u2ID, u3ID := u1["id"].(string), u2["id"].(string), u3["id"].(string)

	// PATCH = idempotent set replace.
	resp := ts.PatchJSON(t, "/api/v2/routing/queues/"+qID+"/members",
		[]map[string]any{
			{"id": u1ID, "ringNumber": 1},
			{"id": u2ID, "ringNumber": 2},
		}, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("first replace: status %d", resp.StatusCode)
	}

	var listing struct {
		Entities []map[string]any `json:"entities"`
		Total    int              `json:"total"`
	}
	resp = ts.GetJSON(t, "/api/v2/routing/queues/"+qID+"/members", &listing)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list: status %d", resp.StatusCode)
	}
	if listing.Total != 2 {
		t.Fatalf("after first replace: total = %d, want 2", listing.Total)
	}

	// Second replace with {u2,u3} should drop u1 + add u3.
	resp = ts.PatchJSON(t, "/api/v2/routing/queues/"+qID+"/members",
		[]map[string]any{
			{"id": u2ID},
			{"id": u3ID},
		}, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("second replace: status %d", resp.StatusCode)
	}
	ts.GetJSON(t, "/api/v2/routing/queues/"+qID+"/members", &listing)
	if listing.Total != 2 {
		t.Fatalf("after second replace: total = %d, want 2", listing.Total)
	}
	// Drop the queue; members cascade.
	resp = ts.DeleteJSON(t, "/api/v2/routing/queues/"+qID)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("queue delete: status %d", resp.StatusCode)
	}
	resp = ts.GetJSON(t, "/api/v2/routing/queues/"+qID+"/members", nil)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("members on deleted queue: status %d, want 404", resp.StatusCode)
	}
}

func TestRoutingQueue_Members_OnMissingQueue404(t *testing.T) {
	ts := testutil.NewTestServer(t)
	resp := ts.PatchJSON(t, "/api/v2/routing/queues/no-such-queue/members",
		[]map[string]any{{"id": "irrelevant"}}, nil)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
}

// S112 finding #5: POST /members?delete=true removes the listed users.
func TestRoutingQueue_MembersAdd_DeleteFlag(t *testing.T) {
	ts := testutil.NewTestServer(t)
	var queue map[string]any
	ts.PostJSON(t, "/api/v2/routing/queues",
		map[string]any{"name": "queue-delete-flag"}, &queue)
	qID := queue["id"].(string)
	var u1 map[string]any
	ts.PostJSON(t, "/api/v2/users",
		map[string]any{"name": "U1", "email": "delete-flag@example.com"}, &u1)
	u1ID := u1["id"].(string)

	ts.PostJSON(t, "/api/v2/routing/queues/"+qID+"/members",
		[]map[string]any{{"id": u1ID}}, nil)
	var listing struct {
		Total int `json:"total"`
	}
	ts.GetJSON(t, "/api/v2/routing/queues/"+qID+"/members", &listing)
	if listing.Total != 1 {
		t.Fatalf("after add: total = %d, want 1", listing.Total)
	}

	resp := ts.PostJSON(t, "/api/v2/routing/queues/"+qID+"/members?delete=true",
		[]map[string]any{{"id": u1ID}}, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("delete-flag POST: status %d", resp.StatusCode)
	}
	ts.GetJSON(t, "/api/v2/routing/queues/"+qID+"/members", &listing)
	if listing.Total != 0 {
		t.Fatalf("after delete-flag: total = %d, want 0", listing.Total)
	}
}

// S112 finding #7: list users filters by ?state.
func TestUser_ListFiltersByState(t *testing.T) {
	ts := testutil.NewTestServer(t)
	var alive, doomed map[string]any
	ts.PostJSON(t, "/api/v2/users",
		map[string]any{"name": "Alive", "email": "alive@example.com"}, &alive)
	ts.PostJSON(t, "/api/v2/users",
		map[string]any{"name": "Doomed", "email": "doomed@example.com"}, &doomed)
	ts.DeleteJSON(t, "/api/v2/users/"+doomed["id"].(string))

	type listing struct {
		Entities []map[string]any `json:"entities"`
		Total    int              `json:"total"`
	}

	var def listing
	ts.GetJSON(t, "/api/v2/users", &def)
	if def.Total != 1 {
		t.Fatalf("default state: total = %d, want 1", def.Total)
	}

	var del listing
	ts.GetJSON(t, "/api/v2/users?state=deleted", &del)
	if del.Total != 1 {
		t.Fatalf("?state=deleted: total = %d, want 1", del.Total)
	}

	var anyState listing
	ts.GetJSON(t, "/api/v2/users?state=any", &anyState)
	if anyState.Total != 2 {
		t.Fatalf("?state=any: total = %d, want 2", anyState.Total)
	}
}

// --- routing_skill ---------------------------------------------------

func TestRoutingSkill_Lifecycle(t *testing.T) {
	ts := testutil.NewTestServer(t)
	var created map[string]any
	resp := ts.PostJSON(t, "/api/v2/routing/skills",
		map[string]any{"name": "english-tier1"}, &created)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create: status %d", resp.StatusCode)
	}
	id := created["id"].(string)
	resp = ts.PatchJSON(t, "/api/v2/routing/skills/"+id,
		map[string]any{"description": "Tier 1"}, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("update: status %d", resp.StatusCode)
	}
	resp = ts.DeleteJSON(t, "/api/v2/routing/skills/"+id)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("delete: status %d", resp.StatusCode)
	}
}

// --- routing_wrapupcode ----------------------------------------------

func TestRoutingWrapupcode_Lifecycle(t *testing.T) {
	ts := testutil.NewTestServer(t)
	var created map[string]any
	resp := ts.PostJSON(t, "/api/v2/routing/wrapupcodes",
		map[string]any{"name": "resolved"}, &created)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create: status %d", resp.StatusCode)
	}
	id := created["id"].(string)
	resp = ts.PutJSON(t, "/api/v2/routing/wrapupcodes/"+id,
		map[string]any{"name": "resolved", "description": "Resolved by agent"}, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("update: status %d", resp.StatusCode)
	}
	resp = ts.DeleteJSON(t, "/api/v2/routing/wrapupcodes/"+id)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("delete: status %d", resp.StatusCode)
	}
}

// --- routing_language ------------------------------------------------

func TestRoutingLanguage_Lifecycle(t *testing.T) {
	ts := testutil.NewTestServer(t)
	var created map[string]any
	resp := ts.PostJSON(t, "/api/v2/routing/languages",
		map[string]any{"name": "en-US"}, &created)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create: status %d", resp.StatusCode)
	}
	id := created["id"].(string)
	resp = ts.GetJSON(t, "/api/v2/routing/languages/"+id, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get: status %d", resp.StatusCode)
	}
	resp = ts.DeleteJSON(t, "/api/v2/routing/languages/"+id)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("delete: status %d", resp.StatusCode)
	}
}

// --- routing_utilization (singleton) ---------------------------------

func TestRoutingUtilization_Singleton(t *testing.T) {
	ts := testutil.NewTestServer(t)
	// Initial GET returns default config (empty utilization map).
	var initial map[string]any
	resp := ts.GetJSON(t, "/api/v2/routing/utilization", &initial)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("initial get: status %d", resp.StatusCode)
	}
	if _, ok := initial["utilization"]; !ok {
		t.Fatalf("initial get: missing utilization key: %v", initial)
	}

	// PUT replaces.
	put := map[string]any{
		"utilization": map[string]any{
			"call":    map[string]any{"maximumCapacity": 1},
			"email":   map[string]any{"maximumCapacity": 3},
		},
	}
	var updated map[string]any
	resp = ts.PutJSON(t, "/api/v2/routing/utilization", put, &updated)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("put: status %d", resp.StatusCode)
	}

	var after map[string]any
	ts.GetJSON(t, "/api/v2/routing/utilization", &after)
	u := after["utilization"].(map[string]any)
	if _, ok := u["call"]; !ok {
		t.Fatalf("PUT did not persist: %v", after)
	}

	// DELETE → back to default.
	resp = ts.DeleteJSON(t, "/api/v2/routing/utilization")
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("delete: status %d", resp.StatusCode)
	}
	var reset map[string]any
	ts.GetJSON(t, "/api/v2/routing/utilization", &reset)
	if u, ok := reset["utilization"].(map[string]any); !ok || len(u) != 0 {
		t.Fatalf("after delete: expected empty utilization map, got %v", reset)
	}
}
