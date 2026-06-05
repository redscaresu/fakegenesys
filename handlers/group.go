package handlers

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/redscaresu/fakegenesys/models"
)

// Genesys group resource — POST/GET /api/v2/groups + GET/PUT/DELETE
// /api/v2/groups/{groupId}. Hard delete (not soft like users); the
// Terraform provider's destroy waits for the GET to return 404.

func (app *Application) registerGroupRoutes(r chi.Router) {
	r.Post("/groups", app.handleGroupCreate)
	r.Get("/groups", app.handleGroupList)
	r.Get("/groups/{groupId}", app.handleGroupGet)
	r.Put("/groups/{groupId}", app.handleGroupUpdate)
	r.Delete("/groups/{groupId}", app.handleGroupDelete)
}

func (app *Application) handleGroupCreate(w http.ResponseWriter, r *http.Request) {
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
	groupType := "official"
	if s, ok := body["type"].(string); ok && s != "" {
		groupType = s
	}
	body["id"] = id
	body["type"] = groupType
	body["selfUri"] = "/api/v2/groups/" + id
	body["version"] = 1
	body["dateCreated"] = nowZ()
	body["dateModified"] = body["dateCreated"]
	enc, _ := json.Marshal(body)
	_, err = app.repo.DB().Exec(
		`INSERT INTO groups(id, name, type, body) VALUES (?, ?, ?, ?)`,
		id, body["name"].(string), groupType, string(enc),
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	writeJSONStatus(w, http.StatusCreated, json.RawMessage(enc))
}

func (app *Application) handleGroupList(w http.ResponseWriter, r *http.Request) {
	rows, err := listAllJSON(app.repo.DB(),
		`SELECT body FROM groups ORDER BY name`)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	pagedList(w, r, rows)
}

func (app *Application) handleGroupGet(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "groupId")
	raw, err := scanOneJSON(app.repo.DB(),
		`SELECT body FROM groups WHERE id = ?`, id)
	if err != nil {
		if errors.Is(err, models.ErrNotFound) {
			writeNotFound(w, "group")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	writeJSONStatus(w, http.StatusOK, raw)
}

func (app *Application) handleGroupUpdate(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "groupId")
	body, err := decodeJSONBody(r)
	if err != nil {
		writeBadRequest(w, "body: "+err.Error())
		return
	}
	existingRaw, err := scanOneJSON(app.repo.DB(),
		`SELECT body FROM groups WHERE id = ?`, id)
	if err != nil {
		if errors.Is(err, models.ErrNotFound) {
			writeNotFound(w, "group")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	var existing map[string]any
	_ = json.Unmarshal(existingRaw, &existing)
	for k, v := range body {
		switch k {
		case "id", "selfUri", "dateCreated", "version":
		default:
			existing[k] = v
		}
	}
	existing["dateModified"] = nowZ()
	if v, ok := existing["version"].(float64); ok {
		existing["version"] = int(v) + 1
	} else {
		existing["version"] = 2
	}
	enc, _ := json.Marshal(existing)
	name, _ := existing["name"].(string)
	gType, _ := existing["type"].(string)
	if gType == "" {
		gType = "official"
	}
	_, err = app.repo.DB().Exec(
		`UPDATE groups SET name = ?, type = ?, body = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`,
		name, gType, string(enc), id,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	writeJSONStatus(w, http.StatusOK, json.RawMessage(enc))
}

func (app *Application) handleGroupDelete(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "groupId")
	res, err := app.repo.DB().Exec(`DELETE FROM groups WHERE id = ?`, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		writeNotFound(w, "group")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
