package handlers_test

import (
	"bytes"
	"io"
	"mime/multipart"
	"net/http"
	"testing"

	"github.com/redscaresu/fakegenesys/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- architect_datatable ---------------------------------------------

func TestDatatable_Lifecycle(t *testing.T) {
	ts := testutil.NewTestServer(t)
	var created map[string]any
	resp := ts.PostJSON(t, "/api/v2/flows/datatables",
		map[string]any{
			"name": "lookup-table",
			"schema": map[string]any{
				"$schema": "http://json-schema.org/draft-04/schema#",
				"type":    "object",
			},
		}, &created)
	require.Equal(t, http.StatusCreated, resp.StatusCode, "create")
	id := created["id"].(string)

	resp = ts.PutJSON(t, "/api/v2/flows/datatables/"+id,
		map[string]any{"name": "lookup-table", "description": "updated"}, nil)
	assert.Equal(t, http.StatusOK, resp.StatusCode, "put")

	resp = ts.DeleteJSON(t, "/api/v2/flows/datatables/"+id)
	assert.Equal(t, http.StatusNoContent, resp.StatusCode, "delete")
}

func TestDatatable_Rows_CRUDAndCascade(t *testing.T) {
	ts := testutil.NewTestServer(t)
	var dt map[string]any
	ts.PostJSON(t, "/api/v2/flows/datatables",
		map[string]any{"name": "cascade-table"}, &dt)
	dtID := dt["id"].(string)

	var row map[string]any
	resp := ts.PostJSON(t, "/api/v2/flows/datatables/"+dtID+"/rows",
		map[string]any{"key": "row1", "value": "a"}, &row)
	require.Equal(t, http.StatusCreated, resp.StatusCode, "row create")
	// duplicate key → 409
	resp = ts.PostJSON(t, "/api/v2/flows/datatables/"+dtID+"/rows",
		map[string]any{"key": "row1", "value": "b"}, nil)
	assert.Equal(t, http.StatusConflict, resp.StatusCode, "dup row")
	// list
	var listing struct {
		Total int `json:"total"`
	}
	ts.GetJSON(t, "/api/v2/flows/datatables/"+dtID+"/rows", &listing)
	assert.Equal(t, 1, listing.Total, "list total")
	// cascade delete: drop datatable, rows go away.
	resp = ts.DeleteJSON(t, "/api/v2/flows/datatables/"+dtID)
	require.Equal(t, http.StatusNoContent, resp.StatusCode, "dt delete")
	resp = ts.GetJSON(t, "/api/v2/flows/datatables/"+dtID+"/rows", nil)
	assert.Equal(t, http.StatusNotFound, resp.StatusCode, "rows on deleted dt")
}

// --- architect_user_prompt -------------------------------------------

func TestUserPrompt_Lifecycle(t *testing.T) {
	ts := testutil.NewTestServer(t)
	var created map[string]any
	resp := ts.PostJSON(t, "/api/v2/architect/prompts",
		map[string]any{"name": "welcome-prompt"}, &created)
	require.Equal(t, http.StatusCreated, resp.StatusCode, "create")
	id := created["id"].(string)
	resp = ts.PutJSON(t, "/api/v2/architect/prompts/"+id,
		map[string]any{"description": "Greet caller"}, nil)
	assert.Equal(t, http.StatusOK, resp.StatusCode, "update")
	resp = ts.DeleteJSON(t, "/api/v2/architect/prompts/"+id)
	assert.Equal(t, http.StatusNoContent, resp.StatusCode, "delete")
}

func TestUserPrompt_DuplicateName409(t *testing.T) {
	ts := testutil.NewTestServer(t)
	ts.PostJSON(t, "/api/v2/architect/prompts",
		map[string]any{"name": "dup-prompt"}, nil)
	resp := ts.PostJSON(t, "/api/v2/architect/prompts",
		map[string]any{"name": "dup-prompt"}, nil)
	assert.Equal(t, http.StatusConflict, resp.StatusCode)
}

// --- flow ------------------------------------------------------------

func TestFlow_LifecycleAndStateMachine(t *testing.T) {
	ts := testutil.NewTestServer(t)
	var created map[string]any
	resp := ts.PostJSON(t, "/api/v2/flows",
		map[string]any{"name": "main-ivr", "type": "inboundcall"}, &created)
	require.Equal(t, http.StatusCreated, resp.StatusCode, "create")
	id := created["id"].(string)
	assert.Equal(t, "unpublished", created["state"], "initial state")

	// checkout → locked
	resp = ts.PostJSON(t, "/api/v2/flows/actions/checkout?flow="+id, nil, nil)
	require.Equal(t, http.StatusOK, resp.StatusCode, "checkout")
	var locked map[string]any
	ts.GetJSON(t, "/api/v2/flows/"+id, &locked)
	assert.Equal(t, "locked", locked["state"], "after checkout state")

	// S112 finding #10a: checkin → unpublished + cleared lock.
	resp = ts.PostJSON(t, "/api/v2/flows/actions/checkin?flow="+id, nil, nil)
	require.Equal(t, http.StatusOK, resp.StatusCode, "checkin")
	var checkedIn map[string]any
	ts.GetJSON(t, "/api/v2/flows/"+id, &checkedIn)
	assert.Equal(t, "unpublished", checkedIn["state"], "after checkin state")
	if v, ok := checkedIn["lockedUser"]; ok {
		assert.Nil(t, v, "after checkin lockedUser should be nil")
	}

	// Re-lock, then force-unlock.
	ts.PostJSON(t, "/api/v2/flows/actions/checkout?flow="+id, nil, nil)
	// S112 finding #10b: unlock from locked → unpublished + cleared lock.
	resp = ts.PostJSON(t, "/api/v2/flows/actions/unlock?flow="+id, nil, nil)
	require.Equal(t, http.StatusOK, resp.StatusCode, "unlock")
	var unlocked map[string]any
	ts.GetJSON(t, "/api/v2/flows/"+id, &unlocked)
	assert.Equal(t, "unpublished", unlocked["state"], "after unlock state")

	// publish → published + cleared lock
	resp = ts.PostJSON(t, "/api/v2/flows/actions/publish?flow="+id, nil, nil)
	require.Equal(t, http.StatusOK, resp.StatusCode, "publish")
	var published map[string]any
	ts.GetJSON(t, "/api/v2/flows/"+id, &published)
	assert.Equal(t, "published", published["state"], "after publish state")
	if v, ok := published["lockedUser"]; ok {
		assert.Nil(t, v, "after publish lockedUser should be nil")
	}

	// S112 finding #9: PUT must NOT bypass the state machine.
	resp = ts.PutJSON(t, "/api/v2/flows/"+id,
		map[string]any{"name": "main-ivr", "state": "unpublished"}, nil)
	require.Equal(t, http.StatusOK, resp.StatusCode, "put bypass attempt")
	var afterBypass map[string]any
	ts.GetJSON(t, "/api/v2/flows/"+id, &afterBypass)
	assert.Equal(t, "published", afterBypass["state"],
		"PUT bypassed state machine: want still published")

	// delete
	resp = ts.DeleteJSON(t, "/api/v2/flows/"+id)
	assert.Equal(t, http.StatusNoContent, resp.StatusCode, "delete")
}

// S112 finding #11: empty multipart upload should 400.
func TestFlow_MultipartUploadEmpty400(t *testing.T) {
	ts := testutil.NewTestServer(t)
	var created map[string]any
	ts.PostJSON(t, "/api/v2/flows",
		map[string]any{"name": "empty-multipart-ivr"}, &created)
	id := created["id"].(string)

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	_ = mw.WriteField("metadata", "no-file-here")
	_ = mw.Close()

	req, _ := http.NewRequest(http.MethodPut, ts.HTTP.URL+"/api/v2/flows/"+id, &buf)
	req.Header.Set("Authorization", "Bearer "+ts.Token)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	resp, err := ts.HTTP.Client().Do(req)
	require.NoError(t, err, "put empty multipart")
	resp.Body.Close()
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode,
		"multipart with no file part")
}

