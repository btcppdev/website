package handlers

import (
	"context"
	"encoding/json"
	"errors"

	"btcpp-web/external/getters"
	"btcpp-web/internal/alertsstripe"
	"btcpp-web/internal/config"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stripe/stripe-go/v86"
)

// Called only after StripeCallback verifies the webhook signature. Event data
// locates our intent; authenticated provider lookups establish billing facts.
func handleAlertsStripeEvent(ctx context.Context, app *config.AppContext, event stripe.Event) (bool, error) {
	return processAlertsStripeEvent(ctx, app, event, alertsstripe.NewClient(event.Livemode))
}

func processAlertsStripeEvent(ctx context.Context, app *config.AppContext, event stripe.Event, client alertsstripe.Client) (bool, error) {
	intent, sessionID, invoiceID, handled, err := alertsStripeEventReferences(event)
	if !handled || err != nil {
		return handled, err
	}
	if _, err = uuid.Parse(intent); err != nil {
		return true, alertsstripe.ErrCheckout
	}
	var person string
	err = app.DB.QueryRow(app.DatabaseContext(), `SELECT coalesce(person_id::text,'') FROM alerts_stripe_checkouts WHERE id=$1`, intent).Scan(&person)
	if errors.Is(err, pgx.ErrNoRows) {
		return true, nil
	}
	if err != nil {
		return true, err
	}
	// Account deletion cannot recreate access; retained financial evidence remains
	// available for operator reconciliation.
	if person == "" {
		return true, nil
	}
	if event.Type == "checkout.session.expired" {
		return true, getters.CloseExpiredAlertsStripeCheckout(ctx, app, person, intent, sessionID, client)
	}
	if sessionID != "" {
		if _, err = getters.ResolveAlertsStripeSubscription(ctx, app, person, intent, client, sessionID); err != nil {
			return true, err
		}
	} else {
		var bound bool
		if err = app.DB.QueryRow(app.DatabaseContext(), `SELECT EXISTS(SELECT 1 FROM alerts_stripe_subscriptions WHERE checkout_id=$1)`, intent).Scan(&bound); err != nil {
			return true, err
		}
		if !bound {
			if _, err = getters.ResolveAlertsStripeSubscription(ctx, app, person, intent, client); err != nil {
				return true, err
			}
		}
	}
	if invoiceID != "" {
		_, err = getters.ReconcileAlertsStripeInvoice(ctx, app, person, intent, invoiceID, client)
	}
	return true, err
}

func alertsStripeEventReferences(event stripe.Event) (intent, sessionID, invoiceID string, handled bool, err error) {
	if event.Data == nil {
		return "", "", "", false, nil
	}
	switch event.Type {
	case "checkout.session.completed", "checkout.session.expired":
		var s stripe.CheckoutSession
		if err = json.Unmarshal(event.Data.Raw, &s); err != nil {
			return "", "", "", true, err
		}
		if s.Metadata["product"] != "alerts.repository-slots" {
			return "", "", "", false, nil
		}
		// Payment activation happens on invoice.paid. Completion may still require
		// payment authentication, so it only establishes the subscription binding.
		return s.Metadata["checkout_intent"], s.ID, "", true, nil
	case "invoice.paid":
		var i stripe.Invoice
		if err = json.Unmarshal(event.Data.Raw, &i); err != nil {
			return "", "", "", true, err
		}
		if i.Parent == nil || i.Parent.SubscriptionDetails == nil || i.Parent.SubscriptionDetails.Metadata["product"] != "alerts.repository-slots" {
			return "", "", "", false, nil
		}
		return i.Parent.SubscriptionDetails.Metadata["checkout_intent"], "", i.ID, true, nil
	default:
		return "", "", "", false, nil
	}
}
