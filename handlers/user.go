package handlers

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/redscaresu/fakegenesys/models"
)

// Genesys user resource — POST/GET /api/v2/users + GET/PATCH/DELETE
// /api/v2/users/{userId}. Soft-delete is the documented behavior: real
// Genesys flips state=deleted instead of removing the row, so a
// subsequent GET returns 200 with state=deleted. fakegenesys mirrors
// this — see docs/spec-notes/user.md.

func (app *Application) registerUserRoutes(r chi.Router) {
	r.Post("/users", app.handleUserCreate)
	r.Get("/users", app.handleUserList)
	r.Get("/users/{userId}", app.handleUserGet)
	r.Patch("/users/{userId}", app.handleUserUpdate)
	r.Delete("/users/{userId}", app.handleUserDelete)
}

func (app *Application) handleUserCreate(w http.ResponseWriter, r *http.Request) {
	body, err := decodeJSONBody(r)
	if err != nil {
		writeBadRequest(w, "body: "+err.Error())
		return
	}
	if err := requireStringFields(body, "name", "email"); err != nil {
		writeBadRequest(w, err.Error())
		return
	}
	id := newID()
	email := body["email"].(string)
	name := body["name"].(string)
	body["id"] = id
	body["state"] = stateOr(body, "active")
	body["selfUri"] = "/api/v2/users/" + id
	body["version"] = 1
	body["dateCreated"] = nowZ()
	body["dateModified"] = body["dateCreated"]
	// S116c: the genesyscloud Terraform provider's readUser dereferences
	// currentUser.Division.Id unconditionally
	// (resource_genesyscloud_user.go:166). A user without a division
	// crashes the plugin process during the read-after-create.
	// Default to the Home division id used by our authorization stubs
	// so any user creation flow yields a state-compatible read.
	if body["division"] == nil {
		body["division"] = map[string]any{
			"id":      fakegenesysHomeDivisionID,
			"name":    "Home",
			"selfUri": "/api/v2/authorization/divisions/" + fakegenesysHomeDivisionID,
		}
	}
	enc, _ := json.Marshal(body)
	_, err = app.repo.DB().Exec(
		`INSERT INTO users(id, email, name, state, body) VALUES (?, ?, ?, ?, ?)`,
		id, email, name, body["state"], string(enc),
	)
	if err != nil {
		if isUniqueViolation(err) {
			writeConflict(w, "user with email "+email+" already exists")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	writeJSONStatus(w, http.StatusCreated, json.RawMessage(enc))
}

func (app *Application) handleUserList(w http.ResponseWriter, r *http.Request) {
	// S112 finding #7: real Genesys returns only active users by
	// default; ?state=deleted filters to soft-deleted; ?state=any
	// returns everything. fakegenesys mirrors that contract so the
	// Terraform data source's default read doesn't see soft-deleted
	// ghosts after a destroy + re-apply cycle.
	stateFilter := r.URL.Query().Get("state")
	var rows []json.RawMessage
	var err error
	switch stateFilter {
	case "", "active":
		rows, err = listAllJSON(app.repo.DB(),
			`SELECT body FROM users WHERE state = 'active' ORDER BY name`)
	case "any":
		rows, err = listAllJSON(app.repo.DB(),
			`SELECT body FROM users ORDER BY name`)
	default:
		rows, err = listAllJSON(app.repo.DB(),
			`SELECT body FROM users WHERE state = ? ORDER BY name`, stateFilter)
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	pagedList(w, r, rows)
}

func (app *Application) handleUserGet(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "userId")
	raw, err := scanOneJSON(app.repo.DB(),
		`SELECT body FROM users WHERE id = ?`, id)
	if err != nil {
		if errors.Is(err, models.ErrNotFound) {
			writeNotFound(w, "user")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	writeJSONStatus(w, http.StatusOK, raw)
}

func (app *Application) handleUserUpdate(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "userId")
	patch, err := decodeJSONBody(r)
	if err != nil {
		writeBadRequest(w, "body: "+err.Error())
		return
	}
	existingRaw, err := scanOneJSON(app.repo.DB(),
		`SELECT body FROM users WHERE id = ?`, id)
	if err != nil {
		if errors.Is(err, models.ErrNotFound) {
			writeNotFound(w, "user")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	var existing map[string]any
	if err := json.Unmarshal(existingRaw, &existing); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	for k, v := range patch {
		// id + selfUri + dateCreated are immutable. Everything else
		// is overwritten by the patch (Genesys's PATCH semantics are
		// merge-at-top-level for users).
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
	state := stateOr(existing, "active")
	name, _ := existing["name"].(string)
	_, err = app.repo.DB().Exec(
		`UPDATE users SET name = ?, state = ?, body = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`,
		name, state, string(enc), id,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	writeJSONStatus(w, http.StatusOK, json.RawMessage(enc))
}

func (app *Application) handleUserDelete(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "userId")
	// Soft delete: flip state to "deleted" + bump version. Mirrors
	// real Genesys behavior; the Terraform provider's destroy waits
	// for the GET to return state=deleted.
	existingRaw, err := scanOneJSON(app.repo.DB(),
		`SELECT body FROM users WHERE id = ?`, id)
	if err != nil {
		if errors.Is(err, models.ErrNotFound) {
			writeNotFound(w, "user")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	var existing map[string]any
	_ = json.Unmarshal(existingRaw, &existing)
	existing["state"] = "deleted"
	existing["dateModified"] = nowZ()
	enc, _ := json.Marshal(existing)
	if _, err := app.repo.DB().Exec(
		`UPDATE users SET state = 'deleted', body = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`,
		string(enc), id,
	); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// stateOr returns body["state"] when present, defaultVal otherwise.
func stateOr(body map[string]any, defaultVal string) string {
	if v, ok := body["state"].(string); ok && v != "" {
		return v
	}
	return defaultVal
}

func nowZ() string { return time.Now().UTC().Format(time.RFC3339) }

// isUniqueViolation reports whether the error is a SQLite UNIQUE
// constraint violation. modernc.org/sqlite surfaces these in error
// strings; we match on the well-known substring rather than depending
// on the driver's error type.
func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "UNIQUE constraint failed") ||
		strings.Contains(msg, "SQLITE_CONSTRAINT_UNIQUE") ||
		strings.Contains(msg, "constraint failed: UNIQUE")
}

// Ensure database/sql import is used (compile-time guard so the
// import survives go vet when the local handler doesn't need it
// directly).
var _ = sql.ErrConnDone
