package api

import (
	"net/http"
	"strings"

	"btcpp-web/external/getters"
)

func (s *server) putConferenceBroadcast(w http.ResponseWriter, r *http.Request) {
	principal, r := s.requireScope(w, r, "recordings:write")
	if principal == nil || !s.requireMutationLimit(w, r, principal, false) {
		return
	}
	conf, ok := s.adminConference(w, r, principal)
	if !ok {
		return
	}
	var input struct {
		recordingBroadcastUpdateDTO
		Title string `json:"title"`
	}
	if !s.decodeJSON(w, r, &input) {
		return
	}
	input.State = strings.ToLower(strings.TrimSpace(input.State))
	if input.State != "scheduled" && input.State != "live" && input.State != "ended" && input.State != "failed" {
		s.writeError(w, r, http.StatusUnprocessableEntity, "validation_error", "state must be scheduled, live, ended, or failed.")
		return
	}
	if (input.State == "live" && strings.TrimSpace(input.HLSURL) == "") || !validOptionalHTTPURL(&input.HLSURL) || !validOptionalHTTPURL(&input.XBroadcastURL) || len([]rune(input.Title)) > 200 {
		s.writeError(w, r, http.StatusUnprocessableEntity, "validation_error", "Live broadcasts require an HTTP(S) HLS URL; titles must be at most 200 characters and broadcast links must be HTTP(S) URLs.")
		return
	}
	if s.upsertConferenceBroadcast == nil || s.loadConferenceBroadcast == nil {
		s.writeError(w, r, http.StatusServiceUnavailable, "unavailable", "Conference broadcasting is unavailable.")
		return
	}
	previous, err := s.loadConferenceBroadcast(conf.Ref)
	if err != nil {
		s.internalError(w, r, "load conference broadcast", err)
		return
	}
	broadcast, err := s.upsertConferenceBroadcast(conf.Ref, getters.ConferenceBroadcastUpdate{
		Title: input.Title, State: input.State, HLSURL: input.HLSURL, XBroadcastURL: input.XBroadcastURL, Now: s.now(),
	})
	if err != nil {
		s.internalError(w, r, "update conference broadcast", err)
		return
	}
	if previous == nil || previous.State != broadcast.State || previous.Title != broadcast.Title || previous.HLSURL != broadcast.HLSURL || previous.XBroadcastURL != broadcast.XBroadcastURL {
		s.auditMutation(r, principal, "api_conference_broadcast_updated", map[string]any{"conference": conf.Tag, "state": broadcast.State})
	}
	s.writePrivate(w, r, http.StatusOK, recordingBroadcastFromDomain(&broadcast.RecordingBroadcast, s.now()))
}
