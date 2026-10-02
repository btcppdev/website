package handlers

import (
	"btcpp-web/external/getters"
	"btcpp-web/internal/config"
	"net/http"
	"strconv"
	"time"
)

type accountPurchasesPage struct {
	InsiderPreferencesPage
	Purchases      []getters.AccountPurchase
	Previous, Next int
}

func DashboardOrders(w http.ResponseWriter, r *http.Request, app *config.AppContext) {
	w.Header().Set("Cache-Control", "private, no-store")
	identity := requirePersonIdentity(w, r, app)
	if identity == nil {
		return
	}
	page := 1
	if value := r.URL.Query().Get("page"); value != "" {
		var err error
		page, err = strconv.Atoi(value)
		if err != nil || page < 1 || page > 1000000 {
			http.Error(w, "Invalid page", 400)
			return
		}
	}
	purchases, more, err := getters.AccountPurchases(app, identity.PersonID, page)
	if err != nil {
		app.Err.Printf("dashboard purchases: %s", err)
		http.Error(w, "Unable to load purchases", 500)
		return
	}
	hackathons, _ := getters.HasHackathonParticipantProjectsForPerson(app, identity.PersonID)
	data := accountPurchasesPage{InsiderPreferencesPage: InsiderPreferencesPage{Speaker: identity.Speaker, HasHackathonProjects: hackathons,
		ShowSponsors: hasManagedSponsorOrganization(app, identity.PersonID, "/dashboard/orders"), IsGlobalAdmin: identity.IsGlobalAdmin(),
		PendingOrgInviteCount: pendingOrganizationInviteCount(app, identity.PersonID, "/dashboard/orders"), Year: uint(time.Now().Year())}, Purchases: purchases}
	if page > 1 {
		data.Previous = page - 1
	}
	if more {
		data.Next = page + 1
	}
	if err := app.TemplateCache.ExecuteTemplate(w, "dashboard_purchases.tmpl", data); err != nil {
		app.Err.Printf("purchases render: %s", err)
	}
}
