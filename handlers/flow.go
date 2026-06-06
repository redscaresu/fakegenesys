package handlers

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/redscaresu/fakegenesys/models"
)

// flow — `/api/v2/flows` (POST create + GET list) + `/{flowId}`
// (GET/PUT/DELETE) + lock/publish state machine via
// `/api/v2/flows/actions/{checkout,checkin,publish,unlock}?flow={id}`.
//
// Wire shape:
//   POST  /api/v2/flows                            → 201 with id + state=unpublished
//   GET   /api/v2/flows                            → paged list
//   GET   /api/v2/flows/{flowId}                   → 200 single flow body
//   PUT   /api/v2/flows/{flowId}                   → 200 merged update (accepts
//                                                    application/json or multipart/form-data)
//   DELETE /api/v2/flows/{flowId}                  → 204
//   POST  /api/v2/flows/actions/checkout?flow=ID   → 200 lock for editing
//   POST  /api/v2/flows/actions/checkin?flow=ID    → 200 save edits
//   POST  /api/v2/flows/actions/publish?flow=ID    → 200 transition unpublished/locked → published
//   POST  /api/v2/flows/actions/unlock?flow=ID     → 200 force-unlock
//
// Multipart upload on PUT: fakegenesys reads either application/json
// OR multipart/form-data. In the multipart case, the file part (any
// name) is persisted opaquely in body["multipartContent"] so a
// subsequent GET returns it. No YAML parsing happens — the Terraform
// provider supplies whatever shape the real Genesys interpreter
// accepts.

func (app *Application) registerFlowRoutes(r chi.Router) {
	// Action routes register FIRST so they match before /flows/{flowId}.
	r.Post("/flows/actions/checkout", app.handleFlowCheckout)
	r.Post("/flows/actions/checkin", app.handleFlowCheckin)
	r.Post("/flows/actions/publish", app.handleFlowPublish)
	r.Post("/flows/actions/unlock", app.handleFlowUnlock)
	r.Post("/flows/actions/revert", app.handleFlowUnlock)     // alias
	r.Post("/flows/actions/deactivate", app.handleFlowUnlock) // alias
	// S122: the upload-job protocol the genesyscloud provider uses for
	// `genesyscloud_flow`. The provider does not POST the flow body
	// directly to /flows; it asks the API for a presignedUrl, uploads
	// the YAML there, then polls the job until success. fakegenesys
	// fakes this by returning an in-process upload URL on the same
	// port; the upload handler stores the bytes and the job-poll
	// returns success + a real flow.id allocated at job-create time.
	r.Post("/flows/jobs", app.handleFlowJobCreate)
	r.Get("/flows/jobs/{jobId}", app.handleFlowJobGet)
	r.Post("/flows", app.handleFlowCreate)
	r.Get("/flows", app.handleFlowList)
	r.Get("/flows/{flowId}", app.handleFlowGet)
	r.Put("/flows/{flowId}", app.handleFlowUpdate)
	r.Delete("/flows/{flowId}", app.handleFlowDelete)
}

