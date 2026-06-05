package handlers

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/redscaresu/fakegenesys/models"
)

// routing/wrapupcodes — POST/GET + GET/PUT/DELETE. Unique name.

func (app *Application) registerRoutingWrapupcodeRoutes(r chi.Router) {
	r.Post("/routing/wrapupcodes", app.handleRoutingWrapupcodeCreate)
	r.Get("/routing/wrapupcodes", app.handleRoutingWrapupcodeList)
	r.Get("/routing/wrapupcodes/{codeId}", app.handleRoutingWrapupcodeGet)
	r.Put("/routing/wrapupcodes/{codeId}", app.handleRoutingWrapupcodeUpdate)
	r.Delete("/routing/wrapupcodes/{codeId}", app.handleRoutingWrapupcodeDelete)
}

func (app *Application) handleRoutingWrapupcodeCreate(w http.ResponseWriter, r *http.Request) {
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
	body["id"] = id
	body["selfUri"] = "/api/v2/routing/wrapupcodes/" + id
	enc, _ := json.Marshal(body)
	_, err = app.repo.DB().Exec(
		`INSERT INTO routing_wrapupcodes(id, name, body) VALUES (?, ?, ?)`,
		id, body["name"].(string), string(enc),
	)
	if err != nil {
		if isUniqueViolation(err) {
			writeConflict(w, "wrapupcode with name "+body["name"].(string)+" already exists")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	writeJSONStatus(w, http.StatusCreated, json.RawMessage(enc))
}

func (app *Application) handleRoutingWrapupcodeList(w http.ResponseWriter, r *http.Request) {
	rows, err := listAllJSON(app.repo.DB(),
		`SELECT body FROM routing_wrapupcodes ORDER BY name`)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	pagedList(w, r, rows)
}

func (app *Application) handleRoutingWrapupcodeGet(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "codeId")
	raw, err := scanOneJSON(app.repo.DB(),
		`SELECT body FROM routing_wrapupcodes WHERE id = ?`, id)
	if err != nil {
		if errors.Is(err, models.ErrNotFound) {
			writeNotFound(w, "routing_wrapupcode")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	writeJSONStatus(w, http.StatusOK, raw)
}

func (app *Application) handleRoutingWrapupcodeUpdate(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "codeId")
	patch, err := decodeJSONBody(r)
	if err != nil {
		writeBadRequest(w, "body: "+err.Error())
		return
	}
	existingRaw, err := scanOneJSON(app.repo.DB(),
		`SELECT body FROM routing_wrapupcodes WHERE id = ?`, id)
	if err != nil {
		if errors.Is(err, models.ErrNotFound) {
			writeNotFound(w, "routing_wrapupcode")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	var existing map[string]any
	_ = json.Unmarshal(existingRaw, &existing)
	for k, v := range patch {
		if k == "id" || k == "selfUri" {
			continue
		}
		existing[k] = v
	}
	enc, _ := json.Marshal(existing)
	name, _ := existing["name"].(string)
	_, err = app.repo.DB().Exec(
		`UPDATE routing_wrapupcodes SET name = ?, body = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`,
		name, string(enc), id,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	writeJSONStatus(w, http.StatusOK, json.RawMessage(enc))
}

func (app *Application) handleRoutingWrapupcodeDelete(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "codeId")
	res, err := app.repo.DB().Exec(`DELETE FROM routing_wrapupcodes WHERE id = ?`, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		writeNotFound(w, "routing_wrapupcode")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
