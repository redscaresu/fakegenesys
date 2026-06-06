package handlers

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

// voicemail userpolicy — per-user voicemail configuration.
//
// S116c: the genesyscloud Terraform provider's readUser calls
// /api/v2/voicemail/userpolicies/{userId} as part of its read-after-
// create loop. Without a 200 response it retry-loops forever and the
// apply eventually times out. fakegenesys returns a minimal default
// shape that satisfies the SDK's `Voicemailuserpolicy` model.
//
// PATCH is the documented update path. We accept any payload and
// return it (the SDK uses the response to populate state).

func (app *Application) registerVoicemailRoutes(r chi.Router) {
	r.Get("/voicemail/userpolicies/{userId}", app.handleVoicemailUserpolicyGet)
	r.Patch("/voicemail/userpolicies/{userId}", app.handleVoicemailUserpolicyPatch)
}

func defaultVoicemailUserpolicy() map[string]any {
	return map[string]any{
		"alertTimeoutSeconds":    30,
		"sendEmailNotifications": true,
		"pinConfiguration": map[string]any{
			"minimumLength": 4,
			"maximumLength": 8,
		},
	}
}

func (app *Application) handleVoicemailUserpolicyGet(w http.ResponseWriter, _ *http.Request) {
	writeJSONStatus(w, http.StatusOK, defaultVoicemailUserpolicy())
}

func (app *Application) handleVoicemailUserpolicyPatch(w http.ResponseWriter, r *http.Request) {
	body, err := decodeJSONBody(r)
	if err != nil {
		writeBadRequest(w, "body: "+err.Error())
		return
	}
	merged := defaultVoicemailUserpolicy()
	for k, v := range body {
		merged[k] = v
	}
	writeJSONStatus(w, http.StatusOK, merged)
}