// handleFlowJobCreate kicks off the multi-step flow upload. We allocate
// both a job id and a flow id eagerly so the polling phase has a real
// flow to return. The presignedUrl points back at fakegenesys's
// /mock/flow-upload/{jobId} endpoint (registered out-of-band in
// admin.go so it's reachable without bearer auth — matching real
// Genesys's S3 pattern where the upload URL is signed and doesn't take
// the Bearer token).
func (app *Application) handleFlowJobCreate(w http.ResponseWriter, r *http.Request) {
	jobID := newID()
	flowID := newID()
	// The infrafactory cloudEnv NO_PROXY includes localhost + 127.0.0.1
	// so the upload won't try to go back through the MITM proxy.
	uploadURL := "http://localhost:8083/mock/flow-upload/" + jobID
	// Track the (jobID -> flowID) pairing so the eventual GET can
	// return the right flow.
	app.recordFlowJob(jobID, flowID)
	// Also pre-create the flow row so subsequent GET /flows/{id}
	// after the job succeeds finds it.
	flowBody := map[string]any{
		"id":      flowID,
		"name":    "harness-flow-" + flowID,
		"type":    "inboundCall",
		"state":   "unpublished",
		"selfUri": "/api/v2/flows/" + flowID,
		"division": map[string]any{
			"id":   fakegenesysHomeDivisionID,
			"name": "Home",
		},
	}
	enc, _ := json.Marshal(flowBody)
	_, _ = app.repo.DB().Exec(
		`INSERT INTO flows(id, name, type, state, body) VALUES (?, ?, ?, ?, ?)`,
		flowID, flowBody["name"], "inboundCall", "unpublished", string(enc))
	writeJSONStatus(w, http.StatusOK, map[string]any{
		"id":           jobID,
		"presignedUrl": uploadURL,
		"headers": map[string]string{
			"Content-Type": "application/octet-stream",
		},
	})
}

func (app *Application) handleFlowJobGet(w http.ResponseWriter, r *http.Request) {
	jobID := chi.URLParam(r, "jobId")
	flowID := app.lookupFlowJob(jobID)
	if flowID == "" {
		writeError(w, http.StatusNotFound, "not_found", "no such job")
		return
	}
	writeJSONStatus(w, http.StatusOK, map[string]any{
		"id":     jobID,
		"status": "Success",
		"flow": map[string]any{
			"id":      flowID,
			"selfUri": "/api/v2/flows/" + flowID,
		},
		"messages": []any{},
	})
}

