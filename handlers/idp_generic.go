package handlers

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/redscaresu/fakegenesys/models"
)

// idp_generic — `/api/v2/identityproviders/generic`. Singleton (no id
// in URL). GET / PUT / DELETE.

const idpGenericID = "_"

func (app *Application) registerIDPGenericRoutes(r chi.Router) {
	r.Get("/identityproviders/generic", app.handleIDPGenericGet)
	r.Put("/identityproviders/generic", app.handleIDPGenericPut)
	r.Delete("/identityproviders/generic", app.handleIDPGenericDelete)
}

func (app *Application) handleIDPGenericGet(w http.ResponseWriter, _ *http.Request) {
	raw, err := scanOneJSON(app.repo.DB(),
		`SELECT body FROM idp_generic WHERE id = ?`, idpGenericID)
	if err != nil {
		if errors.Is(err, models.ErrNotFound) {
			writeNotFound(w, "idp_generic (not configured)")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	writeJSONStatus(w, http.StatusOK, raw)
}

func (app *Application) handleIDPGenericPut(w http.ResponseWriter, r *http.Request) {
	body, err := decodeJSONBody(r)
	if err != nil {
		writeBadRequest(w, "body: "+err.Error())
		return
	}
	body["selfUri"] = "/api/v2/identityproviders/generic"
	enc, _ := json.Marshal(body)
	_, err = app.repo.DB().Exec(
		`INSERT INTO idp_generic(id, body) VALUES (?, ?)
		 ON CONFLICT(id) DO UPDATE SET body = excluded.body, updated_at = CURRENT_TIMESTAMP`,
		idpGenericID, string(enc),
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	writeJSONStatus(w, http.StatusOK, json.RawMessage(enc))
}

func (app *Application) handleIDPGenericDelete(w http.ResponseWriter, _ *http.Request) {
	if _, err := app.repo.DB().Exec(`DELETE FROM idp_generic WHERE id = ?`, idpGenericID); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// Ensure chi is imported for the route registration.
var _ chi.Router
