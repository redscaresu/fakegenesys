package handlers

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/redscaresu/fakegenesys/models"
)

// /mock/state schema (versioned via the top-level "schema_version" key
// so topology_derive_genesys can detect breaking changes). Documented
// inline so the contract is stable from S108 onwards.
//
//	{
//	  "schema_version": 1,
//	  "users":            [...],
//	  "groups":           [...],
//	  "locations":        [...],
//	  "auth_roles":       [...],
//	  "oauth_clients":    [...],
//	  "routing_queues":   [...],
//	  ...
//	  "operations":       [...]   // bookkeeping; ignored by topology
//	}
//
// Per-resource gather methods land per slice (S109/S110/S111).
const stateSchemaVersion = 1

// registerAdminRoutes wires /mock/* admin endpoints. Unauthenticated by
// design — same convention as mockway / fakegcp / fakeaws.
func (app *Application) registerAdminRoutes(r chi.Router) {
	r.Route("/mock", func(mr chi.Router) {
		mr.Post("/reset", app.handleMockReset)
		mr.Post("/snapshot", app.handleMockSnapshot)
		mr.Post("/restore", app.handleMockRestore)
		mr.Get("/state", app.handleMockState)
		mr.Get("/state/{service}", app.handleMockStateService)
	})
}

func (app *Application) handleMockReset(w http.ResponseWriter, _ *http.Request) {
	if err := app.repo.Reset(); err != nil {
		writeJSONStatus(w, http.StatusInternalServerError, map[string]any{
			"status":  "error",
			"message": err.Error(),
		})
		return
	}
	writeJSONStatus(w, http.StatusOK, map[string]any{"status": "ok"})
}

func (app *Application) handleMockSnapshot(w http.ResponseWriter, _ *http.Request) {
	if err := app.repo.Snapshot(); err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, models.ErrConflict) {
			status = http.StatusConflict
		}
		writeJSONStatus(w, status, map[string]any{
			"status":  "error",
			"message": err.Error(),
		})
		return
	}
	writeJSONStatus(w, http.StatusOK, map[string]any{"status": "ok"})
}

func (app *Application) handleMockRestore(w http.ResponseWriter, _ *http.Request) {
	if err := app.repo.Restore(); err != nil {
		status := http.StatusInternalServerError
		switch {
		case errors.Is(err, models.ErrNotFound):
			status = http.StatusNotFound
		case errors.Is(err, models.ErrConflict):
			status = http.StatusConflict
		}
		writeJSONStatus(w, status, map[string]any{
			"status":  "error",
			"message": err.Error(),
		})
		return
	}
	writeJSONStatus(w, http.StatusOK, map[string]any{"status": "ok"})
}

func (app *Application) handleMockState(w http.ResponseWriter, _ *http.Request) {
	state := app.collectState("")
	writeJSONStatus(w, http.StatusOK, state)
}

func (app *Application) handleMockStateService(w http.ResponseWriter, r *http.Request) {
	service := chi.URLParam(r, "service")
	state := app.collectState(service)
	writeJSONStatus(w, http.StatusOK, state)
}

// collectState gathers the per-service state into the documented shape.
// service == "" returns the full state; otherwise just the named
// service's block.
//
// Per-resource gather methods land per slice.
func (app *Application) collectState(service string) map[string]any {
	full := map[string]any{
		"schema_version": stateSchemaVersion,
		// S109 identity:
		"users":         app.gatherTable("users"),
		"groups":        app.gatherTable("groups"),
		"locations":     app.gatherTable("locations"),
		"auth_roles":    app.gatherTable("auth_roles"),
		"oauth_clients": app.gatherTable("oauth_clients"),
		// S110 routing:
		"routing_queues":       []any{},
		"routing_skills":       []any{},
		"routing_wrapupcodes":  []any{},
		"routing_languages":    []any{},
		"routing_utilization":  map[string]any{},
		"routing_queue_members": []any{},
		// S111 architect / responsemanagement / IDP:
		"architect_datatables":         []any{},
		"architect_datatable_rows":     []any{},
		"architect_user_prompts":       []any{},
		"flows":                        []any{},
		"responsemanagement_responses": []any{},
		"idp_generic":                  map[string]any{},
	}
	if service == "" {
		return full
	}
	if v, ok := full[service]; ok {
		return map[string]any{
			"schema_version": stateSchemaVersion,
			service:          v,
		}
	}
	return map[string]any{
		"schema_version": stateSchemaVersion,
		"error":          "unknown service: " + service,
	}
}

// gatherTable returns every body row in the named SQLite table as a
// []json.RawMessage. Used by collectState to walk per-resource tables
// without each handler file needing to register its own gatherer.
func (app *Application) gatherTable(table string) []json.RawMessage {
	rows, err := listAllJSON(app.repo.DB(),
		"SELECT body FROM "+table+" ORDER BY id")
	if err != nil {
		return []json.RawMessage{}
	}
	if rows == nil {
		return []json.RawMessage{}
	}
	return rows
}
