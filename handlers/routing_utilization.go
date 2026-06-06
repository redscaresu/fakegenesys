package handlers

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/redscaresu/fakegenesys/models"
)

// routing/utilization — singleton. No id in the URL. PUT replaces the
// config; DELETE returns to defaults; GET returns current.
//
// The singleton row id is the literal "_". Initial GET (before any
// PUT) returns the default config — a single empty `utilization` map.

const utilizationID = "_"

func (app *Application) registerRoutingUtilizationRoutes(r chi.Router) {
	r.Get("/routing/utilization", app.handleRoutingUtilizationGet)
	r.Put("/routing/utilization", app.handleRoutingUtilizationPut)
	r.Delete("/routing/utilization", app.handleRoutingUtilizationDelete)
	// S116c: per-user routing utilization override. The genesyscloud
	// provider's readUser calls
	// /api/v2/routing/users/{userId}/utilization unconditionally;
	// without a 200 the apply hangs in retry. We return the same
	// default singleton shape — a user with no override matches the
	// global default.
	r.Get("/routing/users/{userId}/utilization", app.handleUserRoutingUtilizationGet)
	r.Put("/routing/users/{userId}/utilization", app.handleUserRoutingUtilizationPut)
	r.Delete("/routing/users/{userId}/utilization", app.handleUserRoutingUtilizationDelete)
}

func (app *Application) handleUserRoutingUtilizationGet(w http.ResponseWriter, _ *http.Request) {
	writeJSONStatus(w, http.StatusOK, defaultUtilization())
}

func (app *Application) handleUserRoutingUtilizationPut(w http.ResponseWriter, r *http.Request) {
	body, err := decodeJSONBody(r)
	if err != nil {
		writeBadRequest(w, "body: "+err.Error())
		return
	}
	writeJSONStatus(w, http.StatusOK, body)
}

func (app *Application) handleUserRoutingUtilizationDelete(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusNoContent)
}

func defaultUtilization() map[string]any {
	return map[string]any{
		"utilization": map[string]any{},
	}
}

func (app *Application) handleRoutingUtilizationGet(w http.ResponseWriter, _ *http.Request) {
	raw, err := scanOneJSON(app.repo.DB(),
		`SELECT body FROM routing_utilization WHERE id = ?`, utilizationID)
	if err != nil {
		if errors.Is(err, models.ErrNotFound) {
			body, _ := json.Marshal(defaultUtilization())
			writeJSONStatus(w, http.StatusOK, json.RawMessage(body))
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	writeJSONStatus(w, http.StatusOK, raw)
}

func (app *Application) handleRoutingUtilizationPut(w http.ResponseWriter, r *http.Request) {
	body, err := decodeJSONBody(r)
	if err != nil {
		writeBadRequest(w, "body: "+err.Error())
		return
	}
	enc, _ := json.Marshal(body)
	_, err = app.repo.DB().Exec(
		`INSERT INTO routing_utilization(id, body) VALUES (?, ?)
		 ON CONFLICT(id) DO UPDATE SET body = excluded.body, updated_at = CURRENT_TIMESTAMP`,
		utilizationID, string(enc),
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	writeJSONStatus(w, http.StatusOK, json.RawMessage(enc))
}

func (app *Application) handleRoutingUtilizationDelete(w http.ResponseWriter, _ *http.Request) {
	_, err := app.repo.DB().Exec(`DELETE FROM routing_utilization WHERE id = ?`, utilizationID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// Verify chi is imported (the file has no other chi reference once
// each handler hits the URL params directly; this keeps the import
// from being dropped during refactor).
var _ chi.Router
