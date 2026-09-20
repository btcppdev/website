package handlers

import (
	"btcpp-web/internal/config"
	"btcpp-web/internal/emails"
	"encoding/json"
	"io"
	"mime"
	"net/http"
	"strings"
)

func CancelInsiderTransactionalEmail(w http.ResponseWriter, r *http.Request, app *config.AppContext) {
	w.Header().Set("Cache-Control", "private, no-store")
	if !validInsiderServiceAuthorization(r.Header.Get("Authorization"), app.Env.InsiderServiceToken) {
		http.Error(w, "Unauthorized", 401)
		return
	}
	media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || media != "application/json" {
		http.Error(w, "JSON required", 415)
		return
	}
	var request struct {
		SchemaVersion  int    `json:"schema_version"`
		IdempotencyKey string `json:"idempotency_key"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&request) != nil || decoder.Decode(new(any)) != io.EOF || request.SchemaVersion != 1 || request.IdempotencyKey == "" || len(request.IdempotencyKey) > 200 || strings.TrimSpace(request.IdempotencyKey) != request.IdempotencyKey || strings.ContainsAny(request.IdempotencyKey, "\r\n\x00") || r.Header.Get("Idempotency-Key") != request.IdempotencyKey {
		http.Error(w, "Invalid cancellation", 400)
		return
	}
	// The caller cannot select another product's job namespace or a whole recipient.
	if err := emails.SuppressJob(r.Context(), app, "btcpp:insider-transactional:"+request.IdempotencyKey); err != nil {
		http.Error(w, "Cancellation not confirmed; retry the same request", 503)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
