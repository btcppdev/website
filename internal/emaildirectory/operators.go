package emaildirectory

import "net/http"

// Operational recipients are only available to the authenticated Alerts service.
// Include admins without email so Alerts can still notify their linked bot chat.
func (h *Handler) operators(w http.ResponseWriter, r *http.Request) {
	if !h.authorized(w, r) {
		return
	}
	if r.PathValue("service") != "alerts" {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}
	rows, err := h.Store.DB.Query(r.Context(), `SELECT p.id::text,coalesce((SELECT c.email::text FROM person_emails e JOIN email_contacts c ON c.id=e.contact_id WHERE e.person_id=p.id AND e.verified_at IS NOT NULL AND c.erased_at IS NULL ORDER BY e.is_primary DESC,c.email LIMIT 1),'') FROM people p WHERE NOT p.is_deleted_account AND EXISTS(SELECT 1 FROM people_roles r WHERE r.person_id=p.id AND r.scope='alerts' AND r.position='admin') ORDER BY p.id`)
	if err != nil {
		respond(w, nil, err)
		return
	}
	defer rows.Close()
	type recipient struct {
		AccountID string `json:"account_id"`
		Email     string `json:"email,omitempty"`
	}
	recipients := []recipient{}
	for rows.Next() {
		var v recipient
		if err = rows.Scan(&v.AccountID, &v.Email); err != nil {
			respond(w, nil, err)
			return
		}
		recipients = append(recipients, v)
	}
	respond(w, struct {
		Recipients []recipient `json:"recipients"`
	}{recipients}, rows.Err())
}
