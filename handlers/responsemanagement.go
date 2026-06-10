package handlers

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/redscaresu/fakegenesys/models"
)

// responsemanagement_response — `/api/v2/responsemanagement/responses`
// (POST/GET/list) + `/{responseId}` (GET/PUT/DELETE).
//
// Body is opaque (rich-text content). fakegenesys stores it verbatim.

func (app *Application) registerResponseManagementRoutes(r chi.Router) {
	r.Post("/responsemanagement/responses", app.handleResponseCreate)
	r.Get("/responsemanagement/responses", app.handleResponseList)
	r.Get("/responsemanagement/responses/{responseId}", app.handleResponseGet)
	r.Put("/responsemanagement/responses/{responseId}", app.handleResponseUpdate)
	r.Delete("/responsemanagement/responses/{responseId}", app.handleResponseDelete)
	// CRITICAL[responsemanagement-library-crud-round-trip]: S122c.
	// Libraries are the parent container for responses. The provider
	// requires this resource before responses can be created. Create
	// must return 200 with a non-empty `id`; subsequent GET by that id
	// must return the stored library (not 404/501). In-memory storage
	// — sufficient within one infrafactory run. Locked in by
	// TestContract_responsemanagement_library_crud_round_trip.
	r.Post("/responsemanagement/libraries", app.handleLibraryCreate)
	r.Get("/responsemanagement/libraries", app.handleLibraryList)
	r.Get("/responsemanagement/libraries/{libraryId}", app.handleLibraryGet)
	r.Put("/responsemanagement/libraries/{libraryId}", app.handleLibraryUpdate)
	r.Delete("/responsemanagement/libraries/{libraryId}", app.handleLibraryDelete)
}

func (app *Application) handleLibraryCreate(w http.ResponseWriter, r *http.Request) {
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
	body["selfUri"] = "/api/v2/responsemanagement/libraries/" + id
	if body["division"] == nil {
		body["division"] = map[string]any{
			"id":   fakegenesysHomeDivisionID,
			"name": "Home",
		}
	}
	app.storeResponseLibrary(id, body)
	writeJSONStatus(w, http.StatusOK, body)
}

func (app *Application) handleLibraryList(w http.ResponseWriter, r *http.Request) {
	entries := app.listResponseLibraries()
	body := map[string]any{
		"entities":   entries,
		"total":      len(entries),
		"pageCount":  1,
		"pageNumber": 1,
		"pageSize":   25,
	}
	writeJSONStatus(w, http.StatusOK, body)
}

func (app *Application) handleLibraryGet(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "libraryId")
	lib := app.getResponseLibrary(id)
	if lib == nil {
		writeNotFound(w, "responsemanagement_library")
		return
	}
	writeJSONStatus(w, http.StatusOK, lib)
}

func (app *Application) handleLibraryUpdate(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "libraryId")
	lib := app.getResponseLibrary(id)
	if lib == nil {
		writeNotFound(w, "responsemanagement_library")
		return
	}
	patch, err := decodeJSONBody(r)
	if err != nil {
		writeBadRequest(w, "body: "+err.Error())
		return
	}
	for k, v := range patch {
		switch k {
		case "id", "selfUri":
		default:
			lib[k] = v
		}
	}
	app.storeResponseLibrary(id, lib)
	writeJSONStatus(w, http.StatusOK, lib)
}

func (app *Application) handleLibraryDelete(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "libraryId")
	app.deleteResponseLibrary(id)
	w.WriteHeader(http.StatusNoContent)
}

func (app *Application) handleResponseCreate(w http.ResponseWriter, r *http.Request) {
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
	body["selfUri"] = "/api/v2/responsemanagement/responses/" + id
	enc, _ := json.Marshal(body)
	_, err = app.repo.DB().Exec(
		`INSERT INTO responsemanagement_responses(id, name, body) VALUES (?, ?, ?)`,
		id, body["name"].(string), string(enc),
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	writeJSONStatus(w, http.StatusCreated, json.RawMessage(enc))
}

func (app *Application) handleResponseList(w http.ResponseWriter, r *http.Request) {
	rows, err := listAllJSON(app.repo.DB(),
		`SELECT body FROM responsemanagement_responses ORDER BY name`)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	pagedList(w, r, rows)
}

func (app *Application) handleResponseGet(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "responseId")
	raw, err := scanOneJSON(app.repo.DB(),
		`SELECT body FROM responsemanagement_responses WHERE id = ?`, id)
	if err != nil {
		if errors.Is(err, models.ErrNotFound) {
			writeNotFound(w, "responsemanagement_response")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	writeJSONStatus(w, http.StatusOK, raw)
}

func (app *Application) handleResponseUpdate(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "responseId")
	patch, err := decodeJSONBody(r)
	if err != nil {
		writeBadRequest(w, "body: "+err.Error())
		return
	}
	existingRaw, err := scanOneJSON(app.repo.DB(),
		`SELECT body FROM responsemanagement_responses WHERE id = ?`, id)
	if err != nil {
		if errors.Is(err, models.ErrNotFound) {
			writeNotFound(w, "responsemanagement_response")
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
		`UPDATE responsemanagement_responses SET name = ?, body = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`,
		name, string(enc), id,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	writeJSONStatus(w, http.StatusOK, json.RawMessage(enc))
}

func (app *Application) handleResponseDelete(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "responseId")
	res, err := app.repo.DB().Exec(`DELETE FROM responsemanagement_responses WHERE id = ?`, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		writeNotFound(w, "responsemanagement_response")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
