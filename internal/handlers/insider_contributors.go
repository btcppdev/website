package handlers

import (
	"encoding/json"
	"net/http"

	"btcpp-web/external/getters"
	"btcpp-web/internal/config"
)

// ListInsiderContributors supplies the private assignment picker with only the
// identity fields it needs. It never includes contact or billing information.
func ListInsiderContributors(w http.ResponseWriter, r *http.Request, app *config.AppContext) {
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("Content-Type", "application/json")
	if app == nil || app.Env == nil || !validInsiderServiceAuthorization(r.Header.Get("Authorization"), app.Env.InsiderServiceToken) {
		writeInsiderServiceError(w, http.StatusUnauthorized, "A valid Insider service token is required.")
		return
	}
	people, err := getters.ListSpeakersWithRole(app, "insider-contributor")
	if err != nil {
		writeInsiderServiceError(w, http.StatusServiceUnavailable, "Contributor lookup is unavailable.")
		return
	}
	type contributor struct {
		PersonID string `json:"person_id"`
		Name     string `json:"name"`
	}
	result := make([]contributor, 0, len(people))
	for _, person := range people {
		result = append(result, contributor{PersonID: person.ID, Name: person.Name})
	}
	_ = json.NewEncoder(w).Encode(result)
}
