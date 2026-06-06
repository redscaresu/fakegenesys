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
		mr.Get("/ca-cert", app.handleMockCACert)
		// S122: flow upload endpoint. The genesyscloud provider gets a
		// presignedUrl back from POST /api/v2/flows/jobs and PUTs the
		// YAML there. We accept any bytes and 200 — no parsing.
		mr.Put("/flow-upload/{jobId}", app.handleFlowUpload)
	})
}

func (app *Application) handleFlowUpload(w http.ResponseWriter, _ *http.Request) {
	// Accept any payload; do not parse. The smoke harness verifies the
	// flow exists after the upload, not the YAML's correctness.
	w.WriteHeader(http.StatusOK)
}

// handleMockCACert returns the PEM-encoded boot-time CA cert that signs
// every leaf the TLS MITM proxy issues. Harnesses fetch it at runtime
// and write it to SSL_CERT_FILE so Go's TLS stack trusts our MITM chain
// without bundling a pre-baked cert. S116.
func (app *Application) handleMockCACert(w http.ResponseWriter, _ *http.Request) {
	if app.mitm == nil {
		writeError(w, http.StatusServiceUnavailable, "mitm.unavailable",
			"TLS MITM proxy not initialised")
		return
	}
	w.Header().Set("Content-Type", "application/x-pem-file")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(app.mitm.CACertPEM())
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
		"routing_queues":        app.gatherTable("routing_queues"),
		"routing_skills":        app.gatherTable("routing_skills"),
		"routing_wrapupcodes":   app.gatherTable("routing_wrapupcodes"),
		"routing_languages":     app.gatherTable("routing_languages"),
		"routing_utilization":   app.gatherUtilization(),
		"routing_queue_members": app.gatherQueueMembers(),
		// S111 architect / responsemanagement / IDP:
		"architect_datatables":         app.gatherTable("architect_datatables"),
		"architect_datatable_rows":     app.gatherDatatableRows(),
		"architect_user_prompts":       app.gatherTable("architect_user_prompts"),
		"flows":                        app.gatherTable("flows"),
		"responsemanagement_responses": app.gatherTable("responsemanagement_responses"),
		"idp_generic":                  app.gatherIDPGeneric(),
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

// gatherUtilization returns the singleton utilization config (or the
// default empty config when no PUT has been issued).
func (app *Application) gatherUtilization() json.RawMessage {
	raw, err := scanOneJSON(app.repo.DB(),
		`SELECT body FROM routing_utilization WHERE id = ?`, utilizationID)
	if err != nil {
		b, _ := json.Marshal(defaultUtilization())
		return json.RawMessage(b)
	}
	return raw
}

// gatherDatatableRows returns the raw rows grid for topology
// derivation. Each entry: {datatableId, rowId, body}.
func (app *Application) gatherDatatableRows() []map[string]any {
	out := []map[string]any{}
	rows, err := app.repo.DB().Query(
		`SELECT datatable_id, row_id, body FROM architect_datatable_rows ORDER BY datatable_id, row_id`)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var dtID, rowID string
		var body []byte
		if err := rows.Scan(&dtID, &rowID, &body); err == nil {
			out = append(out, map[string]any{
				"datatableId": dtID,
				"rowId":       rowID,
				"body":        json.RawMessage(body),
			})
		}
	}
	return out
}

// gatherIDPGeneric returns the singleton IDP config, or an empty
// object when not configured.
func (app *Application) gatherIDPGeneric() json.RawMessage {
	raw, err := scanOneJSON(app.repo.DB(),
		`SELECT body FROM idp_generic WHERE id = ?`, idpGenericID)
	if err != nil {
		return json.RawMessage(`{}`)
	}
	return raw
}

// gatherQueueMembers returns the raw membership grid for topology
// derivation. Each entry: {queueId, userId, ringNumber}.
func (app *Application) gatherQueueMembers() []map[string]any {
	out := []map[string]any{}
	rows, err := app.repo.DB().Query(
		`SELECT queue_id, user_id, ring_number FROM routing_queue_members ORDER BY queue_id, user_id`)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var qID, uID string
		var ring int
		if err := rows.Scan(&qID, &uID, &ring); err == nil {
			out = append(out, map[string]any{
				"queueId":    qID,
				"userId":     uID,
				"ringNumber": ring,
			})
		}
	}
	return out
}
