package handlers

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"btcpp-web/external/getters"
	"btcpp-web/internal/auth"
	"btcpp-web/internal/config"
	"github.com/google/uuid"
)

type serviceClosurePage struct {
	shopPage
	CSRF, ReviewID, Attempt, Closure string
	Plan                             *getters.BitcoinClosurePlan
	Review                           *getters.BitcoinClosureReview
	CanReview                        bool
}

func AdminServiceClosure(w http.ResponseWriter, r *http.Request, app *config.AppContext) {
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("X-Robots-Tag", "noindex, nofollow")
	w.Header().Set("Referrer-Policy", "no-referrer")
	id := requireGlobalAdmin(w, r, app)
	if id == nil {
		return
	}
	serveAdminServiceClosure(w, r, app, id)
}

func serveAdminServiceClosure(w http.ResponseWriter, r *http.Request, app *config.AppContext, id *auth.Identity) {
	if id == nil || !id.IsGlobalAdmin() {
		http.Error(w, "Global administrator required", 403)
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, "Method not allowed", 405)
		return
	}
	if r.Method == http.MethodPost {
		r.Body = http.MaxBytesReader(w, r.Body, 8192)
		if err := r.ParseForm(); err != nil {
			http.Error(w, "Invalid form", 400)
			return
		}
		if len(r.PostForm["csrf"]) != 1 || !secureTokenEqual(app.Session.GetString(r.Context(), authMethodsCSRFKey), r.PostForm.Get("csrf")) {
			http.Error(w, "Reload the preview and try again", 403)
			return
		}
		for _, key := range []string{"review", "attempt", "closure", "amount", "reason", "confirmed"} {
			if len(r.PostForm[key]) != 1 {
				http.Error(w, "Invalid form", 400)
				return
			}
		}
		attempt, closure, err := parseClosureInputs(r.PostForm.Get("attempt"), r.PostForm.Get("closure"))
		amount, amountErr := strconv.ParseInt(r.PostForm.Get("amount"), 10, 64)
		reviewID, reviewErr := uuid.Parse(r.PostForm.Get("review"))
		if err != nil || amountErr != nil || amount <= 0 || reviewErr != nil || reviewID.String() != r.PostForm.Get("review") || r.PostForm.Get("confirmed") != "yes" {
			http.Error(w, "Confirm a valid preview before recording the review", 400)
			return
		}
		review, err := getters.ReviewBitcoinClosureRefund(r.Context(), app, id.PersonID, reviewID.String(), attempt, closure, amount, r.PostForm.Get("reason"))
		if err != nil {
			http.Error(w, "Could not record this review. Reload the preview; payment evidence or the remaining refund may have changed.", 409)
			return
		}
		http.Redirect(w, r, "/admin/service-closure?review="+url.QueryEscape(review.ID), http.StatusSeeOther)
		return
	}
	page := &serviceClosurePage{shopPage: shopPage{Title: "Bitcoin closure refund", Admin: true, Year: time.Now().Year()}, Attempt: strings.TrimSpace(r.URL.Query().Get("attempt")), Closure: strings.TrimSpace(r.URL.Query().Get("closure"))}
	reviewID := r.URL.Query().Get("review")
	if reviewID != "" {
		parsed, err := uuid.Parse(reviewID)
		if err != nil || parsed.String() != reviewID {
			http.Error(w, "Invalid review", 400)
			return
		}
		page.Review, err = getters.InspectBitcoinClosureReview(r.Context(), app, reviewID)
		if err != nil {
			http.Error(w, "Review not found", 404)
			return
		}
		page.Plan = page.Review.Basis
	} else if page.Attempt != "" && page.Closure != "" {
		attempt, closure, err := parseClosureInputs(page.Attempt, page.Closure)
		if err != nil {
			http.Error(w, "Use a payment attempt UUID and an RFC3339 closure time with a timezone", 400)
			return
		}
		page.Plan, err = getters.PlanBitcoinClosureRefund(r.Context(), app, attempt, closure)
		if err != nil {
			http.Error(w, "No eligible settled Bitcoin payment found for this attempt", 404)
			return
		}
		page.Closure = closure.Format(time.RFC3339Nano)
		page.CanReview = len(page.Plan.ReviewReasons) == 0 && page.Plan.RemainingRefundSatoshis != nil && *page.Plan.RemainingRefundSatoshis > 0
		page.ReviewID = uuid.NewString()
	}
	var err error
	page.CSRF, err = ensureAuthMethodsCSRF(app, r)
	if err != nil {
		http.Error(w, "Unable to prepare review form", 500)
		return
	}
	if err := app.TemplateCache.ExecuteTemplate(w, "admin/service_closure.tmpl", page); err != nil {
		app.Err.Printf("closure review render: %v", err)
	}
}

func parseClosureInputs(attempt, raw string) (string, time.Time, error) {
	id, err := uuid.Parse(attempt)
	if err != nil || id.String() != attempt {
		return "", time.Time{}, getters.ErrServiceRefund
	}
	closure, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil || closure.IsZero() || closure.Nanosecond()%1000 != 0 {
		return "", time.Time{}, getters.ErrServiceRefund
	}
	return attempt, closure.UTC(), nil
}
