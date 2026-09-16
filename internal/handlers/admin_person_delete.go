package handlers

import (
	"net/http"
	"net/url"
	"strings"

	"btcpp-web/external/getters"
	"btcpp-web/internal/auth"
	"btcpp-web/internal/config"
	"btcpp-web/internal/helpers"
)

type AdminPersonDeletePage struct {
	Preview     *getters.PersonDeletionPreview
	CSRF, Error string
	Year        uint
}

func AdminPersonDelete(w http.ResponseWriter, r *http.Request, app *config.AppContext) {
	w.Header().Set("Cache-Control", "private, no-store")
	admin := requireGlobalAdmin(w, r, app)
	if admin == nil {
		return
	}
	serveAdminPersonDelete(w, r, app, admin)
}

func serveAdminPersonDelete(w http.ResponseWriter, r *http.Request, app *config.AppContext, admin *auth.Identity) {
	if admin == nil || !admin.IsGlobalAdmin() {
		http.Error(w, "Global administrator required", 403)
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", 405)
		return
	}
	csrf, err := ensureAuthMethodsCSRF(app, r)
	if err != nil {
		http.Error(w, "Unable to secure deletion form", 500)
		return
	}
	personID := strings.TrimSpace(r.URL.Query().Get("person"))
	if r.Method == http.MethodPost {
		limitRequestBody(w, r, maxFormBodyBytes)
		if err := r.ParseForm(); err != nil {
			http.Error(w, "Invalid form", 400)
			return
		}
		if !secureTokenEqual(csrf, r.PostForm.Get("csrf")) {
			http.Error(w, "Invalid form token", 403)
			return
		}
		personID = strings.TrimSpace(r.PostForm.Get("person"))
	}
	preview, err := getters.PreviewPersonDeletion(app, personID)
	if err != nil {
		http.Error(w, "Person not available for deletion", 404)
		return
	}
	page := &AdminPersonDeletePage{Preview: preview, CSRF: csrf, Year: helpers.CurrentYear()}
	if r.Method == http.MethodPost {
		if r.PostForm.Get("confirmation") != "DELETE" || r.PostForm.Get("confirm_delete") != "yes" {
			page.Error = "Type DELETE and check the confirmation box to delete this account."
			w.WriteHeader(http.StatusBadRequest)
		} else if err := getters.DeletePerson(app, personID, admin.PersonID); err != nil {
			app.Err.Printf("account deletion failed: %v", err)
			page.Error = "Deletion was not completed. The account was left unchanged. Resolve shared email conflicts, or check the server log for details. You cannot delete your own account or an anonymized profile."
			w.WriteHeader(http.StatusConflict)
		} else {
			invalidateConferenceCache(app)
			invalidateWhoIsDirectoryCache()
			http.Redirect(w, r, "/admin/people?flash="+url.QueryEscape("Account deleted. Historical contributions now show a randomly assigned anonymous alias. Complete the external-data review described on the deletion page."), http.StatusSeeOther)
			return
		}
	}
	if err := app.TemplateCache.ExecuteTemplate(w, "admin/person_delete.tmpl", page); err != nil {
		app.Err.Printf("account deletion template: %v", err)
	}
}
