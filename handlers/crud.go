package handlers

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/google/uuid"

	"github.com/redscaresu/fakegenesys/models"
)

// Shared CRUD helpers used by per-resource handlers in S109/S110/S111.
//
// The Genesys Cloud public API exhibits a consistent identity-resource
// pattern: UUID id, opaque JSON body, paginated list via pageNumber +
// pageSize. These helpers encapsulate that contract so per-resource
// handlers stay focused on the per-resource quirks (auth-role
// permissions, oauth-client secret reveal-once, flow lock state, ...).

// pagedList serializes the standard Genesys paged-list envelope. Callers
// pass the already-filtered+ordered slice; the helper computes the
// page metadata.
//
// Page numbering is 1-based per the spec; pageSize defaults to 25 and
// is clamped to [1, 200].
func pagedList(w http.ResponseWriter, r *http.Request, entries []json.RawMessage) {
	pageNumber, pageSize := parsePagination(r)
	total := len(entries)
	pageCount := total / pageSize
	if total%pageSize != 0 || pageCount == 0 {
		pageCount = pageCount + 1
		if total == 0 {
			pageCount = 1
		}
	}
	start := (pageNumber - 1) * pageSize
	end := start + pageSize
	if start >= total {
		start = total
	}
	if end > total {
		end = total
	}
	page := entries[start:end]
	if page == nil {
		page = []json.RawMessage{}
	}
	writeJSONStatus(w, http.StatusOK, map[string]any{
		"entities":   page,
		"pageCount":  pageCount,
		"pageNumber": pageNumber,
		"pageSize":   pageSize,
		"total":      total,
		"firstUri":   r.URL.Path + "?pageNumber=1&pageSize=" + strconv.Itoa(pageSize),
		"selfUri":    r.URL.Path + "?pageNumber=" + strconv.Itoa(pageNumber) + "&pageSize=" + strconv.Itoa(pageSize),
	})
}

func parsePagination(r *http.Request) (pageNumber, pageSize int) {
	q := r.URL.Query()
	pageNumber = 1
	pageSize = 25
	if v := q.Get("pageNumber"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			pageNumber = n
		}
	}
	if v := q.Get("pageSize"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			pageSize = n
		}
	}
	if pageSize > 200 {
		pageSize = 200
	}
	return
}

// decodeJSONBody reads the request body into a map. Used by handlers
// that don't impose a strict typed schema (mirrors the opaque-body
// pattern in fakegcp).
func decodeJSONBody(r *http.Request) (map[string]any, error) {
	var body map[string]any
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		return nil, err
	}
	if body == nil {
		return map[string]any{}, nil
	}
	return body, nil
}

// newID returns a fresh UUID string.
func newID() string { return uuid.NewString() }

// listAllJSON walks a SELECT row set and returns each row's JSON
// `body` column as a json.RawMessage. Used by per-resource list
// handlers.
func listAllJSON(db *sql.DB, query string, args ...any) ([]json.RawMessage, error) {
	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []json.RawMessage{}
	for rows.Next() {
		var b []byte
		if err := rows.Scan(&b); err != nil {
			return nil, err
		}
		out = append(out, json.RawMessage(b))
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// scanOneJSON scans a single body column. Returns models.ErrNotFound
// when no row matches.
func scanOneJSON(db *sql.DB, query string, args ...any) (json.RawMessage, error) {
	row := db.QueryRow(query, args...)
	var b []byte
	if err := row.Scan(&b); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, models.ErrNotFound
		}
		return nil, err
	}
	return json.RawMessage(b), nil
}

// writeNotFound returns a Genesys-shaped 404 with the standard code.
func writeNotFound(w http.ResponseWriter, what string) {
	writeError(w, http.StatusNotFound, "not.found", what+" not found")
}

// writeBadRequest returns a Genesys-shaped 400.
func writeBadRequest(w http.ResponseWriter, msg string) {
	writeError(w, http.StatusBadRequest, "bad.request", msg)
}

// writeConflict returns a Genesys-shaped 409.
func writeConflict(w http.ResponseWriter, msg string) {
	writeError(w, http.StatusConflict, "conflict", msg)
}

// requireStringFields validates that the named keys are present and
// non-empty strings in body. Returns the first missing-field error.
func requireStringFields(body map[string]any, fields ...string) error {
	for _, f := range fields {
		v, ok := body[f]
		if !ok {
			return errors.New("missing required field: " + f)
		}
		s, ok := v.(string)
		if !ok || s == "" {
			return errors.New("required field must be a non-empty string: " + f)
		}
	}
	return nil
}
