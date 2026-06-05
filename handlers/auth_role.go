package handlers

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/redscaresu/fakegenesys/models"
)

// Genesys authorization role resource —
// POST/GET /api/v2/authorization/roles +
// GET/PUT/PATCH/DELETE /api/v2/authorization/roles/{roleId}.
//
// Permissions are well-known strings (e.g. "routing:queue:edit").
// fakegenesys doesn't validate the permission grammar — that's a
// fidelity-vs-reverse-fidelity decision: the real provider's
// permissions list shifts across Genesys releases and OpenAPI doesn't
// pin them. Per AGENTS.md § "Fidelity strategy", we don't add
// validation the spec doesn't declare.

func (app *Application) registerAuthRoleRoutes(r chi.Router) {
	r.Post("/authorization/roles", app.handleAuthRoleCreate)
	r.Get("/authorization/roles", app.handleAuthRoleList)
	r.Get("/authorization/roles/{roleId}", app.handleAuthRoleGet)
	r.Put("/authorization/roles/{roleId}", app.handleAuthRoleUpdate)
	r.Patch("/authorization/roles/{roleId}", app.handleAuthRoleUpdate)
	r.Delete("/authorization/roles/{roleId}", app.handleAuthRoleDelete)
}

func (app *Application) handleAuthRoleCreate(w http.ResponseWriter, r *http.Request) {
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
	if _, ok := body["permissions"]; !ok {
		body["permissions"] = []any{}
	}
	body["id"] = id
	body["selfUri"] = "/api/v2/authorization/roles/" + id
	enc, _ := json.Marshal(body)
	_, err = app.repo.DB().Exec(
		`INSERT INTO auth_roles(id, name, body) VALUES (?, ?, ?)`,
		id, body["name"].(string), string(enc),
	)
	if err != nil {
		if isUniqueViolation(err) {
			writeConflict(w, "auth_role with name "+body["name"].(string)+" already exists")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	writeJSONStatus(w, http.StatusCreated, json.RawMessage(enc))
}

func (app *Application) handleAuthRoleList(w http.ResponseWriter, r *http.Request) {
	rows, err := listAllJSON(app.repo.DB(),
		`SELECT body FROM auth_roles ORDER BY name`)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	pagedList(w, r, rows)
}

func (app *Application) handleAuthRoleGet(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "roleId")
	raw, err := scanOneJSON(app.repo.DB(),
		`SELECT body FROM auth_roles WHERE id = ?`, id)
	if err != nil {
		if errors.Is(err, models.ErrNotFound) {
			writeNotFound(w, "auth_role")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	writeJSONStatus(w, http.StatusOK, raw)
}

func (app *Application) handleAuthRoleUpdate(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "roleId")
	body, err := decodeJSONBody(r)
	if err != nil {
		writeBadRequest(w, "body: "+err.Error())
		return
	}
	existingRaw, err := scanOneJSON(app.repo.DB(),
		`SELECT body FROM auth_roles WHERE id = ?`, id)
	if err != nil {
		if errors.Is(err, models.ErrNotFound) {
			writeNotFound(w, "auth_role")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	var existing map[string]any
	_ = json.Unmarshal(existingRaw, &existing)
	for k, v := range body {
		if k == "id" || k == "selfUri" {
			continue
		}
		existing[k] = v
	}
	enc, _ := json.Marshal(existing)
	name, _ := existing["name"].(string)
	_, err = app.repo.DB().Exec(
		`UPDATE auth_roles SET name = ?, body = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`,
		name, string(enc), id,
	)
	if err != nil {
		if isUniqueViolation(err) {
			writeConflict(w, "auth_role with name "+name+" already exists")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	writeJSONStatus(w, http.StatusOK, json.RawMessage(enc))
}

func (app *Application) handleAuthRoleDelete(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "roleId")
	res, err := app.repo.DB().Exec(`DELETE FROM auth_roles WHERE id = ?`, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		writeNotFound(w, "auth_role")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
