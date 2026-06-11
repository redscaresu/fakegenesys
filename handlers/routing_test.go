package handlers_test

import (
	"net/http"
	"testing"

	"github.com/redscaresu/fakegenesys/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- routing_queue ---------------------------------------------------

func TestRoutingQueue_Lifecycle(t *testing.T) {
	ts := testutil.NewTestServer(t)
	var created map[string]any
	resp := ts.PostJSON(t, "/api/v2/routing/queues",
		map[string]any{"name": "Support"}, &created)
	// S116c: queue create returns 200 (matches real Genesys + the
	// genesyscloud provider's StatusOK check). Other routing resources
	// still return 201.
	require.Equal(t, http.StatusOK, resp.StatusCode, "create")
	id, _ := created["id"].(string)
	require.NotEmpty(t, id, "create: missing id")

	resp = ts.PutJSON(t, "/api/v2/routing/queues/"+id,
		map[string]any{"name": "Support", "description": "Tier-1 support queue"}, nil)
	assert.Equal(t, http.StatusOK, resp.StatusCode, "update")

	resp = ts.DeleteJSON(t, "/api/v2/routing/queues/"+id)
	assert.Equal(t, http.StatusNoContent, resp.StatusCode, "delete")
	resp = ts.GetJSON(t, "/api/v2/routing/queues/"+id, nil)
	assert.Equal(t, http.StatusNotFound, resp.StatusCode, "get-after-delete")
}

func TestRoutingQueue_DuplicateName409(t *testing.T) {
	ts := testutil.NewTestServer(t)
	ts.PostJSON(t, "/api/v2/routing/queues",
		map[string]any{"name": "dup-queue"}, nil)
	resp := ts.PostJSON(t, "/api/v2/routing/queues",
		map[string]any{"name": "dup-queue"}, nil)
	assert.Equal(t, http.StatusConflict, resp.StatusCode)
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
	require.Equal(t, http.StatusOK, resp.StatusCode, "first replace")

	var listing struct {
		Entities []map[string]any `json:"entities"`
		Total    int              `json:"total"`
	}
	resp = ts.GetJSON(t, "/api/v2/routing/queues/"+qID+"/members", &listing)
	require.Equal(t, http.StatusOK, resp.StatusCode, "list")
	assert.Equal(t, 2, listing.Total, "after first replace")

	// Second replace with {u2,u3} should drop u1 + add u3.
	resp = ts.PatchJSON(t, "/api/v2/routing/queues/"+qID+"/members",
		[]map[string]any{
			{"id": u2ID},
			{"id": u3ID},
		}, nil)
	require.Equal(t, http.StatusOK, resp.StatusCode, "second replace")
	ts.GetJSON(t, "/api/v2/routing/queues/"+qID+"/members", &listing)
	assert.Equal(t, 2, listing.Total, "after second replace")
	// Drop the queue; members cascade.
	resp = ts.DeleteJSON(t, "/api/v2/routing/queues/"+qID)
	require.Equal(t, http.StatusNoContent, resp.StatusCode, "queue delete")
	resp = ts.GetJSON(t, "/api/v2/routing/queues/"+qID+"/members", nil)
	assert.Equal(t, http.StatusNotFound, resp.StatusCode, "members on deleted queue")
}

func TestRoutingQueue_Members_OnMissingQueue404(t *testing.T) {
	ts := testutil.NewTestServer(t)
	resp := ts.PatchJSON(t, "/api/v2/routing/queues/no-such-queue/members",
		[]map[string]any{{"id": "irrelevant"}}, nil)
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
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
	require.Equal(t, 1, listing.Total, "after add")

	resp := ts.PostJSON(t, "/api/v2/routing/queues/"+qID+"/members?delete=true",
		[]map[string]any{{"id": u1ID}}, nil)
	require.Equal(t, http.StatusOK, resp.StatusCode, "delete-flag POST")
	ts.GetJSON(t, "/api/v2/routing/queues/"+qID+"/members", &listing)
	assert.Equal(t, 0, listing.Total, "after delete-flag")
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
	assert.Equal(t, 1, def.Total, "default state")

	var del listing
	ts.GetJSON(t, "/api/v2/users?state=deleted", &del)
	assert.Equal(t, 1, del.Total, "?state=deleted")

	var anyState listing
	ts.GetJSON(t, "/api/v2/users?state=any", &anyState)
	assert.Equal(t, 2, anyState.Total, "?state=any")
}

// --- routing_skill ---------------------------------------------------

func TestRoutingSkill_Lifecycle(t *testing.T) {
	ts := testutil.NewTestServer(t)
	var created map[string]any
	resp := ts.PostJSON(t, "/api/v2/routing/skills",
		map[string]any{"name": "english-tier1"}, &created)
	require.Equal(t, http.StatusCreated, resp.StatusCode, "create")
	id := created["id"].(string)
	resp = ts.PatchJSON(t, "/api/v2/routing/skills/"+id,
		map[string]any{"description": "Tier 1"}, nil)
	assert.Equal(t, http.StatusOK, resp.StatusCode, "update")
	resp = ts.DeleteJSON(t, "/api/v2/routing/skills/"+id)
	assert.Equal(t, http.StatusNoContent, resp.StatusCode, "delete")
}

// --- routing_wrapupcode ----------------------------------------------

func TestRoutingWrapupcode_Lifecycle(t *testing.T) {
	ts := testutil.NewTestServer(t)
	var created map[string]any
	resp := ts.PostJSON(t, "/api/v2/routing/wrapupcodes",
		map[string]any{"name": "resolved"}, &created)
	require.Equal(t, http.StatusCreated, resp.StatusCode, "create")
	id := created["id"].(string)
	resp = ts.PutJSON(t, "/api/v2/routing/wrapupcodes/"+id,
		map[string]any{"name": "resolved", "description": "Resolved by agent"}, nil)
	assert.Equal(t, http.StatusOK, resp.StatusCode, "update")
	resp = ts.DeleteJSON(t, "/api/v2/routing/wrapupcodes/"+id)
	assert.Equal(t, http.StatusNoContent, resp.StatusCode, "delete")
}

// --- routing_language ------------------------------------------------

func TestRoutingLanguage_Lifecycle(t *testing.T) {
	ts := testutil.NewTestServer(t)
	var created map[string]any
	resp := ts.PostJSON(t, "/api/v2/routing/languages",
		map[string]any{"name": "en-US"}, &created)
	require.Equal(t, http.StatusCreated, resp.StatusCode, "create")
	id := created["id"].(string)
	resp = ts.GetJSON(t, "/api/v2/routing/languages/"+id, nil)
	assert.Equal(t, http.StatusOK, resp.StatusCode, "get")
	resp = ts.DeleteJSON(t, "/api/v2/routing/languages/"+id)
	assert.Equal(t, http.StatusNoContent, resp.StatusCode, "delete")
}

// --- routing_utilization (singleton) ---------------------------------

func TestRoutingUtilization_Singleton(t *testing.T) {
	ts := testutil.NewTestServer(t)
	// Initial GET returns default config (empty utilization map).
	var initial map[string]any
	resp := ts.GetJSON(t, "/api/v2/routing/utilization", &initial)
	require.Equal(t, http.StatusOK, resp.StatusCode, "initial get")
	_, hasUtil := initial["utilization"]
	require.True(t, hasUtil, "initial get: missing utilization key: %v", initial)

	// PUT replaces.
	put := map[string]any{
		"utilization": map[string]any{
			"call":  map[string]any{"maximumCapacity": 1},
			"email": map[string]any{"maximumCapacity": 3},
		},
	}
	var updated map[string]any
	resp = ts.PutJSON(t, "/api/v2/routing/utilization", put, &updated)
	require.Equal(t, http.StatusOK, resp.StatusCode, "put")

	var after map[string]any
	ts.GetJSON(t, "/api/v2/routing/utilization", &after)
	u := after["utilization"].(map[string]any)
	_, hasCall := u["call"]
	assert.True(t, hasCall, "PUT did not persist: %v", after)

	// DELETE → back to default.
	resp = ts.DeleteJSON(t, "/api/v2/routing/utilization")
	require.Equal(t, http.StatusNoContent, resp.StatusCode, "delete")
	var reset map[string]any
	ts.GetJSON(t, "/api/v2/routing/utilization", &reset)
	resetU, ok := reset["utilization"].(map[string]any)
	if assert.True(t, ok, "after delete: utilization not a map: %v", reset) {
		assert.Empty(t, resetU, "after delete: expected empty utilization map")
	}
}

// --- S123 contract tests ---------------------------------------------

// TestContract_routing_queue_create_200_with_membercount asserts two
// invariants the genesyscloud_routing_queue resource depends on:
//
//  1. POST /api/v2/routing/queues returns HTTP 200 (NOT 201). Real
//     Genesys returns 200; the provider's CreateContext fails the apply
//     if it sees anything else (resource_genesyscloud_routing_queue.go:154).
//
//  2. Subsequent GET /api/v2/routing/queues/{id} includes a non-nil
//     integer `memberCount`. The provider's flattenQueueMembers
//     short-circuits with "no members belong to queue" when MemberCount
//     is nil — even if /members would have returned actual rows. State
//     ends up missing the HCL members and the ring_num consistency
//     check fails on subsequent plans.
//
// Paired with CRITICAL[routing-queue-create-200-with-membercount] in
// handlers/routing_queue.go.
func TestContract_routing_queue_create_200_with_membercount(t *testing.T) {
	ts := testutil.NewTestServer(t)
	var created map[string]any
	resp := ts.PostJSON(t, "/api/v2/routing/queues",
		map[string]any{"name": "contract-q-200"}, &created)
	require.Equal(t, http.StatusOK, resp.StatusCode,
		"POST /routing/queues (NOT 201 — provider gates on 200)")
	id, _ := created["id"].(string)
	require.NotEmpty(t, id, "POST /routing/queues: response missing id")

	var got map[string]any
	resp = ts.GetJSON(t, "/api/v2/routing/queues/"+id, &got)
	require.Equal(t, http.StatusOK, resp.StatusCode, "GET /routing/queues/{id}")
	mc, present := got["memberCount"]
	require.True(t, present, "GET response missing 'memberCount' — provider's flattenQueueMembers short-circuits on nil")
	require.NotNil(t, mc, "GET response 'memberCount' is JSON null — must be integer (provider short-circuits)")
	_, isNum := mc.(float64)
	assert.True(t, isNum, "GET response 'memberCount' is %T (%v), want number", mc, mc)
}
