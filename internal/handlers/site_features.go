package handlers

import (
	"btcpp-web/internal/config"
	"bytes"
	"context"
	"net/http"
	"time"
)

// Missing configuration or an unavailable database must not expose an unlaunched product.
func insiderVisible(app *config.AppContext) bool {
	if app.DB == nil {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	var enabled bool
	return app.DB.QueryRow(ctx, `SELECT enabled FROM site_features WHERE name='insider'`).Scan(&enabled) == nil && enabled
}

func AdminSiteFeatures(w http.ResponseWriter, r *http.Request, app *config.AppContext) {
	w.Header().Set("Cache-Control", "private, no-store")
	id := requireGlobalAdmin(w, r, app)
	if id == nil {
		return
	}
	if r.Method == http.MethodPost {
		r.Body = http.MaxBytesReader(w, r.Body, 4096)
		if r.ParseForm() != nil || !secureTokenEqual(app.Session.GetString(r.Context(), authMethodsCSRFKey), r.PostFormValue("csrf")) {
			http.Error(w, "Invalid form token", http.StatusForbidden)
			return
		}
		_, err := app.DB.Exec(r.Context(), `INSERT INTO site_features(name,enabled,updated_by) VALUES('insider',$1,$2) ON CONFLICT(name) DO UPDATE SET enabled=EXCLUDED.enabled,updated_by=EXCLUDED.updated_by,updated_at=now()`, r.PostFormValue("insider") == "on", id.PersonID)
		if err != nil {
			http.Error(w, "Unable to save visibility", 503)
			return
		}
		http.Redirect(w, r, "/admin/site-features?saved=1", http.StatusSeeOther)
		return
	}
	csrf, err := ensureAuthMethodsCSRF(app, r)
	if err != nil {
		http.Error(w, "Unable to prepare form", 500)
		return
	}
	page := struct {
		CSRF           string
		Enabled, Saved bool
	}{csrf, insiderVisible(app), r.URL.Query().Get("saved") == "1"}
	var out bytes.Buffer
	if err := app.TemplateCache.ExecuteTemplate(&out, "admin/site_features.tmpl", page); err != nil {
		http.Error(w, "Unable to render settings", 500)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(out.Bytes())
}
