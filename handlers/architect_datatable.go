package handlers

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/redscaresu/fakegenesys/models"
)

// architect_datatable — POST/GET /api/v2/flows/datatables +
// CRUD on /{datatableId}. Plus the rows sub-resource at
// /{datatableId}/rows + /{datatableId}/rows/{rowId}.
//
// Rows are validated against the parent datatable's `schema` only at
// the row-id-uniqueness level — fakegenesys does NOT enforce
// per-cell type checks (Reverse Fidelity: the real provider handles
// schema validation client-side and the spec doesn't pin the
// per-cell error shape).
//
// IMPORTANT: routes must register BEFORE /api/v2/flows/{flowId} so chi
// matches the more-specific /datatables prefix first.

func (app *Application) registerArchitectDatatableRoutes(r chi.Router) {
	r.Post("/flows/datatables", app.handleDatatableCreate)
	r.Get("/flows/datatables", app.handleDatatableList)
	r.Get("/flows/datatables/{datatableId}", app.handleDatatableGet)
	r.Put("/flows/datatables/{datatableId}", app.handleDatatableUpdate)
	// PATCH not in spec — Reverse Fidelity (S112 finding #2 caught this).
	r.Delete("/flows/datatables/{datatableId}", app.handleDatatableDelete)
	r.Post("/flows/datatables/{datatableId}/rows", app.handleDatatableRowCreate)
	r.Get("/flows/datatables/{datatableId}/rows", app.handleDatatableRowList)
	r.Get("/flows/datatables/{datatableId}/rows/{rowId}", app.handleDatatableRowGet)
	r.Put("/flows/datatables/{datatableId}/rows/{rowId}", app.handleDatatableRowUpdate)
	r.Delete("/flows/datatables/{datatableId}/rows/{rowId}", app.handleDatatableRowDelete)
}

