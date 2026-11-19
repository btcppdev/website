package api

import "net/http"

// Insider verifies each bearer request here so token revocation, scopes and
// current account roles stay authoritative in bitcoin++.
func (s *server) insiderIdentity(w http.ResponseWriter, r *http.Request) {
	scope := r.URL.Query().Get("scope")
	switch scope {
	case "insider:read", "insider:write", "insider:review", "insider:schedule", "insider:analytics", "insider:subscriber:read", "insider:subscriber:write":
	default:
		s.writeError(w, r, http.StatusBadRequest, "invalid_scope", "Choose an Insider API scope.")
		return
	}
	principal, r := s.requireScope(w, r, scope)
	if principal == nil {
		return
	}
	s.writePrivate(w, r, http.StatusOK, struct {
		ID     string   `json:"id"`
		Name   string   `json:"name"`
		Roles  []string `json:"roles"`
		Scope  string   `json:"scope"`
		Scopes []string `json:"scopes"`
	}{principal.Person.ID, principal.Person.Name, principal.Person.Roles, scope, principal.Grant.Scopes})
}
