package handlers

import (
	"context"
	"errors"
	"mime"
	"net/http"
	"strings"

	"btcpp-web/external/getters"
	"btcpp-web/internal/config"
	"github.com/google/uuid"
)

func ServiceOpenNodeCallback(w http.ResponseWriter, r *http.Request, app *config.AppContext) {
	serviceOpenNodeCallback(w, r, app, getters.ReconcileOpenNodeServiceCharge)
}

func serviceOpenNodeCallback(w http.ResponseWriter, r *http.Request, app *config.AppContext, reconcile func(context.Context, *config.AppContext, string) (bool, error)) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if app == nil || app.Env == nil || strings.TrimSpace(app.Env.OpenNode.Key) == "" {
		http.Error(w, "Payment service unavailable", http.StatusServiceUnavailable)
		return
	}
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/x-www-form-urlencoded" {
		http.Error(w, "Expected form payload", http.StatusUnsupportedMediaType)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid payload", http.StatusBadRequest)
		return
	}
	ids, signatures := r.PostForm["id"], r.PostForm["hashed_order"]
	if len(ids) != 1 || len(signatures) != 1 {
		http.Error(w, "Invalid signature", http.StatusUnauthorized)
		return
	}
	id, err := uuid.Parse(ids[0])
	if err != nil || id.String() != ids[0] || !validHash(app.Env.OpenNode.Key, ids[0], signatures[0]) {
		http.Error(w, "Invalid signature", http.StatusUnauthorized)
		return
	}
	// The HMAC covers only the ID. Ignore payload status, price and order_id;
	// the reconciler independently fetches all payment evidence from OpenNode.
	_, err = reconcile(r.Context(), app, ids[0])
	if errors.Is(err, getters.ErrServiceFulfillment) {
		// Settlement is durable, but this purchase needs operator review. Do not
		// encourage provider retries to change a canceled/edited purchase.
		w.WriteHeader(http.StatusAccepted)
		return
	}
	if err != nil {
		http.Error(w, "Payment verification pending", http.StatusServiceUnavailable)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