func (app *Application) handleFlowCreate(w http.ResponseWriter, r *http.Request) {
	body, err := decodeJSONBody(r)
	if err != nil {
		writeBadRequest(w, "body: "+err.Error())
		return
	}
	if err := requireStringFields(body, "name"); err != nil {
		writeBadRequest(w, err.Error())
		return
	}
	id := newID()
	flowType := "inboundcall"
	if t, ok := body["type"].(string); ok && t != "" {
		flowType = t
	}
	body["id"] = id
	body["type"] = flowType
	body["selfUri"] = "/api/v2/flows/" + id
	body["state"] = "unpublished"
	body["lockedUser"] = nil
	enc, _ := json.Marshal(body)
	_, err = app.repo.DB().Exec(
		`INSERT INTO flows(id, name, type, state, body) VALUES (?, ?, ?, ?, ?)`,
		id, body["name"].(string), flowType, "unpublished", string(enc),
	)
	if err != nil {
		if isUniqueViolation(err) {
			writeConflict(w, "flow with name "+body["name"].(string)+" already exists")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	writeJSONStatus(w, http.StatusCreated, json.RawMessage(enc))
}

func (app *Application) handleFlowList(w http.ResponseWriter, r *http.Request) {
	rows, err := listAllJSON(app.repo.DB(),
		`SELECT body FROM flows ORDER BY name`)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	pagedList(w, r, rows)
}

func (app *Application) handleFlowGet(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "flowId")
	raw, err := scanOneJSON(app.repo.DB(),
		`SELECT body FROM flows WHERE id = ?`, id)
	if err != nil {
		if errors.Is(err, models.ErrNotFound) {
			writeNotFound(w, "flow")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	writeJSONStatus(w, http.StatusOK, raw)
}

func (app *Application) handleFlowUpdate(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "flowId")
	existingRaw, err := scanOneJSON(app.repo.DB(),
		`SELECT body FROM flows WHERE id = ?`, id)
	if err != nil {
		if errors.Is(err, models.ErrNotFound) {
			writeNotFound(w, "flow")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	var existing map[string]any
	_ = json.Unmarshal(existingRaw, &existing)

	ct := r.Header.Get("Content-Type")
	if strings.HasPrefix(ct, "multipart/form-data") {
		// Multipart upload of flow YAML. Persist the file content opaquely.
		if err := r.ParseMultipartForm(32 << 20); err != nil {
			writeBadRequest(w, "multipart parse: "+err.Error())
			return
		}
		gotFile := false
		for _, fhs := range r.MultipartForm.File {
			for _, fh := range fhs {
				f, err := fh.Open()
				if err != nil {
					writeBadRequest(w, "multipart open: "+err.Error())
					return
				}
				b, err := io.ReadAll(f)
				_ = f.Close()
				if err != nil {
					writeBadRequest(w, "multipart read: "+err.Error())
					return
				}
				existing["multipartContent"] = string(b)
				existing["multipartFilename"] = fh.Filename
				gotFile = true
			}
		}
		// S112 finding #11: silent no-op on empty multipart is a
		// production-shaped bug. Surface explicitly.
		if !gotFile {
			writeBadRequest(w, "multipart upload missing file part")
			return
		}
	} else {
		patch, err := decodeJSONBody(r)
		if err != nil {
			writeBadRequest(w, "body: "+err.Error())
			return
		}
		// S112 finding #9: state + lockedUser are owned by the
		// /flows/actions/* state machine. Don't let a PUT bypass it.
		for k, v := range patch {
			if k == "id" || k == "selfUri" || k == "state" || k == "lockedUser" {
				continue
			}
			existing[k] = v
		}
	}
	enc, _ := json.Marshal(existing)
	name, _ := existing["name"].(string)
	state, _ := existing["state"].(string)
	if state == "" {
		state = "unpublished"
	}
	_, err = app.repo.DB().Exec(
		`UPDATE flows SET name = ?, state = ?, body = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`,
		name, state, string(enc), id,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	writeJSONStatus(w, http.StatusOK, json.RawMessage(enc))
}

func (app *Application) handleFlowDelete(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "flowId")
	res, err := app.repo.DB().Exec(`DELETE FROM flows WHERE id = ?`, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		writeNotFound(w, "flow")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// flowAction is the shared shape for checkout/checkin/publish/unlock.
// Genesys passes the flow id as `?flow=<id>` query.
func (app *Application) flowAction(w http.ResponseWriter, r *http.Request, newState, lockedUser string) {
	flowID := r.URL.Query().Get("flow")
	if flowID == "" {
		writeBadRequest(w, "missing required query param: flow")
		return
	}
	existingRaw, err := scanOneJSON(app.repo.DB(),
		`SELECT body FROM flows WHERE id = ?`, flowID)
	if err != nil {
		// S112 finding #4: branch on ErrNotFound, don't mask DB
		// failures as 404s.
		if errors.Is(err, models.ErrNotFound) {
			writeNotFound(w, "flow")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	var existing map[string]any
	_ = json.Unmarshal(existingRaw, &existing)
	existing["state"] = newState
	if lockedUser != "" {
		existing["lockedUser"] = map[string]any{"id": lockedUser}
	} else {
		existing["lockedUser"] = nil
	}
	enc, _ := json.Marshal(existing)
	if _, err := app.repo.DB().Exec(
		`UPDATE flows SET state = ?, locked_user_id = ?, body = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`,
		newState, lockedUser, string(enc), flowID,
	); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	writeJSONStatus(w, http.StatusOK, json.RawMessage(enc))
}

func (app *Application) handleFlowCheckout(w http.ResponseWriter, r *http.Request) {
	app.flowAction(w, r, "locked", "fake-tf-user")
}
func (app *Application) handleFlowCheckin(w http.ResponseWriter, r *http.Request) {
	app.flowAction(w, r, "unpublished", "")
}
func (app *Application) handleFlowPublish(w http.ResponseWriter, r *http.Request) {
	app.flowAction(w, r, "published", "")
}
func (app *Application) handleFlowUnlock(w http.ResponseWriter, r *http.Request) {
	app.flowAction(w, r, "unpublished", "")
}
