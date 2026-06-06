package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
)

// S116c: per-user subresources hit by the genesyscloud provider's
// updateUser / readUser path. Without these the create chain hits
// 501s and either retry-loops or fails the apply outright.
//
// Persisted in-process on the Application; lossy across restarts but
// sufficient within one infrafactory run. See routing_queue.go's
// wrapup-code association for the same pattern.

func (app *Application) registerUserSubresourceRoutes(r chi.Router) {
	r.Get("/users/{userId}/routingskills", app.handleUserRoutingSkillsList)
	r.Patch("/users/{userId}/routingskills/bulk", app.handleUserRoutingSkillsBulkPatch)
	r.Get("/users/{userId}/routinglanguages", app.handleUserRoutingLanguagesList)
	r.Patch("/users/{userId}/routinglanguages/bulk", app.handleUserRoutingLanguagesBulkPatch)
	// /users/search — the provider's destroy path calls this to verify
	// the soft-delete landed. POST with a body of {pageNumber, query: [{...}]}.
	r.Post("/users/search", app.handleUserSearch)
}

// userProficiencyRef matches the SDK's Userroutingskillpost /
// Userroutinglanguagepost shape used in the bulk PATCH body.
type userProficiencyRef struct {
	ID          string  `json:"id"`
	Proficiency float64 `json:"proficiency,omitempty"`
}

func (app *Application) handleUserRoutingSkillsList(w http.ResponseWriter, r *http.Request) {
	userID := chi.URLParam(r, "userId")
	app.subresMu.Lock()
	skills := app.userSkills[userID]
	app.subresMu.Unlock()
	entries := make([]json.RawMessage, 0, len(skills))
	for _, s := range skills {
		b, _ := json.Marshal(map[string]any{
			"id":          s.ID,
			"proficiency": s.Proficiency,
		})
		entries = append(entries, json.RawMessage(b))
	}
	pagedList(w, r, entries)
}

func (app *Application) handleUserRoutingSkillsBulkPatch(w http.ResponseWriter, r *http.Request) {
	userID := chi.URLParam(r, "userId")
	var refs []userProficiencyRef
	if err := json.NewDecoder(r.Body).Decode(&refs); err != nil {
		writeBadRequest(w, "body: "+err.Error())
		return
	}
	app.subresMu.Lock()
	if app.userSkills == nil {
		app.userSkills = map[string][]userProficiencyRef{}
	}
	app.userSkills[userID] = refs
	app.subresMu.Unlock()
	writeJSONStatus(w, http.StatusOK, refs)
}

func (app *Application) handleUserRoutingLanguagesList(w http.ResponseWriter, r *http.Request) {
	userID := chi.URLParam(r, "userId")
	app.subresMu.Lock()
	langs := app.userLanguages[userID]
	app.subresMu.Unlock()
	entries := make([]json.RawMessage, 0, len(langs))
	for _, l := range langs {
		b, _ := json.Marshal(map[string]any{
			"id":          l.ID,
			"proficiency": l.Proficiency,
		})
		entries = append(entries, json.RawMessage(b))
	}
	pagedList(w, r, entries)
}

func (app *Application) handleUserRoutingLanguagesBulkPatch(w http.ResponseWriter, r *http.Request) {
	userID := chi.URLParam(r, "userId")
	var refs []userProficiencyRef
	if err := json.NewDecoder(r.Body).Decode(&refs); err != nil {
		writeBadRequest(w, "body: "+err.Error())
		return
	}
	app.subresMu.Lock()
	if app.userLanguages == nil {
		app.userLanguages = map[string][]userProficiencyRef{}
	}
	app.userLanguages[userID] = refs
	app.subresMu.Unlock()
	writeJSONStatus(w, http.StatusOK, refs)
}

// handleUserSearch — POST /api/v2/users/search.
//
// The genesyscloud provider's destroy verification at
// resource_genesyscloud_user_utils.go::getDeletedUserId posts a search
// with two EXACT clauses (email + state=deleted) and inspects
// `Usersearchresponse.Results`. Note: the response key is "results",
// NOT the "entities" used by every other paged list endpoint — this is
// the public Genesys API's actual contract.
func (app *Application) handleUserSearch(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Query []struct {
			VarType string   `json:"type"`
			Fields  []string `json:"fields"`
			Value   string   `json:"value"`
			Values  []string `json:"values"`
		} `json:"query"`
		PageSize   int `json:"pageSize"`
		PageNumber int `json:"pageNumber"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	rows, err := listAllJSON(app.repo.DB(),
		`SELECT body FROM users ORDER BY name`)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	matched := make([]json.RawMessage, 0, len(rows))
	for _, raw := range rows {
		var u map[string]any
		if jsonErr := json.Unmarshal(raw, &u); jsonErr != nil {
			continue
		}
		if userSearchMatches(u, req.Query) {
			matched = append(matched, raw)
		}
	}
	pageSize := req.PageSize
	if pageSize <= 0 {
		pageSize = 25
	}
	pageNumber := req.PageNumber
	if pageNumber <= 0 {
		pageNumber = 1
	}
	body := map[string]any{
		"results":    matched,
		"total":      len(matched),
		"pageCount":  1,
		"pageNumber": pageNumber,
		"pageSize":   pageSize,
	}
	writeJSONStatus(w, http.StatusOK, body)
}

// userSearchMatches: minimal AND of clauses over the named fields.
// Empty Query → match-all (provider's destroy path passes an explicit
// email + state filter, so the match-all branch is reserved for
// harness probes).
func userSearchMatches(user map[string]any, query []struct {
	VarType string   `json:"type"`
	Fields  []string `json:"fields"`
	Value   string   `json:"value"`
	Values  []string `json:"values"`
}) bool {
	if len(query) == 0 {
		return true
	}
	for _, q := range query {
		needles := q.Values
		if q.Value != "" {
			needles = append(needles, q.Value)
		}
		matched := false
		fields := q.Fields
		if len(fields) == 0 {
			fields = []string{"name", "email"}
		}
		for _, field := range fields {
			val, _ := user[field].(string)
			if val == "" {
				continue
			}
			for _, needle := range needles {
				if needle == val {
					matched = true
					break
				}
			}
			if matched {
				break
			}
		}
		if !matched {
			return false
		}
	}
	return true
}
