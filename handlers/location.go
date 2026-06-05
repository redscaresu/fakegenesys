package handlers

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/redscaresu/fakegenesys/models"
)

// Genesys location resource — POST/GET /api/v2/locations +
// GET/PATCH/DELETE /api/v2/locations/{locationId}.

func (app *Application) registerLocationRoutes(r chi.Router) {
	r.Post("/locations", app.handleLocationCreate)
	r.Get("/locations", app.handleLocationList)
	r.Get("/locations/{locationId}", app.handleLocationGet)
	r.Patch("/locations/{locationId}", app.handleLocationUpdate)
	r.Delete("/locations/{locationId}", app.handleLocationDelete)
}

func (app *Application) handleLocationCreate(w http.ResponseWriter, r *http.Request) {
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
	body["selfUri"] = "/api/v2/locations/" + id
	body["version"] = 1
	body["state"] = stateOr(body, "active")
	enc, _ := json.Marshal(body)
	_, err = app.repo.DB().Exec(
		`INSERT INTO locations(id, name, body) VALUES (?, ?, ?)`,
		id, body["name"].(string), string(enc),
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	writeJSONStatus(w, http.StatusCreated, json.RawMessage(enc))
}

func (app *Application) handleLocationList(w http.ResponseWriter, r *http.Request) {
	rows, err := listAllJSON(app.repo.DB(),
		`SELECT body FROM locations ORDER BY name`)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	pagedList(w, r, rows)
}

func (app *Application) handleLocationGet(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "locationId")
	raw, err := scanOneJSON(app.repo.DB(),
		`SELECT body FROM locations WHERE id = ?`, id)
	if err != nil {
		if errors.Is(err, models.ErrNotFound) {
			writeNotFound(w, "location")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	writeJSONStatus(w, http.StatusOK, raw)
}

func (app *Application) handleLocationUpdate(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "locationId")
	patch, err := decodeJSONBody(r)
	if err != nil {
		writeBadRequest(w, "body: "+err.Error())
		return
	}
	existingRaw, err := scanOneJSON(app.repo.DB(),
		`SELECT body FROM locations WHERE id = ?`, id)
	if err != nil {
		if errors.Is(err, models.ErrNotFound) {
			writeNotFound(w, "location")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	var existing map[string]any
	_ = json.Unmarshal(existingRaw, &existing)
	for k, v := range patch {
		switch k {
		case "id", "selfUri", "version":
		default:
			existing[k] = v
		}
	}
	if v, ok := existing["version"].(float64); ok {
		existing["version"] = int(v) + 1
	} else {
		existing["version"] = 2
	}
	enc, _ := json.Marshal(existing)
	name, _ := existing["name"].(string)
	_, err = app.repo.DB().Exec(
		`UPDATE locations SET name = ?, body = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`,
		name, string(enc), id,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	writeJSONStatus(w, http.StatusOK, json.RawMessage(enc))
}

func (app *Application) handleLocationDelete(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "locationId")
	res, err := app.repo.DB().Exec(`DELETE FROM locations WHERE id = ?`, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		writeNotFound(w, "location")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