func TestFlow_MultipartUploadRoundTrip(t *testing.T) {
	ts := testutil.NewTestServer(t)
	var created map[string]any
	ts.PostJSON(t, "/api/v2/flows",
		map[string]any{"name": "multipart-ivr"}, &created)
	id := created["id"].(string)

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	part, _ := mw.CreateFormFile("flowYaml", "flow.yaml")
	yaml := `flow:
  name: multipart-ivr
  start_state: greet
  states:
    greet:
      type: prompt
      next: end
    end:
      type: terminate
`
	_, _ = io.Copy(part, bytes.NewReader([]byte(yaml)))
	_ = mw.Close()

	req, _ := http.NewRequest(http.MethodPut, ts.HTTP.URL+"/api/v2/flows/"+id, &buf)
	req.Header.Set("Authorization", "Bearer "+ts.Token)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	resp, err := ts.HTTP.Client().Do(req)
	require.NoError(t, err, "put multipart")
	resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode, "put multipart")

	var fetched map[string]any
	ts.GetJSON(t, "/api/v2/flows/"+id, &fetched)
	assert.Equal(t, "flow.yaml", fetched["multipartFilename"], "filename round-trip")
	got, _ := fetched["multipartContent"].(string)
	assert.Equal(t, yaml, got, "content round-trip")
}

