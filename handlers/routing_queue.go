package handlers

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/redscaresu/fakegenesys/models"
)

// Genesys routing queue resource —
// POST/GET /api/v2/routing/queues +
// GET/PUT/DELETE /api/v2/routing/queues/{queueId}.
//
// Plus the queue-members sub-resource:
//   POST  /api/v2/routing/queues/{queueId}/members  (add users by ID)
//   GET   /api/v2/routing/queues/{queueId}/members  (list members, paged)
//   PATCH /api/v2/routing/queues/{queueId}/members  (idempotent set replace)
//
// The provider's update path uses different field names on PUT vs POST
// (e.g. `mediaSettings` flattened on create, nested on update). We
// accept either by storing the body opaquely.
//
// FK semantics: deleting a routing_queue cascades to
// routing_queue_members via the schema-level FK. fakegenesys does NOT
// pre-block destruction of queues with active members; the cascade
// handles cleanup. (The real Genesys cascade behavior matches.)

func (app *Application) registerRoutingQueueRoutes(r chi.Router) {
	r.Post("/routing/queues", app.handleRoutingQueueCreate)
	r.Get("/routing/queues", app.handleRoutingQueueList)
	r.Get("/routing/queues/{queueId}", app.handleRoutingQueueGet)
	r.Put("/routing/queues/{queueId}", app.handleRoutingQueueUpdate)
	r.Delete("/routing/queues/{queueId}", app.handleRoutingQueueDelete)
	r.Post("/routing/queues/{queueId}/members", app.handleRoutingQueueMembersAdd)
	r.Get("/routing/queues/{queueId}/members", app.handleRoutingQueueMembersList)
	r.Patch("/routing/queues/{queueId}/members", app.handleRoutingQueueMembersReplace)
}

