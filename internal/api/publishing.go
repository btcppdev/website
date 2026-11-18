package api

import (
	"net/http"
	"strings"
	"time"

	"btcpp-web/internal/publishing"
)

// Streamctl contributes its complete set of manually managed schedules. Website
// recording plans already appear in the calendar and must not be duplicated.
func (s *server) putPublishingStreams(w http.ResponseWriter, r *http.Request) {
	principal, r := s.requireScope(w, r, "recordings:write")
	if principal == nil || !s.requireMutationLimit(w, r, principal, false) {
		return
	}
	if principal.Identity == nil || !principal.Identity.IsGlobalAdmin() {
		s.writeError(w, r, 403, "forbidden", "Global admin access is required.")
		return
	}
	var input struct {
		Streams []struct {
			ID         string     `json:"id"`
			Title      string     `json:"title"`
			Conference string     `json:"conference"`
			State      string     `json:"state"`
			At         *time.Time `json:"at"`
			End        *time.Time `json:"end"`
		}
	}
	if !s.decodeJSON(w, r, &input) {
		return
	}
	if len(input.Streams) > 2000 {
		s.writeError(w, r, 422, "validation_error", "At most 2,000 streams are supported.")
		return
	}
	entries := []publishing.Entry{}
	seen := map[string]bool{}
	for _, v := range input.Streams {
		if v.ID == "" || len(v.ID) > 80 || seen[v.ID] || strings.TrimSpace(v.Title) == "" || len(v.Title) > 500 || len(v.Conference) > 120 || (v.State != "scheduled" && v.State != "disabled") || (v.End != nil && (v.At == nil || !v.End.After(*v.At))) {
			s.writeError(w, r, 422, "validation_error", "Invalid stream schedule.")
			return
		}
		seen[v.ID] = true
		entries = append(entries, publishing.Entry{ID: v.ID, ChannelID: "broadcasts", Conference: v.Conference, Title: v.Title, State: v.State, At: v.At, End: v.End})
	}
	if s.app == nil || s.app.DB == nil {
		s.writeError(w, r, 503, "unavailable", "Publishing is unavailable.")
		return
	}
	if err := (publishing.Store{DB: s.app.DB}).ReplaceExternal(r.Context(), "streamer", nil, entries); err != nil {
		s.internalError(w, r, "update publishing streams", err)
		return
	}
	s.writePrivate(w, r, 200, map[string]int{"streams": len(entries)})
}
