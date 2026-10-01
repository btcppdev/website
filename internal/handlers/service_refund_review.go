package handlers

import (
	"context"
	"net/http"
	"strings"
	"time"

	"btcpp-web/external/getters"
	"btcpp-web/internal/auth"
	"btcpp-web/internal/config"
	"github.com/google/uuid"
)

func AdminServiceRefunds(w http.ResponseWriter, r *http.Request, app *config.AppContext) {
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("X-Robots-Tag", "noindex, nofollow")
	w.Header().Set("Referrer-Policy", "no-referrer")
	id := requireGlobalAdmin(w, r, app)
	if id == nil {
		return
	}
	serveAdminServiceRefunds(w, r, app, id)
}

func serveAdminServiceRefunds(w http.ResponseWriter, r *http.Request, app *config.AppContext, id *auth.Identity) {
	if id == nil || !id.IsGlobalAdmin() {
		http.Error(w, "Global administrator required", 403)
		return
	}
	if r.Method == http.MethodPost {
		r.Body = http.MaxBytesReader(w, r.Body, 8192)
		if err := r.ParseForm(); err != nil {
			http.Error(w, "Invalid form", 400)
			return
		}
		if len(r.PostForm["csrf"]) != 1 || !secureTokenEqual(app.Session.GetString(r.Context(), authMethodsCSRFKey), r.PostForm.Get("csrf")) {
			http.Error(w, "Reload this page and try again", 403)
			return
		}
		for _, field := range []string{"refund_reference", "reversal", "reason"} {
			if len(r.PostForm[field]) != 1 {
				http.Error(w, "Invalid form", 400)
				return
			}
		}
		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()
		err := getters.ReconcileServiceRefundTax(ctx, app, id.PersonID, r.PostForm.Get("refund_reference"), strings.TrimSpace(r.PostForm.Get("reversal")), r.PostForm.Get("reason"))
		if err != nil {
			http.Error(w, "Could not verify this reversal. The refund remains available for inspection; check the provider reference and reload before retrying.", 409)
			return
		}
		http.Redirect(w, r, "/admin/service-refunds", http.StatusSeeOther)
		return
	}
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, "Method not allowed", 405)
		return
	}
	attempt := strings.TrimSpace(r.URL.Query().Get("attempt"))
	if attempt != "" {
		if id, err := uuid.Parse(attempt); err != nil || id.String() != attempt {
			http.Error(w, "Invalid payment attempt", 400)
			return
		}
	}
	rows, err := getters.ListServiceRefunds(r.Context(), app, attempt)
	if err != nil {
		http.Error(w, "Unable to load refunds", 500)
		return
	}
	csrf, err := ensureAuthMethodsCSRF(app, r)
	if err != nil {
		http.Error(w, "Unable to prepare review", 500)
		return
	}
	renderShopTemplate(w, r, app, "admin/service_refunds.tmpl", &shopPage{Title: "Service refunds", Admin: true, Year: time.Now().Year(), RefundReview: rows, RefundCSRF: csrf})
}