// --- responsemanagement_response -------------------------------------

func TestResponseManagement_Lifecycle(t *testing.T) {
	ts := testutil.NewTestServer(t)
	var created map[string]any
	resp := ts.PostJSON(t, "/api/v2/responsemanagement/responses",
		map[string]any{"name": "canned-greeting"}, &created)
	require.Equal(t, http.StatusCreated, resp.StatusCode, "create")
	id := created["id"].(string)
	resp = ts.PutJSON(t, "/api/v2/responsemanagement/responses/"+id,
		map[string]any{"name": "canned-greeting", "texts": []any{}}, nil)
	assert.Equal(t, http.StatusOK, resp.StatusCode, "update")
	resp = ts.DeleteJSON(t, "/api/v2/responsemanagement/responses/"+id)
	assert.Equal(t, http.StatusNoContent, resp.StatusCode, "delete")
}

// --- idp_generic (singleton) -----------------------------------------

func TestIDPGeneric_Singleton(t *testing.T) {
	ts := testutil.NewTestServer(t)
	// Initial GET → 404 (not configured).
	resp := ts.GetJSON(t, "/api/v2/identityproviders/generic", nil)
	assert.Equal(t, http.StatusNotFound, resp.StatusCode, "initial get")
	// PUT → 200.
	put := map[string]any{
		"name":      "Okta",
		"issuerURI": "https://example.okta.com",
	}
	var updated map[string]any
	resp = ts.PutJSON(t, "/api/v2/identityproviders/generic", put, &updated)
	require.Equal(t, http.StatusOK, resp.StatusCode, "put")
	// GET after PUT → 200 with the config.
	var fetched map[string]any
	ts.GetJSON(t, "/api/v2/identityproviders/generic", &fetched)
	assert.Equal(t, "https://example.okta.com", fetched["issuerURI"], "PUT lost")
	// DELETE → 204 + subsequent GET → 404.
	resp = ts.DeleteJSON(t, "/api/v2/identityproviders/generic")
	assert.Equal(t, http.StatusNoContent, resp.StatusCode, "delete")
	resp = ts.GetJSON(t, "/api/v2/identityproviders/generic", nil)
	assert.Equal(t, http.StatusNotFound, resp.StatusCode, "after delete")
}

// --- S123 contract tests ---------------------------------------------

// TestContract_architect_datatable_create_default_division asserts the
// wire-shape invariant that POST /api/v2/flows/datatables with no
// `division` field still round-trips a non-empty `division.id` on both
// the create response and the read-after-create. The provider's
// readArchitectDatatable derefs *datatable.Division.Id
// (resource_genesyscloud_architect_datatable.go:121); without a default
// division the plugin segfaults.
//
// Paired with CRITICAL[architect-datatable-create-default-division] in
// handlers/architect_datatable.go.
func TestContract_architect_datatable_create_default_division(t *testing.T) {
	ts := testutil.NewTestServer(t)
	var created map[string]any
	resp := ts.PostJSON(t, "/api/v2/flows/datatables",
		map[string]any{"name": "defaulted-dt"}, &created)
	require.Equal(t, http.StatusCreated, resp.StatusCode, "create")
	assertNonEmptyDivisionID(t, created, "create response")

	id, _ := created["id"].(string)
	var got map[string]any
	resp = ts.GetJSON(t, "/api/v2/flows/datatables/"+id, &got)
	require.Equal(t, http.StatusOK, resp.StatusCode, "read-after-create")
	assertNonEmptyDivisionID(t, got, "read-after-create response")
}

