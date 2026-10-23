package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/stripe/stripe-go/v86"
)

func TestLegacyStripeInvoiceEventRoutesFromCurrentProviderInvoice(t *testing.T) {
	for _, product := range []string{"alerts.repository-slots", "insider.membership.annual"} {
		for _, kind := range []stripe.EventType{"invoice.paid", "invoice.payment_failed"} {
			event := stripe.Event{Type: kind, Livemode: true, Data: &stripe.EventData{Raw: json.RawMessage(`{"id":"in_legacy","subscription":"sub_legacy","subscription_details":{"metadata":{"product":"untrusted-old-value"}}}`)}}
			normalized, err := normalizeStripeInvoiceEvent(context.Background(), event, func(id string, p *stripe.InvoiceParams) (*stripe.Invoice, error) {
				if id != "in_legacy" || p.Context == nil {
					t.Fatal("missing authenticated lookup context")
				}
				return &stripe.Invoice{ID: id, Livemode: true, Parent: &stripe.InvoiceParent{SubscriptionDetails: &stripe.InvoiceParentSubscriptionDetails{Metadata: map[string]string{"product": product, "checkout_intent": "intent"}}}}, nil
			})
			if err != nil {
				t.Fatal(err)
			}
			if product == "alerts.repository-slots" {
				intent, _, invoice, handled, err := alertsStripeEventReferences(normalized)
				if err != nil || !handled || intent != "intent" || invoice != "in_legacy" {
					t.Fatal(intent, invoice, handled, err)
				}
			} else {
				ref, handled, err := serviceStripeEventReference(normalized)
				if err != nil || !handled || ref.Intent != "intent" || ref.Invoice != "in_legacy" {
					t.Fatal(ref, handled, err)
				}
			}
		}
	}
}
func TestStripeInvoiceNormalizationRejectsFailedOrMismatchedLookup(t *testing.T) {
	event := stripe.Event{Type: "invoice.paid", Livemode: true, Data: &stripe.EventData{Raw: json.RawMessage(`{"id":"in_legacy"}`)}}
	for _, result := range []*stripe.Invoice{nil, {ID: "in_other", Livemode: true}, {ID: "in_legacy", Livemode: false}} {
		if _, err := normalizeStripeInvoiceEvent(context.Background(), event, func(string, *stripe.InvoiceParams) (*stripe.Invoice, error) { return result, nil }); err == nil {
			t.Fatal("lookup mismatch accepted")
		}
	}
	if _, err := normalizeStripeInvoiceEvent(context.Background(), event, func(string, *stripe.InvoiceParams) (*stripe.Invoice, error) { return nil, errors.New("timeout") }); err == nil {
		t.Fatal("failed lookup acknowledged")
	}
}
func TestCurrentInvoiceAndExistingCheckoutEventsDoNotNeedNormalization(t *testing.T) {
	for _, event := range []stripe.Event{
		{Type: "checkout.session.completed", Data: &stripe.EventData{Raw: json.RawMessage(`{"id":"cs_existing"}`)}},
		{Type: "invoice.paid", Data: &stripe.EventData{Raw: json.RawMessage(`{"id":"in_current","parent":{"subscription_details":{}}}`)}},
	} {
		if _, err := normalizeStripeInvoiceEvent(context.Background(), event, func(string, *stripe.InvoiceParams) (*stripe.Invoice, error) {
			t.Fatal("unexpected provider call")
			return nil, nil
		}); err != nil {
			t.Fatal(err)
		}
	}
}