func (app *Application) handleDatatableCreate(w http.ResponseWriter, r *http.Request) {
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
	body["selfUri"] = "/api/v2/flows/datatables/" + id
	// S122: the genesyscloud provider's readArchitectDatatable does
	// `*datatable.Division.Id` unconditionally
	// (resource_genesyscloud_architect_datatable.go:121). Without a
	// default division the plugin segfaults during the read-after-
	// create. Mirrors the user create fix from S116c.
	if body["division"] == nil {
		body["division"] = map[string]any{
			"id":      fakegenesysHomeDivisionID,
			"name":    "Home",
			"selfUri": "/api/v2/authorization/divisions/" + fakegenesysHomeDivisionID,
		}
	}
	enc, _ := json.Marshal(body)
	_, err = app.repo.DB().Exec(
		`INSERT INTO architect_datatables(id, name, body) VALUES (?, ?, ?)`,
		id, body["name"].(string), string(enc),
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	writeJSONStatus(w, http.StatusCreated, json.RawMessage(enc))
}

func (app *Application) handleDatatableList(w http.ResponseWriter, r *http.Request) {
	rows, err := listAllJSON(app.repo.DB(),
		`SELECT body FROM architect_datatables ORDER BY name`)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	pagedList(w, r, rows)
}

func (app *Application) handleDatatableGet(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "datatableId")
	raw, err := scanOneJSON(app.repo.DB(),
		`SELECT body FROM architect_datatables WHERE id = ?`, id)
	if err != nil {
		if errors.Is(err, models.ErrNotFound) {
			writeNotFound(w, "architect_datatable")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	writeJSONStatus(w, http.StatusOK, raw)
}

func (app *Application) handleDatatableUpdate(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "datatableId")
	patch, err := decodeJSONBody(r)
	if err != nil {
		writeBadRequest(w, "body: "+err.Error())
		return
	}
	existingRaw, err := scanOneJSON(app.repo.DB(),
		`SELECT body FROM architect_datatables WHERE id = ?`, id)
	if err != nil {
		if errors.Is(err, models.ErrNotFound) {
			writeNotFound(w, "architect_datatable")
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
		`UPDATE architect_datatables SET name = ?, body = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`,
		name, string(enc), id,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	writeJSONStatus(w, http.StatusOK, json.RawMessage(enc))
}

func (app *Application) handleDatatableDelete(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "datatableId")
	res, err := app.repo.DB().Exec(`DELETE FROM architect_datatables WHERE id = ?`, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		writeNotFound(w, "architect_datatable")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- rows ---

func (app *Application) handleDatatableRowCreate(w http.ResponseWriter, r *http.Request) {
	dtID := chi.URLParam(r, "datatableId")
	if err := app.requireDatatableExists(dtID); err != nil {
		if errors.Is(err, models.ErrNotFound) {
			writeNotFound(w, "architect_datatable")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	body, err := decodeJSONBody(r)
	if err != nil {
		writeBadRequest(w, "body: "+err.Error())
		return
	}
	rowID, _ := body["key"].(string)
	if rowID == "" {
		rowID = newID()
		body["key"] = rowID
	}
	enc, _ := json.Marshal(body)
	_, err = app.repo.DB().Exec(
		`INSERT INTO architect_datatable_rows(datatable_id, row_id, body) VALUES (?, ?, ?)`,
		dtID, rowID, string(enc),
	)
	if err != nil {
		if isUniqueViolation(err) {
			writeConflict(w, "row with key "+rowID+" already exists")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	writeJSONStatus(w, http.StatusCreated, json.RawMessage(enc))
}

func (app *Application) handleDatatableRowList(w http.ResponseWriter, r *http.Request) {
	dtID := chi.URLParam(r, "datatableId")
	if err := app.requireDatatableExists(dtID); err != nil {
		if errors.Is(err, models.ErrNotFound) {
			writeNotFound(w, "architect_datatable")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	rows, err := listAllJSON(app.repo.DB(),
		`SELECT body FROM architect_datatable_rows WHERE datatable_id = ? ORDER BY row_id`,
		dtID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	pagedList(w, r, rows)
}

func (app *Application) handleDatatableRowGet(w http.ResponseWriter, r *http.Request) {
	dtID := chi.URLParam(r, "datatableId")
	rowID := chi.URLParam(r, "rowId")
	raw, err := scanOneJSON(app.repo.DB(),
		`SELECT body FROM architect_datatable_rows WHERE datatable_id = ? AND row_id = ?`,
		dtID, rowID)
	if err != nil {
		if errors.Is(err, models.ErrNotFound) {
			writeNotFound(w, "architect_datatable_row")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	writeJSONStatus(w, http.StatusOK, raw)
}

func (app *Application) handleDatatableRowUpdate(w http.ResponseWriter, r *http.Request) {
	dtID := chi.URLParam(r, "datatableId")
	rowID := chi.URLParam(r, "rowId")
	// S112 finding #12: missing parent datatable is reported as
	// "architect_datatable not found", not "architect_datatable_row
	// not found" (which would mislead the caller).
	if err := app.requireDatatableExists(dtID); err != nil {
		if errors.Is(err, models.ErrNotFound) {
			writeNotFound(w, "architect_datatable")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	body, err := decodeJSONBody(r)
	if err != nil {
		writeBadRequest(w, "body: "+err.Error())
		return
	}
	// S113 pass-2 finding #3: distinguish row-missing from DB errors.
	if _, err := scanOneJSON(app.repo.DB(),
		`SELECT body FROM architect_datatable_rows WHERE datatable_id = ? AND row_id = ?`,
		dtID, rowID); err != nil {
		if errors.Is(err, models.ErrNotFound) {
			writeNotFound(w, "architect_datatable_row")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	body["key"] = rowID
	enc, _ := json.Marshal(body)
	_, err = app.repo.DB().Exec(
		`UPDATE architect_datatable_rows SET body = ? WHERE datatable_id = ? AND row_id = ?`,
		string(enc), dtID, rowID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	writeJSONStatus(w, http.StatusOK, json.RawMessage(enc))
}

func (app *Application) handleDatatableRowDelete(w http.ResponseWriter, r *http.Request) {
	dtID := chi.URLParam(r, "datatableId")
	rowID := chi.URLParam(r, "rowId")
	// S112 finding #12: same parent-not-found classification as Update.
	if err := app.requireDatatableExists(dtID); err != nil {
		if errors.Is(err, models.ErrNotFound) {
			writeNotFound(w, "architect_datatable")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	res, err := app.repo.DB().Exec(
		`DELETE FROM architect_datatable_rows WHERE datatable_id = ? AND row_id = ?`,
		dtID, rowID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		writeNotFound(w, "architect_datatable_row")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// S113 pass-2 finding #3: differentiate ErrNotFound from DB errors so
// callers can render 500 instead of 404 on a degraded DB.
func (app *Application) requireDatatableExists(id string) error {
	var dummy string
	row := app.repo.DB().QueryRow(`SELECT id FROM architect_datatables WHERE id = ?`, id)
	if err := row.Scan(&dummy); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return models.ErrNotFound
		}
		return err
	}
	return nil
}