// TestContract_flow_jobs_upload_protocol asserts the 3-step upload-job
// protocol the genesyscloud_flow resource uses:
//
//  1. POST /api/v2/flows/jobs → 200 with `presignedUrl` (pointing at
//     a NO_PROXY host so the upload doesn't loop back through MITM) +
//     `id` (jobId).
//  2. PUT to presignedUrl with the YAML payload → 2xx.
//  3. GET /api/v2/flows/jobs/{jobId} → 200 with status="Success" +
//     non-empty flow.id.
//
// Paired with CRITICAL[flow-jobs-upload-protocol] in handlers/flow.go.
func TestContract_flow_jobs_upload_protocol(t *testing.T) {
	ts := testutil.NewTestServer(t)

	// Step 1: create job.
	var job map[string]any
	resp := ts.PostJSON(t, "/api/v2/flows/jobs", map[string]any{}, &job)
	require.Equal(t, http.StatusOK, resp.StatusCode, "POST /flows/jobs")
	jobID, _ := job["id"].(string)
	require.NotEmpty(t, jobID, "POST /flows/jobs: response missing id")
	uploadURL, _ := job["presignedUrl"].(string)
	require.NotEmpty(t, uploadURL, "POST /flows/jobs: response missing presignedUrl")
	// presignedUrl MUST point at a NO_PROXY host (localhost / 127.0.0.1)
	// or the upload loops back through the MITM proxy and fails.
	hasLocalhost := bytes.Contains([]byte(uploadURL), []byte("localhost")) ||
		bytes.Contains([]byte(uploadURL), []byte("127.0.0.1"))
	require.True(t, hasLocalhost,
		"presignedUrl %q must point at localhost or 127.0.0.1 (NO_PROXY host)", uploadURL)

	// Step 2: upload.
	req, err := http.NewRequest(http.MethodPut, uploadURL,
		bytes.NewReader([]byte("inboundCall:\n  name: fake-flow\n")))
	require.NoError(t, err, "upload NewRequest")
	req.Header.Set("Content-Type", "application/octet-stream")
	upResp, err := http.DefaultClient.Do(req)
	require.NoError(t, err, "upload Do")
	defer upResp.Body.Close()
	if upResp.StatusCode < 200 || upResp.StatusCode >= 300 {
		body, _ := io.ReadAll(upResp.Body)
		t.Fatalf("upload PUT: status %d; body: %s", upResp.StatusCode, body)
	}

	// Step 3: poll job.
	var poll map[string]any
	resp = ts.GetJSON(t, "/api/v2/flows/jobs/"+jobID, &poll)
	require.Equal(t, http.StatusOK, resp.StatusCode, "GET /flows/jobs/{id}")
	status, _ := poll["status"].(string)
	assert.Equal(t, "Success", status, "job status (provider polls until Success)")
	flow, _ := poll["flow"].(map[string]any)
	require.NotNil(t, flow, "job poll missing 'flow' object")
	flowID, _ := flow["id"].(string)
	assert.NotEmpty(t, flowID, "job poll: flow.id is empty (provider reads it into state)")
}

// TestContract_responsemanagement_library_crud_round_trip asserts the
// library resource — which is the parent container the provider
// requires before any responsemanagement_response can be created.
// POST returns 200 with a non-empty id; subsequent GET returns the
// stored library.
//
// Paired with CRITICAL[responsemanagement-library-crud-round-trip] in
// handlers/responsemanagement.go.
func TestContract_responsemanagement_library_crud_round_trip(t *testing.T) {
	ts := testutil.NewTestServer(t)
	var created map[string]any
	resp := ts.PostJSON(t, "/api/v2/responsemanagement/libraries",
		map[string]any{"name": "lib1"}, &created)
	// Real Genesys returns 200 (not 201) for library create; the
	// provider's CreateContext gates on 2xx and reads body.id either way,
	// but the contract is 200.
	require.Contains(t, []int{http.StatusOK, http.StatusCreated}, resp.StatusCode,
		"POST /libraries: status %d, want 2xx", resp.StatusCode)
	id, _ := created["id"].(string)
	require.NotEmpty(t, id, "POST /libraries: response missing id")
	name, _ := created["name"].(string)
	assert.Equal(t, "lib1", name, "POST /libraries: name round-trip")

	var got map[string]any
	resp = ts.GetJSON(t, "/api/v2/responsemanagement/libraries/"+id, &got)
	require.Equal(t, http.StatusOK, resp.StatusCode, "GET /libraries/{id}")
	gotID, _ := got["id"].(string)
	assert.Equal(t, id, gotID, "GET id")
	gotName, _ := got["name"].(string)
	assert.Equal(t, "lib1", gotName, "GET name")
}
