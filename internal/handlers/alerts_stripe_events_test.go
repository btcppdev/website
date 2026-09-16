package handlers

import (
	"encoding/json"
	"io"
	"log"
	"net/http/httptest"
	"strings"
	"testing"

	"btcpp-web/internal/config"
	"btcpp-web/internal/types"
	"github.com/stripe/stripe-go/v86"
)

func TestAlertsStripeEventRouting(t *testing.T) {
	cases := []struct {
		name             string
		kind             stripe.EventType
		raw              string
		handled          bool
		session, invoice string
	}{
		{"completion only binds", "checkout.session.completed", `{"id":"cs_test","metadata":{"product":"alerts.repository-slots","checkout_intent":"intent"},"invoice":"in_not_proof"}`, true, "cs_test", ""},
		{"expiry requires provider lookup", "checkout.session.expired", `{"id":"cs_test","metadata":{"product":"alerts.repository-slots","checkout_intent":"intent"}}`, true, "cs_test", ""},
		{"subscription change requires lookup", "customer.subscription.updated", `{"id":"sub_test","metadata":{"product":"alerts.repository-slots","checkout_intent":"intent"}}`, true, "", ""},
		{"invoice verifies payment", "invoice.paid", `{"id":"in_test","parent":{"subscription_details":{"metadata":{"product":"alerts.repository-slots","checkout_intent":"intent"}}}}`, true, "", "in_test"},
		{"failure checks current subscription", "invoice.payment_failed", `{"id":"in_test","parent":{"subscription_details":{"metadata":{"product":"alerts.repository-slots","checkout_intent":"intent"}}}}`, true, "", "in_test"},
		{"merch checkout untouched", "checkout.session.completed", `{"id":"cs_merch","metadata":{"product":"merch"}}`, false, "", ""},
		{"other invoice untouched", "invoice.paid", `{"id":"in_other"}`, false, "", ""},
		{"failed invoice never grants", "invoice.payment_failed", `{"id":"in_failed"}`, false, "", ""},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			e := stripe.Event{Type: tt.kind, Data: &stripe.EventData{Raw: json.RawMessage(tt.raw)}}
			intent, session, invoice, handled, err := alertsStripeEventReferences(e)
			if err != nil || handled != tt.handled || session != tt.session || invoice != tt.invoice || (handled && intent != "intent") {
				t.Fatal(intent, session, invoice, handled, err)
			}
		})
	}
}

func TestUnsignedAlertsStripeInvoiceCannotReachDatabase(t *testing.T) {
	app := &config.AppContext{Env: &types.EnvConfig{StripeEndpointSec: "whsec_local_test"}, Err: log.New(io.Discard, "", 0)}
	req := httptest.NewRequest("POST", "/callback/stripe", strings.NewReader(`{"id":"evt_forged","type":"invoice.paid","data":{"object":{"id":"in_forged","parent":{"subscription_details":{"metadata":{"product":"alerts.repository-slots","checkout_intent":"intent"}}}}}}`))
	out := httptest.NewRecorder()
	StripeCallback(out, req, app)
	if out.Code != 400 {
		t.Fatal("unsigned invoice accepted", out.Code)
	}
}
