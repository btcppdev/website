package handlers

import (
	"net/http"
	"strings"
	"time"

	"btcpp-web/external/getters"
	"btcpp-web/internal/config"
	"github.com/google/uuid"
)

func AdminServicePayments(w http.ResponseWriter, r *http.Request, app *config.AppContext) {
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("X-Robots-Tag", "noindex, nofollow")
	if requireGlobalAdmin(w, r, app) == nil {
		return
	}
	after := strings.TrimSpace(r.URL.Query().Get("after"))
	if after != "" {
		id, err := uuid.Parse(after)
		if err != nil {
			http.Error(w, "Invalid page cursor", http.StatusBadRequest)
			return
		}
		after = id.String()
	}
	review, err := getters.ReviewServicePayments(app, after, time.Now().UTC())
	if err != nil {
		app.Err.Printf("service payment review: %v", err)
		http.Error(w, "Unable to load service payments", http.StatusInternalServerError)
		return
	}
	page := &shopPage{Title: "Service payments", Admin: true, Year: time.Now().Year(), PaymentReview: review}
	renderShopTemplate(w, r, app, "admin/service_payments.tmpl", page)
}
