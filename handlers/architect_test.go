package handlers_test

import (
	"bytes"
	"io"
	"mime/multipart"
	"net/http"
	"testing"

	"github.com/redscaresu/fakegenesys/testutil"
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
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create: status %d", resp.StatusCode)
	}
	id := created["id"].(string)

	resp = ts.PutJSON(t, "/api/v2/flows/datatables/"+id,
		map[string]any{"name": "lookup-table", "description": "updated"}, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("put: status %d", resp.StatusCode)
	}

	resp = ts.DeleteJSON(t, "/api/v2/flows/datatables/"+id)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("delete: status %d", resp.StatusCode)
	}
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
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("row create: status %d", resp.StatusCode)
	}
	// duplicate key → 409
	resp = ts.PostJSON(t, "/api/v2/flows/datatables/"+dtID+"/rows",
		map[string]any{"key": "row1", "value": "b"}, nil)
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("dup row: status %d, want 409", resp.StatusCode)
	}
	// list
	var listing struct {
		Total int `json:"total"`
	}
	ts.GetJSON(t, "/api/v2/flows/datatables/"+dtID+"/rows", &listing)
	if listing.Total != 1 {
		t.Fatalf("list total = %d, want 1", listing.Total)
	}
	// cascade delete: drop datatable, rows go away.
	resp = ts.DeleteJSON(t, "/api/v2/flows/datatables/"+dtID)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("dt delete: status %d", resp.StatusCode)
	}
	resp = ts.GetJSON(t, "/api/v2/flows/datatables/"+dtID+"/rows", nil)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("rows on deleted dt: status %d, want 404", resp.StatusCode)
	}
}

// --- architect_user_prompt -------------------------------------------

func TestUserPrompt_Lifecycle(t *testing.T) {
	ts := testutil.NewTestServer(t)
	var created map[string]any
	resp := ts.PostJSON(t, "/api/v2/architect/prompts",
		map[string]any{"name": "welcome-prompt"}, &created)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create: status %d", resp.StatusCode)
	}
	id := created["id"].(string)
	resp = ts.PutJSON(t, "/api/v2/architect/prompts/"+id,
		map[string]any{"description": "Greet caller"}, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("update: status %d", resp.StatusCode)
	}
	resp = ts.DeleteJSON(t, "/api/v2/architect/prompts/"+id)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("delete: status %d", resp.StatusCode)
	}
}

func TestUserPrompt_DuplicateName409(t *testing.T) {
	ts := testutil.NewTestServer(t)
	ts.PostJSON(t, "/api/v2/architect/prompts",
		map[string]any{"name": "dup-prompt"}, nil)
	resp := ts.PostJSON(t, "/api/v2/architect/prompts",
		map[string]any{"name": "dup-prompt"}, nil)
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("status = %d, want 409", resp.StatusCode)
	}
}

// --- flow ------------------------------------------------------------

