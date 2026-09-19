package handlers

import (
	"btcpp-web/external/getters"
	"btcpp-web/internal/alertsstripe"
	"btcpp-web/internal/config"
	"btcpp-web/internal/servicebilling"
	"btcpp-web/internal/servicestripe"
	"context"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stripe/stripe-go/v86"
)

type serviceStripeReference struct{ Product, Intent, Session, Invoice string }

// Only called after StripeCallback verifies the event signature. The webhook
// locates saved state; authenticated provider lookups establish payment facts.
func handleServiceStripeEvent(ctx context.Context, app *config.AppContext, event stripe.Event) (bool, error) {
	client := alertsstripe.NewClient(event.Livemode)
	return processServiceStripeEvent(ctx, app, event, servicestripe.CheckoutClient{Retrieve: client.Retrieve, Live: client.Live}, servicestripe.InvoiceClient{Invoice: client.Invoice, Price: client.Price, Live: client.Live})
}

func serviceStripeEventReference(event stripe.Event) (ref serviceStripeReference, handled bool, err error) {
	if event.Data == nil {
		return
	}
	switch event.Type {
	case "checkout.session.completed", "checkout.session.expired":
		var s stripe.CheckoutSession
		if err = json.Unmarshal(event.Data.Raw, &s); err != nil {
			return ref, true, err
		}
		if s.Mode != "subscription" {
			return ref, false, nil
		}
		ref = serviceStripeReference{Product: s.Metadata["product"], Intent: s.Metadata["checkout_intent"], Session: s.ID}
	case "invoice.paid":
		var i stripe.Invoice
		if err = json.Unmarshal(event.Data.Raw, &i); err != nil {
			return ref, true, err
		}
		if i.Parent == nil || i.Parent.SubscriptionDetails == nil {
			return ref, false, nil
		}
		meta := i.Parent.SubscriptionDetails.Metadata
		ref = serviceStripeReference{Product: meta["product"], Intent: meta["checkout_intent"], Invoice: i.ID}
	default:
		return ref, false, nil
	}
	plan, planErr := servicebilling.LookupProductPlan(ref.Product)
	if planErr != nil || plan.Family != "insider" {
		return serviceStripeReference{}, false, nil
	}
	return ref, true, nil
}

func processServiceStripeEvent(ctx context.Context, app *config.AppContext, event stripe.Event, checkout servicestripe.CheckoutClient, invoices servicestripe.InvoiceClient) (bool, error) {
	ref, handled, err := serviceStripeEventReference(event)
	if !handled || err != nil {
		return handled, err
	}
	if _, err = uuid.Parse(ref.Intent); err != nil {
		return true, servicestripe.ErrCheckout
	}
	if checkout.Live != event.Livemode || invoices.Live != event.Livemode {
		return true, servicestripe.ErrCheckout
	}
	var person string
	err = app.DB.QueryRow(ctx, `SELECT coalesce(person_id::text,'') FROM service_stripe_checkouts WHERE id=$1 AND product_code=$2`, ref.Intent, ref.Product).Scan(&person)
	if errors.Is(err, pgx.ErrNoRows) {
		return true, nil
	}
	if err != nil {
		return true, err
	}
	// Deleted accounts retain financial evidence but cannot regain access.
	if person == "" {
		return true, nil
	}
	if event.Type == "checkout.session.expired" {
		return true, getters.CloseExpiredServiceStripeCheckout(ctx, app, person, ref.Intent, ref.Product, ref.Session, checkout)
	}
	if ref.Session != "" {
		_, err = getters.ResolveServiceStripeSubscription(ctx, app, person, ref.Intent, ref.Product, checkout, ref.Session)
		return true, err
	}
	var bound bool
	if err = app.DB.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM service_stripe_subscriptions WHERE checkout_id=$1)`, ref.Intent).Scan(&bound); err != nil {
		return true, err
	}
	if !bound {
		if _, err = getters.ResolveServiceStripeSubscription(ctx, app, person, ref.Intent, ref.Product, checkout); err != nil {
			return true, err
		}
	}
	_, err = getters.ReconcileServiceStripeInvoice(ctx, app, person, ref.Intent, ref.Product, ref.Invoice, invoices)
	return true, err
}
