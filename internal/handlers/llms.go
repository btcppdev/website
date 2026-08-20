package handlers

import (
	"net/http"
)

// LLMSText serves static/llms.txt at the conventional root path where
// language-model clients look for it.
func LLMSText(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	http.ServeFile(w, r, "static/llms.txt")
}
