package api

import (
	"net/http"
	"time"

	"btcpp-web/external/getters"
)

func (s *server) entitlements(w http.ResponseWriter, r *http.Request) {
	principal, r := s.requireScope(w, r, "entitlements:self:read")
	if principal == nil {
		return
	}
	// The principal and clock are server-owned. Query parameters never select a
	// different buyer or a historical moment at which access was still active.
	at := s.now().UTC()
	if s.listAccountAccess != nil {
		access, err := s.listAccountAccess(r.Context(), principal.Person.ID, at)
		if err != nil {
			s.internalError(w, r, "list account access", err)
			return
		}
		s.writePrivate(w, r, http.StatusOK, access)
		return
	}
	grants, err := s.listEntitlements(principal.Person.ID, at)
	if err != nil {
		s.internalError(w, r, "list account entitlements", err)
		return
	}
	if grants == nil {
		grants = []getters.ServiceEntitlement{}
	}
	s.writePrivate(w, r, http.StatusOK, struct {
		AccountID    string                       `json:"accountId"`
		AsOf         time.Time                    `json:"asOf"`
		Entitlements []getters.ServiceEntitlement `json:"entitlements"`
	}{principal.Person.ID, at, grants})
}
