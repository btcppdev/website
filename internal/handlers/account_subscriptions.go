package handlers

import (
	"fmt"
	"net/http"
	"time"

	"btcpp-web/external/getters"
	"btcpp-web/internal/config"
)

type accountSubscription struct {
	Name, Description, Status, URL, BillingStatus, CheckedAt string
	Periods                                                  []string
}

type accountSubscriptionsPage struct {
	InsiderPreferencesPage
	Subscriptions []accountSubscription
}

func cardSubscriptionLabel(item getters.AccountCardSubscription) string {
	switch item.State.State {
	case "active":
		return "Card subscription active"
	case "cancelling":
		return "Cancellation scheduled"
	case "cancelled":
		return "Card subscription canceled"
	case "paused":
		return "Card collection paused"
	case "renewal_unpaid":
		return "Renewal payment overdue"
	case "initial_unpaid":
		return "First payment pending"
	case "expired":
		return "Card subscription expired"
	case "disputed":
		return "Payment disputed"
	default:
		return "Card subscription status pending"
	}
}

func DashboardSubscriptions(w http.ResponseWriter, r *http.Request, app *config.AppContext) {
	w.Header().Set("Cache-Control", "private, no-store")
	identity := requirePersonIdentity(w, r, app)
	if identity == nil {
		return
	}
	grants, err := getters.ActiveServiceEntitlements(app, identity.PersonID, time.Now())
	if err != nil {
		app.Err.Printf("subscriptions grants: %s", err)
		http.Error(w, "Unable to load subscriptions", 500)
		return
	}
	projection, err := getters.GetInsiderSubscriberProjection(app, identity.PersonID)
	if err != nil {
		app.Err.Printf("subscriptions Insider: %s", err)
		http.Error(w, "Unable to load subscriptions", 500)
		return
	}
	cards, err := getters.AccountCardSubscriptions(app, identity.PersonID)
	if err != nil {
		app.Err.Printf("subscriptions billing: %s", err)
		http.Error(w, "Unable to load subscriptions", 500)
		return
	}
	alerts := accountSubscription{Name: "Alerts", Description: "Software release notifications by email and Telegram.", Status: "3 free projects", URL: "https://alerts.btcpp.dev/alerts"}
	var quantity int64
	for _, grant := range grants {
		if grant.ProductCode != "alerts.repository-slots" {
			continue
		}
		quantity += grant.Quantity
		period := fmt.Sprintf("%d additional paid projects", grant.Quantity)
		if grant.EndsAt != nil {
			period += " through " + grant.EndsAt.UTC().Format("2 Jan 2006, 15:04 UTC")
		}
		alerts.Periods = append(alerts.Periods, period)
	}
	if quantity > 0 {
		alerts.Status = fmt.Sprintf("3 free + %d paid projects", quantity)
	}
	insider := accountSubscription{Name: "Insider Edition", Description: "Bitcoin news, deep dives, interviews, and editorials.", Status: "No membership reported", URL: insiderAccountURL}
	if projection != nil {
		switch projection.SubscriberTier {
		case "paid":
			insider.Status = "Paid membership"
		case "free":
			insider.Status = "Free subscriber"
		}
		insider.CheckedAt = projection.AsOf.UTC().Format("2 Jan 2006, 15:04 UTC")
	}
	items := []accountSubscription{alerts, insider}
	for _, card := range cards {
		index := 0
		if card.Service == "insider" {
			index = 1
		} else if card.Service != "alerts" {
			continue
		}
		items[index].BillingStatus = cardSubscriptionLabel(card)
		if card.ObservedAt != nil {
			items[index].BillingStatus += " · checked " + card.ObservedAt.UTC().Format("2 Jan 2006, 15:04 UTC")
		}
	}
	hackathons, _ := getters.HasHackathonParticipantProjectsForPerson(app, identity.PersonID)
	page := accountSubscriptionsPage{InsiderPreferencesPage: InsiderPreferencesPage{
		Speaker: identity.Speaker, HasHackathonProjects: hackathons, IsGlobalAdmin: identity.IsGlobalAdmin(),
		ShowSponsors:          hasManagedSponsorOrganization(app, identity.PersonID, "/dashboard/subscriptions"),
		PendingOrgInviteCount: pendingOrganizationInviteCount(app, identity.PersonID, "/dashboard/subscriptions"),
		Year:                  uint(time.Now().Year()),
	}, Subscriptions: items}
	if err := app.TemplateCache.ExecuteTemplate(w, "dashboard_subscriptions.tmpl", page); err != nil {
		app.Err.Printf("subscriptions render: %s", err)
	}
}
