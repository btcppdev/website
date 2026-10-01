package api

import (
	"errors"
	"net/http"
	"time"

	"github.com/gorilla/mux"
)

// The closure date is public service status, independent of account purchases.
// Never expose the admin identity or private review reason from the audit row.
func (s *server) serviceClosure(w http.ResponseWriter, r *http.Request) {
	service := mux.Vars(r)["service"]
	if service != "alerts" && service != "insider" {
		s.writeError(w, r, http.StatusNotFound, "not_found", "Service not found.")
		return
	}
	if s.loadServiceClosure == nil {
		s.writeError(w, r, http.StatusServiceUnavailable, "unavailable", "Service status is unavailable.")
		return
	}
	plan, err := s.loadServiceClosure(r.Context(), service)
	if err != nil {
		s.internalError(w, r, "read service closure", err)
		return
	}
	var closesAt *time.Time
	if plan != nil {
		if plan.Service != service || plan.ClosesAt.IsZero() || plan.ClosesAt.Nanosecond() != 0 {
			s.internalError(w, r, "read service closure", errors.New("invalid stored closure plan"))
			return
		}
		at := plan.ClosesAt.UTC()
		closesAt = &at
	}
	// No ETag/304 or stale public-cache response: consumers must inspect freshness
	// and persist a learned cutoff even when a later fetch fails.
	s.writePrivate(w, r, http.StatusOK, struct {
		Service  string     `json:"service"`
		AsOf     time.Time  `json:"asOf"`
		ClosesAt *time.Time `json:"closesAt"`
	}{service, s.now().UTC(), closesAt})
}
