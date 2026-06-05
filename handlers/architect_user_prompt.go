package handlers

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/redscaresu/fakegenesys/models"
)

// architect_user_prompt — `/api/v2/architect/prompts` (POST/GET/list)
// + `/{promptId}` (GET/PUT/DELETE). Resources (audio files per
// language) are deferred; the smoke harness exercises the prompt
// envelope only.

func (app *Application) registerArchitectUserPromptRoutes(r chi.Router) {
	r.Post("/architect/prompts", app.handleUserPromptCreate)
	r.Get("/architect/prompts", app.handleUserPromptList)
	r.Get("/architect/prompts/{promptId}", app.handleUserPromptGet)
	r.Put("/architect/prompts/{promptId}", app.handleUserPromptUpdate)
	r.Delete("/architect/prompts/{promptId}", app.handleUserPromptDelete)
}

func (app *Application) handleUserPromptCreate(w http.ResponseWriter, r *http.Request) {
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
	body["selfUri"] = "/api/v2/architect/prompts/" + id
	enc, _ := json.Marshal(body)
	_, err = app.repo.DB().Exec(
		`INSERT INTO architect_user_prompts(id, name, body) VALUES (?, ?, ?)`,
		id, body["name"].(string), string(enc),
	)
	if err != nil {
		if isUniqueViolation(err) {
			writeConflict(w, "prompt with name "+body["name"].(string)+" already exists")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	writeJSONStatus(w, http.StatusCreated, json.RawMessage(enc))
}

func (app *Application) handleUserPromptList(w http.ResponseWriter, r *http.Request) {
	rows, err := listAllJSON(app.repo.DB(),
		`SELECT body FROM architect_user_prompts ORDER BY name`)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	pagedList(w, r, rows)
}

func (app *Application) handleUserPromptGet(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "promptId")
	raw, err := scanOneJSON(app.repo.DB(),
		`SELECT body FROM architect_user_prompts WHERE id = ?`, id)
	if err != nil {
		if errors.Is(err, models.ErrNotFound) {
			writeNotFound(w, "architect_user_prompt")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	writeJSONStatus(w, http.StatusOK, raw)
}

func (app *Application) handleUserPromptUpdate(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "promptId")
	patch, err := decodeJSONBody(r)
	if err != nil {
		writeBadRequest(w, "body: "+err.Error())
		return
	}
	existingRaw, err := scanOneJSON(app.repo.DB(),
		`SELECT body FROM architect_user_prompts WHERE id = ?`, id)
	if err != nil {
		if errors.Is(err, models.ErrNotFound) {
			writeNotFound(w, "architect_user_prompt")
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
		`UPDATE architect_user_prompts SET name = ?, body = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`,
		name, string(enc), id,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	writeJSONStatus(w, http.StatusOK, json.RawMessage(enc))
}

func (app *Application) handleUserPromptDelete(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "promptId")
	res, err := app.repo.DB().Exec(`DELETE FROM architect_user_prompts WHERE id = ?`, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		writeNotFound(w, "architect_user_prompt")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
