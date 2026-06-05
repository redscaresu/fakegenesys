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
	r.Post("/flows", app.handleFlowCreate)
	r.Get("/flows", app.handleFlowList)
	r.Get("/flows/{flowId}", app.handleFlowGet)
	r.Put("/flows/{flowId}", app.handleFlowUpdate)
	r.Delete("/flows/{flowId}", app.handleFlowDelete)
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
			}
		}
	} else {
		patch, err := decodeJSONBody(r)
		if err != nil {
			writeBadRequest(w, "body: "+err.Error())
			return
		}
		for k, v := range patch {
			if k == "id" || k == "selfUri" {
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
		writeNotFound(w, "flow")
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
