package handlers

import (
	"btcpp-web/internal/config"
	"btcpp-web/internal/types"
	"context"
	"github.com/stripe/stripe-go/v86"
	"io"
	"log"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestUnsignedRefundCannotReachDatabase(t *testing.T) {
	app := &config.AppContext{Env: &types.EnvConfig{StripeEndpointSec: "whsec_test"}, Err: log.New(io.Discard, "", 0)}
	for _, kind := range []string{"refund.created", "refund.updated", "charge.refunded"} {
		w := httptest.NewRecorder()
		StripeCallback(w, httptest.NewRequest("POST", "/callback/stripe", strings.NewReader(`{"id":"evt_fake","type":"`+kind+`","data":{"object":{"id":"re_fake"}}}`)), app)
		if w.Code != 400 {
			t.Fatal(kind, w.Code)
		}
	}
}
func TestRefundEventModeGuard(t *testing.T) {
	app := &config.AppContext{Env: &types.EnvConfig{StripeKey: "sk_live_test"}}
	handled, err := handleServiceStripeRefundEvent(context.Background(), app, stripe.Event{Type: "refund.created", Livemode: false, Data: &stripe.EventData{}})
	if !handled || err == nil {
		t.Fatal("test event reached live ledger", handled, err)
	}
	handled, err = handleServiceStripeRefundEvent(context.Background(), nil, stripe.Event{Type: "invoice.paid"})
	if handled || err != nil {
		t.Fatal("invoice intercepted")
	}
}
