package handlers

import (
	"errors"
	"net/http"
	"net/url"
	"strings"

	"btcpp-web/external/getters"
	"btcpp-web/internal/config"

	"github.com/gorilla/mux"
)

func SponsorDashboardPayoutPaid(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	w.Header().Set("Cache-Control", "private, no-store")
	id, memberships, ok := sponsorDashboardIdentity(w, r, ctx)
	if !ok {
		return
	}
	organizationID := strings.TrimSpace(mux.Vars(r)["organizationID"])
	if !sponsorMembershipCanManage(organizationMembershipByID(memberships, organizationID)) {
		http.Error(w, "Only organization owners and managers can record sponsor payouts.", http.StatusForbidden)
		return
	}
	limitRequestBody(w, r, maxFormBodyBytes)
	if err := r.ParseForm(); err != nil || !secureTokenEqual(ctx.Session.GetString(r.Context(), authMethodsCSRFKey), r.PostForm.Get("csrf")) {
		http.Error(w, "Invalid form token", http.StatusBadRequest)
		return
	}
	if r.PostForm.Get("ConfirmPaid") != "yes" {
		http.Error(w, "Confirm that you have already sent this payment.", http.StatusBadRequest)
		return
	}
	changed, err := getters.MarkSponsorPayoutPaid(ctx, organizationID, id.PersonID,
		mux.Vars(r)["distributionID"], r.PostForm.Get("PaymentReference"))
	if err != nil {
		switch {
		case errors.Is(err, getters.ErrSponsorPayoutReference):
			http.Error(w, err.Error(), http.StatusBadRequest)
		case errors.Is(err, getters.ErrSponsorPayoutUnavailable):
			http.Error(w, "This payout is not available to mark as paid. Refresh the dashboard or contact organizers.", http.StatusConflict)
		default:
			ctx.Err.Printf("/dashboard/sponsor/%s payout confirmation: %s", organizationID, err)
			http.Error(w, "Unable to record payment. Refresh the dashboard before trying again.", http.StatusInternalServerError)
		}
		return
	}
	message := "Payment recorded. Thank you!"
	if !changed {
		message = "This payment was already recorded. No further payment is needed."
	}
	http.Redirect(w, r, "/dashboard/sponsor/"+url.PathEscape(organizationID)+"?flash="+url.QueryEscape(message)+"#payouts", http.StatusSeeOther)
}