func (app *Application) handleRoutingQueueCreate(w http.ResponseWriter, r *http.Request) {
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
	body["selfUri"] = "/api/v2/routing/queues/" + id
	body["dateCreated"] = nowZ()
	body["dateModified"] = body["dateCreated"]
	enc, _ := json.Marshal(body)
	_, err = app.repo.DB().Exec(
		`INSERT INTO routing_queues(id, name, body) VALUES (?, ?, ?)`,
		id, body["name"].(string), string(enc),
	)
	if err != nil {
		if isUniqueViolation(err) {
			writeConflict(w, "queue with name "+body["name"].(string)+" already exists")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	writeJSONStatus(w, http.StatusCreated, json.RawMessage(enc))
}

func (app *Application) handleRoutingQueueList(w http.ResponseWriter, r *http.Request) {
	rows, err := listAllJSON(app.repo.DB(),
		`SELECT body FROM routing_queues ORDER BY name`)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	pagedList(w, r, rows)
}

func (app *Application) handleRoutingQueueGet(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "queueId")
	raw, err := scanOneJSON(app.repo.DB(),
		`SELECT body FROM routing_queues WHERE id = ?`, id)
	if err != nil {
		if errors.Is(err, models.ErrNotFound) {
			writeNotFound(w, "routing_queue")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	writeJSONStatus(w, http.StatusOK, raw)
}

func (app *Application) handleRoutingQueueUpdate(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "queueId")
	patch, err := decodeJSONBody(r)
	if err != nil {
		writeBadRequest(w, "body: "+err.Error())
		return
	}
	existingRaw, err := scanOneJSON(app.repo.DB(),
		`SELECT body FROM routing_queues WHERE id = ?`, id)
	if err != nil {
		if errors.Is(err, models.ErrNotFound) {
			writeNotFound(w, "routing_queue")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	var existing map[string]any
	_ = json.Unmarshal(existingRaw, &existing)
	for k, v := range patch {
		if k == "id" || k == "selfUri" || k == "dateCreated" {
			continue
		}
		existing[k] = v
	}
	existing["dateModified"] = nowZ()
	enc, _ := json.Marshal(existing)
	name, _ := existing["name"].(string)
	_, err = app.repo.DB().Exec(
		`UPDATE routing_queues SET name = ?, body = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`,
		name, string(enc), id,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	writeJSONStatus(w, http.StatusOK, json.RawMessage(enc))
}

func (app *Application) handleRoutingQueueDelete(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "queueId")
	res, err := app.repo.DB().Exec(`DELETE FROM routing_queues WHERE id = ?`, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		writeNotFound(w, "routing_queue")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// queueMember is the per-member row shape the provider uses on POST/PATCH.
type queueMember struct {
	ID         string `json:"id"`
	RingNumber int    `json:"ringNumber,omitempty"`
}

func (app *Application) handleRoutingQueueMembersAdd(w http.ResponseWriter, r *http.Request) {
	queueID := chi.URLParam(r, "queueId")
	if err := app.requireQueueExists(queueID); err != nil {
		writeNotFound(w, "routing_queue")
		return
	}
	var members []queueMember
	if err := json.NewDecoder(r.Body).Decode(&members); err != nil {
		writeBadRequest(w, "body: "+err.Error())
		return
	}
	// S112 finding #5: ?delete=true flips POST from add to remove
	// (Genesys bulk add-or-delete endpoint).
	if r.URL.Query().Get("delete") == "true" {
		for _, m := range members {
			if m.ID == "" {
				continue
			}
			if _, err := app.repo.DB().Exec(
				`DELETE FROM routing_queue_members WHERE queue_id = ? AND user_id = ?`,
				queueID, m.ID); err != nil {
				writeError(w, http.StatusInternalServerError, "internal", err.Error())
				return
			}
		}
		w.WriteHeader(http.StatusOK)
		return
	}
	for _, m := range members {
		if m.ID == "" {
			continue
		}
		ring := m.RingNumber
		if ring == 0 {
			ring = 1
		}
		_, err := app.repo.DB().Exec(
			`INSERT OR REPLACE INTO routing_queue_members(queue_id, user_id, ring_number, joined) VALUES (?, ?, ?, 1)`,
			queueID, m.ID, ring,
		)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal", err.Error())
			return
		}
	}
	w.WriteHeader(http.StatusOK)
}

func (app *Application) handleRoutingQueueMembersReplace(w http.ResponseWriter, r *http.Request) {
	queueID := chi.URLParam(r, "queueId")
	if err := app.requireQueueExists(queueID); err != nil {
		writeNotFound(w, "routing_queue")
		return
	}
	var members []queueMember
	if err := json.NewDecoder(r.Body).Decode(&members); err != nil {
		writeBadRequest(w, "body: "+err.Error())
		return
	}
	// Idempotent set replace: wipe the queue's members, re-insert.
	tx, err := app.repo.DB().Begin()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.Exec(`DELETE FROM routing_queue_members WHERE queue_id = ?`, queueID); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	for _, m := range members {
		if m.ID == "" {
			continue
		}
		ring := m.RingNumber
		if ring == 0 {
			ring = 1
		}
		if _, err := tx.Exec(
			`INSERT INTO routing_queue_members(queue_id, user_id, ring_number, joined) VALUES (?, ?, ?, 1)`,
			queueID, m.ID, ring,
		); err != nil {
			writeError(w, http.StatusInternalServerError, "internal", err.Error())
			return
		}
	}
	if err := tx.Commit(); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	w.WriteHeader(http.StatusOK)
}

func (app *Application) handleRoutingQueueMembersList(w http.ResponseWriter, r *http.Request) {
	queueID := chi.URLParam(r, "queueId")
	if err := app.requireQueueExists(queueID); err != nil {
		writeNotFound(w, "routing_queue")
		return
	}
	rows, err := app.repo.DB().Query(
		`SELECT user_id, ring_number, joined FROM routing_queue_members WHERE queue_id = ? ORDER BY user_id`,
		queueID,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	defer rows.Close()
	entries := []json.RawMessage{}
	for rows.Next() {
		var userID string
		var ringNumber, joined int
		if err := rows.Scan(&userID, &ringNumber, &joined); err != nil {
			writeError(w, http.StatusInternalServerError, "internal", err.Error())
			return
		}
		m := map[string]any{
			"id":         userID,
			"user":       map[string]any{"id": userID, "selfUri": "/api/v2/users/" + userID},
			"ringNumber": ringNumber,
			"joined":     joined == 1,
		}
		b, _ := json.Marshal(m)
		entries = append(entries, json.RawMessage(b))
	}
	pagedList(w, r, entries)
}

// requireQueueExists returns models.ErrNotFound if the queue is not
// present. Used by member sub-resource handlers.
func (app *Application) requireQueueExists(queueID string) error {
	var dummy string
	row := app.repo.DB().QueryRow(`SELECT id FROM routing_queues WHERE id = ?`, queueID)
	if err := row.Scan(&dummy); err != nil {
		return models.ErrNotFound
	}
	return nil
}
