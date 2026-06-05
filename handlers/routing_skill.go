package handlers

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/redscaresu/fakegenesys/models"
)

// routing/skills — POST/GET + GET/PATCH/DELETE. Unique name.

func (app *Application) registerRoutingSkillRoutes(r chi.Router) {
	r.Post("/routing/skills", app.handleRoutingSkillCreate)
	r.Get("/routing/skills", app.handleRoutingSkillList)
	r.Get("/routing/skills/{skillId}", app.handleRoutingSkillGet)
	r.Patch("/routing/skills/{skillId}", app.handleRoutingSkillUpdate)
	r.Delete("/routing/skills/{skillId}", app.handleRoutingSkillDelete)
}

func (app *Application) handleRoutingSkillCreate(w http.ResponseWriter, r *http.Request) {
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
	body["selfUri"] = "/api/v2/routing/skills/" + id
	enc, _ := json.Marshal(body)
	_, err = app.repo.DB().Exec(
		`INSERT INTO routing_skills(id, name, body) VALUES (?, ?, ?)`,
		id, body["name"].(string), string(enc),
	)
	if err != nil {
		if isUniqueViolation(err) {
			writeConflict(w, "skill with name "+body["name"].(string)+" already exists")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	writeJSONStatus(w, http.StatusCreated, json.RawMessage(enc))
}

func (app *Application) handleRoutingSkillList(w http.ResponseWriter, r *http.Request) {
	rows, err := listAllJSON(app.repo.DB(),
		`SELECT body FROM routing_skills ORDER BY name`)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	pagedList(w, r, rows)
}

func (app *Application) handleRoutingSkillGet(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "skillId")
	raw, err := scanOneJSON(app.repo.DB(),
		`SELECT body FROM routing_skills WHERE id = ?`, id)
	if err != nil {
		if errors.Is(err, models.ErrNotFound) {
			writeNotFound(w, "routing_skill")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	writeJSONStatus(w, http.StatusOK, raw)
}

func (app *Application) handleRoutingSkillUpdate(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "skillId")
	patch, err := decodeJSONBody(r)
	if err != nil {
		writeBadRequest(w, "body: "+err.Error())
		return
	}
	existingRaw, err := scanOneJSON(app.repo.DB(),
		`SELECT body FROM routing_skills WHERE id = ?`, id)
	if err != nil {
		if errors.Is(err, models.ErrNotFound) {
			writeNotFound(w, "routing_skill")
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
		`UPDATE routing_skills SET name = ?, body = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`,
		name, string(enc), id,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	writeJSONStatus(w, http.StatusOK, json.RawMessage(enc))
}

func (app *Application) handleRoutingSkillDelete(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "skillId")
	res, err := app.repo.DB().Exec(`DELETE FROM routing_skills WHERE id = ?`, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		writeNotFound(w, "routing_skill")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
