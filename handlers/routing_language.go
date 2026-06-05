package handlers

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/redscaresu/fakegenesys/models"
)

// routing/languages — POST/GET + GET/DELETE. Spec declares no PUT or
// PATCH on languages; fakegenesys mirrors that (Reverse Fidelity: if
// the spec doesn't declare it, we don't add it).

func (app *Application) registerRoutingLanguageRoutes(r chi.Router) {
	r.Post("/routing/languages", app.handleRoutingLanguageCreate)
	r.Get("/routing/languages", app.handleRoutingLanguageList)
	r.Get("/routing/languages/{languageId}", app.handleRoutingLanguageGet)
	r.Delete("/routing/languages/{languageId}", app.handleRoutingLanguageDelete)
}

func (app *Application) handleRoutingLanguageCreate(w http.ResponseWriter, r *http.Request) {
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
	body["selfUri"] = "/api/v2/routing/languages/" + id
	body["state"] = stateOr(body, "active")
	enc, _ := json.Marshal(body)
	_, err = app.repo.DB().Exec(
		`INSERT INTO routing_languages(id, name, body) VALUES (?, ?, ?)`,
		id, body["name"].(string), string(enc),
	)
	if err != nil {
		if isUniqueViolation(err) {
			writeConflict(w, "language with name "+body["name"].(string)+" already exists")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	writeJSONStatus(w, http.StatusCreated, json.RawMessage(enc))
}

func (app *Application) handleRoutingLanguageList(w http.ResponseWriter, r *http.Request) {
	rows, err := listAllJSON(app.repo.DB(),
		`SELECT body FROM routing_languages ORDER BY name`)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	pagedList(w, r, rows)
}

func (app *Application) handleRoutingLanguageGet(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "languageId")
	raw, err := scanOneJSON(app.repo.DB(),
		`SELECT body FROM routing_languages WHERE id = ?`, id)
	if err != nil {
		if errors.Is(err, models.ErrNotFound) {
			writeNotFound(w, "routing_language")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	writeJSONStatus(w, http.StatusOK, raw)
}

func (app *Application) handleRoutingLanguageDelete(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "languageId")
	res, err := app.repo.DB().Exec(`DELETE FROM routing_languages WHERE id = ?`, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		writeNotFound(w, "routing_language")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
