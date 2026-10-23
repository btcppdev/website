package emaildirectory

import (
	"errors"
	"net/http"
	"net/url"
)

var errAccountRequired = errors.New("account required")

// Account contacts are derived from the authenticated person, never from an
// address supplied by the browser or from an account-free management cookie.
func (h *Handler) accountContacts(r *http.Request) ([]*Contact, string, error) {
	if h.Account == nil {
		return nil, "", errAccountRequired
	}
	person, csrf, err := h.Account(r)
	if err != nil {
		return nil, "", err
	}
	if person == "" || csrf == "" {
		return nil, "", errAccountRequired
	}
	rows, err := h.Store.DB.Query(r.Context(), `SELECT contact_id::text FROM person_emails WHERE person_id=$1 AND verified_at IS NOT NULL ORDER BY is_primary DESC,email`, person)
	if err != nil {
		return nil, "", err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return nil, "", err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, "", err
	}
	var contacts []*Contact
	for _, id := range ids {
		c, e := h.Store.Get(r.Context(), id, "")
		if e != nil {
			return nil, "", e
		}
		contacts = append(contacts, c)
	}
	return contacts, csrf, nil
}

func (h *Handler) accountManage(w http.ResponseWriter, r *http.Request) {
	contacts, csrf, err := h.accountContacts(r)
	if errors.Is(err, errAccountRequired) {
		http.Redirect(w, r, "/email-preferences", http.StatusSeeOther)
		return
	}
	if err != nil {
		http.Error(w, "Unable to load email subscriptions", 500)
		return
	}
	var current *Contact
	selected := r.URL.Query().Get("contact")
	for _, c := range contacts {
		if (selected == "" && current == nil) || c.ID == selected {
			current = c
		}
	}
	if selected != "" && current == nil {
		http.Error(w, "Email address unavailable", 404)
		return
	}
	render(w, page{AccountPage: true, Contacts: contacts, Contact: current, CSRF: csrf, Key: randomToken()})
}
func (h *Handler) accountSave(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid request", 400)
		return
	}
	contacts, csrf, err := h.accountContacts(r)
	if errors.Is(err, errAccountRequired) {
		http.Error(w, "Sign in to manage email subscriptions", 401)
		return
	}
	if err != nil {
		http.Error(w, "Unable to load email subscriptions", 500)
		return
	}
	id := r.PostForm.Get("contact")
	for _, c := range contacts {
		if c.ID == id {
			h.savePreferences(w, r, id, csrf, "/dashboard/email-subscriptions?contact="+url.QueryEscape(id))
			return
		}
	}
	http.Error(w, "Email address unavailable", 404)
}
