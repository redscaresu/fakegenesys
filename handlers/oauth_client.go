package handlers

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/redscaresu/fakegenesys/models"
)

// Genesys OAuth client resource —
// POST/GET /api/v2/oauth/clients +
// GET/PUT/DELETE /api/v2/oauth/clients/{clientId}.
//
// Reveal-once secret semantics: the create response includes
// `secret`; subsequent GET responses do NOT (mirrors real Genesys
// where the secret is only visible at create time). Tests that need
// to re-read the secret can hit `POST /api/v2/oauth/clients/{id}/secret`
// to mint a fresh one — implemented when a scenario actually needs it.

func (app *Application) registerOAuthClientRoutes(r chi.Router) {
	r.Post("/oauth/clients", app.handleOAuthClientCreate)
	r.Get("/oauth/clients", app.handleOAuthClientList)
	r.Get("/oauth/clients/{clientId}", app.handleOAuthClientGet)
	r.Put("/oauth/clients/{clientId}", app.handleOAuthClientUpdate)
	r.Delete("/oauth/clients/{clientId}", app.handleOAuthClientDelete)
}

func (app *Application) handleOAuthClientCreate(w http.ResponseWriter, r *http.Request) {
	body, err := decodeJSONBody(r)
	if err != nil {
		writeBadRequest(w, "body: "+err.Error())
		return
	}
	if err := requireStringFields(body, "name", "authorizedGrantType"); err != nil {
		writeBadRequest(w, err.Error())
		return
	}
	id := newID()
	secret := uuid.NewString() + uuid.NewString()
	body["id"] = id
	body["selfUri"] = "/api/v2/oauth/clients/" + id
	// Stored body omits the secret so subsequent GETs don't leak it;
	// the response payload below includes it (reveal-once).
	stored := cloneMap(body)
	body["secret"] = secret
	respEnc, _ := json.Marshal(body)
	storedEnc, _ := json.Marshal(stored)
	_, err = app.repo.DB().Exec(
		`INSERT INTO oauth_clients(id, name, secret, grant_type, body) VALUES (?, ?, ?, ?, ?)`,
		id, body["name"].(string), secret, body["authorizedGrantType"].(string), string(storedEnc),
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	writeJSONStatus(w, http.StatusCreated, json.RawMessage(respEnc))
}

func (app *Application) handleOAuthClientList(w http.ResponseWriter, r *http.Request) {
	rows, err := listAllJSON(app.repo.DB(),
		`SELECT body FROM oauth_clients ORDER BY name`)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	pagedList(w, r, rows)
}

func (app *Application) handleOAuthClientGet(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "clientId")
	raw, err := scanOneJSON(app.repo.DB(),
		`SELECT body FROM oauth_clients WHERE id = ?`, id)
	if err != nil {
		if errors.Is(err, models.ErrNotFound) {
			writeNotFound(w, "oauth_client")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	writeJSONStatus(w, http.StatusOK, raw)
}

func (app *Application) handleOAuthClientUpdate(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "clientId")
	body, err := decodeJSONBody(r)
	if err != nil {
		writeBadRequest(w, "body: "+err.Error())
		return
	}
	existingRaw, err := scanOneJSON(app.repo.DB(),
		`SELECT body FROM oauth_clients WHERE id = ?`, id)
	if err != nil {
		if errors.Is(err, models.ErrNotFound) {
			writeNotFound(w, "oauth_client")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	var existing map[string]any
	_ = json.Unmarshal(existingRaw, &existing)
	for k, v := range body {
		if k == "id" || k == "selfUri" || k == "secret" {
			continue
		}
		existing[k] = v
	}
	enc, _ := json.Marshal(existing)
	name, _ := existing["name"].(string)
	grantType, _ := existing["authorizedGrantType"].(string)
	_, err = app.repo.DB().Exec(
		`UPDATE oauth_clients SET name = ?, grant_type = ?, body = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`,
		name, grantType, string(enc), id,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	writeJSONStatus(w, http.StatusOK, json.RawMessage(enc))
}

func (app *Application) handleOAuthClientDelete(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "clientId")
	res, err := app.repo.DB().Exec(`DELETE FROM oauth_clients WHERE id = ?`, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		writeNotFound(w, "oauth_client")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// cloneMap returns a shallow copy of m so callers can mutate the
// returned reference without affecting the original.
func cloneMap(m map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}