func TestFlow_LifecycleAndStateMachine(t *testing.T) {
	ts := testutil.NewTestServer(t)
	var created map[string]any
	resp := ts.PostJSON(t, "/api/v2/flows",
		map[string]any{"name": "main-ivr", "type": "inboundcall"}, &created)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create: status %d", resp.StatusCode)
	}
	id := created["id"].(string)
	if created["state"] != "unpublished" {
		t.Fatalf("initial state = %v, want unpublished", created["state"])
	}

	// checkout → locked
	resp = ts.PostJSON(t, "/api/v2/flows/actions/checkout?flow="+id, nil, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("checkout: status %d", resp.StatusCode)
	}
	var locked map[string]any
	ts.GetJSON(t, "/api/v2/flows/"+id, &locked)
	if locked["state"] != "locked" {
		t.Fatalf("after checkout state = %v, want locked", locked["state"])
	}

	// S112 finding #10a: checkin → unpublished + cleared lock.
	resp = ts.PostJSON(t, "/api/v2/flows/actions/checkin?flow="+id, nil, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("checkin: status %d", resp.StatusCode)
	}
	var checkedIn map[string]any
	ts.GetJSON(t, "/api/v2/flows/"+id, &checkedIn)
	if checkedIn["state"] != "unpublished" {
		t.Fatalf("after checkin state = %v, want unpublished", checkedIn["state"])
	}
	if v, ok := checkedIn["lockedUser"]; ok && v != nil {
		t.Fatalf("after checkin lockedUser should be nil, got %v", v)
	}

	// Re-lock, then force-unlock.
	ts.PostJSON(t, "/api/v2/flows/actions/checkout?flow="+id, nil, nil)
	// S112 finding #10b: unlock from locked → unpublished + cleared lock.
	resp = ts.PostJSON(t, "/api/v2/flows/actions/unlock?flow="+id, nil, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("unlock: status %d", resp.StatusCode)
	}
	var unlocked map[string]any
	ts.GetJSON(t, "/api/v2/flows/"+id, &unlocked)
	if unlocked["state"] != "unpublished" {
		t.Fatalf("after unlock state = %v, want unpublished", unlocked["state"])
	}

	// publish → published + cleared lock
	resp = ts.PostJSON(t, "/api/v2/flows/actions/publish?flow="+id, nil, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("publish: status %d", resp.StatusCode)
	}
	var published map[string]any
	ts.GetJSON(t, "/api/v2/flows/"+id, &published)
	if published["state"] != "published" {
		t.Fatalf("after publish state = %v, want published", published["state"])
	}
	if v, ok := published["lockedUser"]; ok && v != nil {
		t.Fatalf("after publish lockedUser should be nil, got %v", v)
	}

	// S112 finding #9: PUT must NOT bypass the state machine.
	resp = ts.PutJSON(t, "/api/v2/flows/"+id,
		map[string]any{"name": "main-ivr", "state": "unpublished"}, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("put bypass attempt: status %d", resp.StatusCode)
	}
	var afterBypass map[string]any
	ts.GetJSON(t, "/api/v2/flows/"+id, &afterBypass)
	if afterBypass["state"] != "published" {
		t.Fatalf("PUT bypassed state machine: state = %v, want still published",
			afterBypass["state"])
	}

	// delete
	resp = ts.DeleteJSON(t, "/api/v2/flows/"+id)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("delete: status %d", resp.StatusCode)
	}
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
	if err != nil {
		t.Fatalf("put empty multipart: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (multipart with no file part)", resp.StatusCode)
	}
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
	if err != nil {
		t.Fatalf("put multipart: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("put multipart: status %d", resp.StatusCode)
	}

	var fetched map[string]any
	ts.GetJSON(t, "/api/v2/flows/"+id, &fetched)
	if fetched["multipartFilename"] != "flow.yaml" {
		t.Fatalf("filename round-trip lost: %v", fetched["multipartFilename"])
	}
	if got, _ := fetched["multipartContent"].(string); got != yaml {
		t.Fatalf("content round-trip lost; got %q", got)
	}
}

// --- responsemanagement_response -------------------------------------

func TestResponseManagement_Lifecycle(t *testing.T) {
	ts := testutil.NewTestServer(t)
	var created map[string]any
	resp := ts.PostJSON(t, "/api/v2/responsemanagement/responses",
		map[string]any{"name": "canned-greeting"}, &created)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create: status %d", resp.StatusCode)
	}
	id := created["id"].(string)
	resp = ts.PutJSON(t, "/api/v2/responsemanagement/responses/"+id,
		map[string]any{"name": "canned-greeting", "texts": []any{}}, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("update: status %d", resp.StatusCode)
	}
	resp = ts.DeleteJSON(t, "/api/v2/responsemanagement/responses/"+id)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("delete: status %d", resp.StatusCode)
	}
}

// --- idp_generic (singleton) -----------------------------------------

func TestIDPGeneric_Singleton(t *testing.T) {
	ts := testutil.NewTestServer(t)
	// Initial GET → 404 (not configured).
	resp := ts.GetJSON(t, "/api/v2/identityproviders/generic", nil)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("initial get: status %d, want 404", resp.StatusCode)
	}
	// PUT → 200.
	put := map[string]any{
		"name":     "Okta",
		"issuerURI": "https://example.okta.com",
	}
	var updated map[string]any
	resp = ts.PutJSON(t, "/api/v2/identityproviders/generic", put, &updated)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("put: status %d", resp.StatusCode)
	}
	// GET after PUT → 200 with the config.
	var fetched map[string]any
	ts.GetJSON(t, "/api/v2/identityproviders/generic", &fetched)
	if fetched["issuerURI"] != "https://example.okta.com" {
		t.Fatalf("PUT lost: %v", fetched)
	}
	// DELETE → 204 + subsequent GET → 404.
	resp = ts.DeleteJSON(t, "/api/v2/identityproviders/generic")
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("delete: status %d", resp.StatusCode)
	}
	resp = ts.GetJSON(t, "/api/v2/identityproviders/generic", nil)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("after delete: status %d, want 404", resp.StatusCode)
	}
}
