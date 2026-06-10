package handlers

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/redscaresu/fakegenesys/models"
)

// Genesys group resource — POST/GET /api/v2/groups + GET/PUT/DELETE
// /api/v2/groups/{groupId}. Hard delete (not soft like users); the
// Terraform provider's destroy waits for the GET to return 404.

func (app *Application) registerGroupRoutes(r chi.Router) {
	r.Post("/groups", app.handleGroupCreate)
	r.Get("/groups", app.handleGroupList)
	r.Get("/groups/{groupId}", app.handleGroupGet)
	r.Put("/groups/{groupId}", app.handleGroupUpdate)
	r.Delete("/groups/{groupId}", app.handleGroupDelete)
	// CRITICAL[group-members-individuals-round-trip]: S122. The
	// genesyscloud provider's group Read path calls /individuals (member
	// list) unconditionally; the apply hangs forever if its count
	// doesn't converge against the HCL member_ids. POST /members is the
	// associate path. DELETE removes via ?id=u1,u2 query param. Round-
	// trip is asserted via TestContract_group_members_individuals_round_trip.
	r.Get("/groups/{groupId}/individuals", app.handleGroupIndividuals)
	// CRITICAL[group-voicemail-dual-paths]: S122. Genesys split the
	// voicemail-policy namespace — provider tries both /groups/{id}/
	// voicemail (legacy) AND /voicemail/groups/{id}/policy (modern).
	// BOTH must respond to GET and PATCH (not 501). Locked in by
	// TestContract_group_voicemail_dual_paths.
	r.Get("/groups/{groupId}/voicemail", app.handleGroupVoicemail)
	r.Patch("/groups/{groupId}/voicemail", app.handleGroupVoicemailPatch)
	// S122: POST /groups/{id}/members is how the provider associates
	// users with a group on create. Body is a list of {id, version}.
	// We acknowledge with 204 (no body). Covered by the
	// group-members-individuals-round-trip contract above.
	r.Post("/groups/{groupId}/members", app.handleGroupMembersAdd)
	r.Delete("/groups/{groupId}/members", app.handleGroupMembersDelete)
	// Modern voicemail-policy path — covered by the
	// group-voicemail-dual-paths contract above.
	r.Get("/voicemail/groups/{groupId}/policy", app.handleGroupVoicemail)
	r.Patch("/voicemail/groups/{groupId}/policy", app.handleGroupVoicemailPatch)
}

type groupMembersAddBody struct {
	MemberIDs []string `json:"memberIds"`
	Version   int      `json:"version,omitempty"`
}

func (app *Application) handleGroupMembersAdd(w http.ResponseWriter, r *http.Request) {
	groupID := chi.URLParam(r, "groupId")
	var body groupMembersAddBody
	_ = json.NewDecoder(r.Body).Decode(&body)
	for _, uid := range body.MemberIDs {
		if uid != "" {
			app.addGroupMember(groupID, uid)
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

func (app *Application) handleGroupMembersDelete(w http.ResponseWriter, r *http.Request) {
	groupID := chi.URLParam(r, "groupId")
	// Bulk delete via query param: ?id=u1,u2,...
	for _, uid := range strings.Split(r.URL.Query().Get("id"), ",") {
		uid = strings.TrimSpace(uid)
		if uid != "" {
			app.removeGroupMember(groupID, uid)
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

func (app *Application) handleGroupIndividuals(w http.ResponseWriter, r *http.Request) {
	groupID := chi.URLParam(r, "groupId")
	members := app.groupMemberIDs(groupID)
	entities := make([]map[string]any, 0, len(members))
	for _, uid := range members {
		entities = append(entities, map[string]any{
			"id":      uid,
			"selfUri": "/api/v2/users/" + uid,
		})
	}
	body := map[string]any{
		"entities":   entities,
		"total":      len(entities),
		"pageCount":  1,
		"pageNumber": 1,
		"pageSize":   25,
	}
	writeJSONStatus(w, http.StatusOK, body)
}

func defaultGroupVoicemailPolicy() map[string]any {
	return map[string]any{
		"alertTimeoutSeconds":    30,
		"sendEmailNotifications": true,
		"pinConfiguration": map[string]any{
			"minimumLength": 4,
			"maximumLength": 8,
		},
	}
}

func (app *Application) handleGroupVoicemail(w http.ResponseWriter, _ *http.Request) {
	writeJSONStatus(w, http.StatusOK, defaultGroupVoicemailPolicy())
}

func (app *Application) handleGroupVoicemailPatch(w http.ResponseWriter, r *http.Request) {
	body, err := decodeJSONBody(r)
	if err != nil {
		writeBadRequest(w, "body: "+err.Error())
		return
	}
	merged := defaultGroupVoicemailPolicy()
	for k, v := range body {
		merged[k] = v
	}
	writeJSONStatus(w, http.StatusOK, merged)
}

func (app *Application) handleGroupCreate(w http.ResponseWriter, r *http.Request) {
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
	groupType := "official"
	if s, ok := body["type"].(string); ok && s != "" {
		groupType = s
	}
	body["id"] = id
	body["type"] = groupType
	body["selfUri"] = "/api/v2/groups/" + id
	body["version"] = 1
	body["dateCreated"] = nowZ()
	body["dateModified"] = body["dateCreated"]
	enc, _ := json.Marshal(body)
	_, err = app.repo.DB().Exec(
		`INSERT INTO groups(id, name, type, body) VALUES (?, ?, ?, ?)`,
		id, body["name"].(string), groupType, string(enc),
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	writeJSONStatus(w, http.StatusCreated, json.RawMessage(enc))
}

func (app *Application) handleGroupList(w http.ResponseWriter, r *http.Request) {
	rows, err := listAllJSON(app.repo.DB(),
		`SELECT body FROM groups ORDER BY name`)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	pagedList(w, r, rows)
}

func (app *Application) handleGroupGet(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "groupId")
	raw, err := scanOneJSON(app.repo.DB(),
		`SELECT body FROM groups WHERE id = ?`, id)
	if err != nil {
		if errors.Is(err, models.ErrNotFound) {
			writeNotFound(w, "group")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	writeJSONStatus(w, http.StatusOK, raw)
}

func (app *Application) handleGroupUpdate(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "groupId")
	body, err := decodeJSONBody(r)
	if err != nil {
		writeBadRequest(w, "body: "+err.Error())
		return
	}
	existingRaw, err := scanOneJSON(app.repo.DB(),
		`SELECT body FROM groups WHERE id = ?`, id)
	if err != nil {
		if errors.Is(err, models.ErrNotFound) {
			writeNotFound(w, "group")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	var existing map[string]any
	_ = json.Unmarshal(existingRaw, &existing)
	for k, v := range body {
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
	name, _ := existing["name"].(string)
	gType, _ := existing["type"].(string)
	if gType == "" {
		gType = "official"
	}
	_, err = app.repo.DB().Exec(
		`UPDATE groups SET name = ?, type = ?, body = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`,
		name, gType, string(enc), id,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	writeJSONStatus(w, http.StatusOK, json.RawMessage(enc))
}

func (app *Application) handleGroupDelete(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "groupId")
	res, err := app.repo.DB().Exec(`DELETE FROM groups WHERE id = ?`, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		writeNotFound(w, "group")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
