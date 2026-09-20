package handlers

import (
	"crypto/subtle"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"btcpp-web/external/getters"
	"btcpp-web/internal/config"
)

type insiderSubscriberProjectionRequest struct {
	PersonID         string    `json:"person_id"`
	SubscriberTier   string    `json:"subscriber_tier"`
	NewsletterStatus string    `json:"newsletter_status"`
	Version          int64     `json:"version"`
	AsOf             time.Time `json:"as_of"`
}

func ApplyInsiderSubscriberProjection(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "private, no-store")
	if !validInsiderServiceAuthorization(r.Header.Get("Authorization"), ctx.Env.InsiderServiceToken) {
		w.Header().Set("WWW-Authenticate", `Bearer realm="btcpp-insider"`)
		writeInsiderServiceError(w, http.StatusUnauthorized, "A valid Insider service token is required.")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	var request insiderSubscriberProjectionRequest
	if err := decoder.Decode(&request); err != nil {
		writeInsiderServiceError(w, http.StatusBadRequest, "The subscriber projection is invalid.")
		return
	}
	applied, err := getters.ApplyInsiderSubscriberProjection(ctx, getters.InsiderSubscriberProjection{
		PersonID: request.PersonID, SubscriberTier: request.SubscriberTier,
		NewsletterStatus: request.NewsletterStatus, Version: request.Version, AsOf: request.AsOf,
	})
	if err != nil {
		ctx.Err.Printf("apply Insider subscriber projection: %s", err)
		writeInsiderServiceError(w, http.StatusUnprocessableEntity, "The subscriber projection could not be applied.")
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]bool{"applied": applied})
}

func validInsiderServiceAuthorization(header, secret string) bool {
	secret = strings.TrimSpace(secret)
	if secret == "" {
		return false
	}
	scheme, token, ok := strings.Cut(strings.TrimSpace(header), " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(strings.TrimSpace(token)), []byte(secret)) == 1
}

func writeInsiderServiceError(w http.ResponseWriter, status int, message string) {
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]string{"message": message}})
}
