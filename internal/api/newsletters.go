package api

import (
	"errors"
	"net/http"

	"btcpp-web/external/getters"
)

func (s *server) newsletters(w http.ResponseWriter, r *http.Request) {
	scope := "newsletters:self:read"
	if r.Method == http.MethodPut {
		scope = "newsletters:self:write"
	}
	principal, r := s.requireScope(w, r, scope)
	if principal == nil {
		return
	}
	if r.Method == http.MethodGet {
		preferences, err := s.listNewsletters(principal.Person.ID)
		if err != nil {
			s.internalError(w, r, "list newsletter preferences", err)
			return
		}
		s.writePrivate(w, r, 200, preferences)
		return
	}
	var in struct {
		Email      string `json:"email"`
		Newsletter string `json:"newsletter"`
		Subscribed *bool  `json:"subscribed"`
	}
	if !s.decodeJSON(w, r, &in) {
		return
	}
	key := r.Header.Get("Idempotency-Key")
	if in.Subscribed == nil || len(key) < 16 || len(key) > 128 {
		s.writeError(w, r, 422, "invalid_preference", "Provide an explicit subscription choice and an Idempotency-Key of 16–128 characters.")
		return
	}
	source := principal.Grant.ClientID
	if source == "" {
		source = "personal_access_token"
	}
	err := s.setNewsletter(principal.Person.ID, source, key, getters.NewsletterPreference{Email: in.Email, Newsletter: in.Newsletter, Subscribed: *in.Subscribed})
	switch {
	case errors.Is(err, getters.ErrNewsletterEmail):
		s.writeError(w, r, 403, "unverified_email", "Choose a verified email address on your account.")
	case errors.Is(err, getters.ErrNewsletterConflict):
		s.writeError(w, r, 409, "idempotency_conflict", "Use a new idempotency key for a different preference change.")
	case errors.Is(err, getters.ErrNewsletterInput):
		s.writeError(w, r, 422, "invalid_preference", "Choose a supported newsletter and a valid account email.")
	case err != nil:
		s.internalError(w, r, "update newsletter preference", err)
	default:
		s.writePrivate(w, r, 200, map[string]bool{"recorded": true})
	}
}
