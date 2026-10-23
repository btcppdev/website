package handlers

import (
	"net/http"

	"btcpp-web/external/getters"
	"btcpp-web/internal/config"
	"btcpp-web/internal/helpers"
	"btcpp-web/internal/types"
)

const insiderAccountURL = "https://beta.btcpp.dev/account"

type InsiderPreferencesPage struct {
	Speaker               *types.Speaker
	Projection            *getters.InsiderSubscriberProjection
	ManageURL             string
	HasHackathonProjects  bool
	ShowSponsors          bool
	IsGlobalAdmin         bool
	PendingOrgInviteCount int
	Year                  uint
}

// DashboardInsiderPreferences exposes only Insider's coarse, read-only account
// projection. Billing, purchases, grants, newsletter consent, pauses, and
// delivery cadence remain owned and managed by the Insider application.
func DashboardInsiderPreferences(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	w.Header().Set("Cache-Control", "private, no-store")
	if !insiderVisible(ctx) {
		http.NotFound(w, r)
		return
	}
	identity := requirePersonIdentity(w, r, ctx)
	if identity == nil {
		return
	}
	projection, err := getters.GetInsiderSubscriberProjection(ctx, identity.PersonID)
	if err != nil {
		ctx.Err.Printf("/dashboard/insider projection %s: %s", identity.PersonID, err)
		http.Error(w, "Unable to load Insider status", http.StatusInternalServerError)
		return
	}
	hasHackathonProjects, _ := getters.HasHackathonParticipantProjectsForPerson(ctx, identity.PersonID)
	page := &InsiderPreferencesPage{
		Speaker: identity.Speaker, Projection: projection, ManageURL: insiderAccountURL,
		HasHackathonProjects:  hasHackathonProjects,
		ShowSponsors:          hasManagedSponsorOrganization(ctx, identity.PersonID, "/dashboard/insider"),
		IsGlobalAdmin:         identity.IsGlobalAdmin(),
		PendingOrgInviteCount: pendingOrganizationInviteCount(ctx, identity.PersonID, "/dashboard/insider"),
		Year:                  helpers.CurrentYear(),
	}
	w.Header().Set("Cache-Control", "private, no-store")
	if err := ctx.TemplateCache.ExecuteTemplate(w, "dashboard_insider.tmpl", page); err != nil {
		ctx.Err.Printf("/dashboard/insider render: %s", err)
		http.Error(w, "Unable to render Insider status", http.StatusInternalServerError)
	}
}
