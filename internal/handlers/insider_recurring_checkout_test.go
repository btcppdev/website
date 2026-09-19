package handlers

import (
	"btcpp-web/internal/config"
	"btcpp-web/internal/types"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestInsiderRecurringTerms(t *testing.T) {
	now := time.Now().UTC()
	request := insiderCheckoutRequest{BillingMode: "recurring", RequestID: "recurring-checkout-request", PersonID: "00000000-0000-0000-0000-000000000001", ProductCode: "insider.membership.monthly", PaymentProvider: "stripe", AmountCents: 2100, Currency: "USD", QuoteExpiresAt: now.Add(10 * time.Minute), SuccessURL: "https://insider.btcpp.dev/account/success", CancelURL: "https://insider.btcpp.dev/account/cancel"}
	terms, err := insiderRecurringTerms(request, "https://insider.btcpp.dev", "txcd_test", now)
	if err != nil || terms.Quantity != 1 || terms.TaxCode != "txcd_test" || terms.ReturnOrigin != "https://insider.btcpp.dev" {
		t.Fatal(terms, err)
	}
	annual := request
	annual.ProductCode = "insider.membership.annual"
	annual.AmountCents = 21000
	if _, err := insiderRecurringTerms(annual, "https://insider.btcpp.dev", "txcd_test", now); err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*insiderCheckoutRequest){
		func(r *insiderCheckoutRequest) { r.AmountCents = 1 },
		func(r *insiderCheckoutRequest) { r.PaymentProvider = "opennode" },
		func(r *insiderCheckoutRequest) { r.ProductCode = "alerts.repository-slots" },
		func(r *insiderCheckoutRequest) { r.ContributionPublicationID = "campaign" },
		func(r *insiderCheckoutRequest) { r.PublicCredit = json.RawMessage(`null`) },
		func(r *insiderCheckoutRequest) { r.SuccessURL = "https://other.example/account" },
		func(r *insiderCheckoutRequest) { r.CancelURL = "https://beta.btcpp.dev/account" },
		func(r *insiderCheckoutRequest) { r.QuoteExpiresAt = now },
		func(r *insiderCheckoutRequest) { r.BillingMode = "fixed_term" },
	} {
		changed := request
		change(&changed)
		if _, err := insiderRecurringTerms(changed, "https://insider.btcpp.dev,https://beta.btcpp.dev", "txcd_test", now); err == nil {
			t.Fatalf("unsafe recurring request accepted: %+v", changed)
		}
	}
	if _, err := insiderRecurringTerms(request, "https://insider.btcpp.dev", "", now); err == nil {
		t.Fatal("missing server tax classification accepted")
	}
}

func TestInsiderRecurringCheckoutDisabledWithoutConfiguration(t *testing.T) {
	app := &config.AppContext{Env: &types.EnvConfig{InsiderServiceToken: "private-test"}}
	body := `{"billing_mode":"recurring","request_id":"recurring-checkout-request"}`
	req := httptest.NewRequest("POST", "/api/internal/insider/checkouts", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer private-test")
	req.Header.Set("Idempotency-Key", "recurring-checkout-request")
	out := httptest.NewRecorder()
	CreateInsiderCheckout(out, req, app)
	if out.Code != 503 {
		t.Fatal(out.Code, out.Body.String())
	}
}
