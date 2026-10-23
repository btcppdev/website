package alertsstripe

import (
	"context"
	"errors"
	"github.com/stripe/stripe-go/v86"
	"reflect"
	"testing"
	"time"
)

func testIntent() Intent {
	return Intent{ID: "b5a5dd86-22b0-49d7-970d-ecc58f99b46e", PersonID: "c852ac28-a479-414e-8f4c-e007c6764e51", Email: "buyer@example.test", PaidSlots: 2, CreatedAt: time.Now().UTC().Truncate(time.Second), ReturnBaseURL: "https://btcpp.dev"}
}
func TestCheckoutTermsAndStableRetry(t *testing.T) {
	in := testIntent()
	ctx := context.Background()
	first, err := checkoutParams(ctx, in, in.CreatedAt)
	if err != nil {
		t.Fatal(err)
	}
	second, err := checkoutParams(ctx, in, in.CreatedAt.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatal("retry changed provider request")
	}
	item := first.LineItems[0]
	if *first.Mode != "subscription" || *item.Quantity != 2 || *item.PriceData.UnitAmount != 2400 || *item.PriceData.TaxBehavior != "inclusive" || *item.PriceData.Recurring.Interval != "year" || *item.PriceData.ProductData.TaxCode != "txcd_10103000" || !*first.AutomaticTax.Enabled || *first.AllowPromotionCodes || *first.BillingAddressCollection != "required" {
		t.Fatal("wrong recurring tax-inclusive checkout terms")
	}
	if _, err = checkoutParams(ctx, in, in.CreatedAt.Add(90*time.Minute)); !errors.Is(err, ErrReconcile) {
		t.Fatal("old attempt can create again", err)
	}
	for _, base := range []string{"https://evil.test@btcpp.dev", "https://btcpp.dev/elsewhere", "https://btcpp.dev?next=evil", "http://btcpp.dev"} {
		bad := in
		bad.ReturnBaseURL = base
		if _, err := checkoutParams(ctx, bad, in.CreatedAt); err == nil {
			t.Fatal("accepted", base)
		}
	}
}
func TestCheckoutLookupAndResponseValidation(t *testing.T) {
	in := testIntent()
	calls := 0
	good := func() *stripe.CheckoutSession {
		return &stripe.CheckoutSession{ID: "cs_test_safe", Mode: "subscription", Status: "open", ExpiresAt: in.CreatedAt.Add(time.Hour).Unix(), ClientReferenceID: in.ID, Metadata: map[string]string{"account_id": in.PersonID, "checkout_intent": in.ID, "product": "alerts.repository-slots"}, URL: "https://checkout.stripe.com/c/pay/cs_test_safe#test"}
	}
	c := Client{Create: func(*stripe.CheckoutSessionParams) (*stripe.CheckoutSession, error) { calls++; return good(), nil }, Retrieve: func(string, *stripe.CheckoutSessionParams) (*stripe.CheckoutSession, error) { return good(), nil }}
	id, link, err := c.Open(context.Background(), in, "", in.CreatedAt)
	if err != nil || id != "cs_test_safe" || link == "" {
		t.Fatal(id, link, err)
	}
	if _, _, err = c.Open(context.Background(), in, id, in.CreatedAt); err != nil || calls != 1 {
		t.Fatal("retrieval created duplicate", calls, err)
	}
	for _, mutate := range []func(*stripe.CheckoutSession){func(s *stripe.CheckoutSession) { s.Livemode = true }, func(s *stripe.CheckoutSession) { s.Metadata["account_id"] = "another" }, func(s *stripe.CheckoutSession) { s.URL = "https://checkout.stripe.com.evil.test/c/pay/cs_test_safe" }, func(s *stripe.CheckoutSession) { s.URL = "https://checkout.stripe.com/c/pay/cs_test_safeevil" }, func(s *stripe.CheckoutSession) { s.Status = "complete" }} {
		c.Retrieve = func(string, *stripe.CheckoutSessionParams) (*stripe.CheckoutSession, error) {
			s := good()
			mutate(s)
			return s, nil
		}
		if _, link, err = c.Open(context.Background(), in, id, in.CreatedAt); err == nil || link != "" {
			t.Fatal("invalid checkout redirected", link, err)
		}
	}
}
