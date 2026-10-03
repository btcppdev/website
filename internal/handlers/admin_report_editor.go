package handlers

import (
	"btcpp-web/internal/config"
	"html/template"
	"net/http"
)

// Saved narrative and live preview share the existing allowlisted Markdown renderer.
func (s reportSection) Markdown() template.HTML { return hackathonMarkdownHTML(s.Text) }

func AdminSponsorReportPreview(w http.ResponseWriter, r *http.Request, app *config.AppContext) {
	w.Header().Set("Cache-Control", "no-store")
	if requireGlobalAdmin(w, r, app) == nil {
		return
	}
	token, err := ensureAuthMethodsCSRF(app, r)
	if err != nil {
		http.Error(w, "Unable to prepare preview", 500)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	if r.ParseForm() != nil {
		http.Error(w, "Invalid preview request", 400)
		return
	}
	if !secureTokenEqual(token, r.PostForm.Get("csrf")) {
		http.Error(w, "Invalid form token", 403)
		return
	}
	renderReportMarkdownPreview(w, r)
}

func renderReportMarkdownPreview(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", 405)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	if r.ParseForm() != nil {
		http.Error(w, "Invalid preview request", 400)
		return
	}
	text := r.PostForm.Get("text")
	if len(text) > 12000 {
		http.Error(w, "Section exceeds 12,000 bytes", 400)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(hackathonMarkdownHTML(text)))
}
