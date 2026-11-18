package handlers

import (
	"net/http"

	"btcpp-web/internal/config"
)

func DevelopersAPI(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	w.Header().Set("Cache-Control", "public, max-age=300")
	page := struct{ SocialCardURL string }{
		SocialCardURL: siteSocialCardPath("api", "", developersAPISocialCard()),
	}
	if err := ctx.TemplateCache.ExecuteTemplate(w, "developers_api.tmpl", page); err != nil {
		ctx.Err.Printf("/developers/api render: %s", err)
		http.Error(w, "Unable to load API documentation", http.StatusInternalServerError)
	}
}
