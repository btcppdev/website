package servicestripe

import (
	"btcpp-web/internal/servicebilling"
	"context"
	"github.com/stripe/stripe-go/v86"
	"reflect"
	"testing"
	"time"
)

func insiderIntent(code string) CheckoutIntent {
	return CheckoutIntent{ID: "b5a5dd86-22b0-49d7-970d-ecc58f99b46e", PersonID: "c852ac28-a479-414e-8f4c-e007c6764e51", Email: "reader@example.test", ProductCode: code, TaxCode: "txcd_fixture_publication", Quantity: 1, CreatedAt: time.Now().UTC().Truncate(time.Second), ReturnOrigin: "https://insider.btcpp.dev", SuccessURL: "https://insider.btcpp.dev/account/billing?result=returned", CancelURL: "https://insider.btcpp.dev/account/billing?result=cancelled"}
}
func TestInsiderRecurringCheckoutTermsAndRetries(t *testing.T) {
	for _, code := range []string{servicebilling.InsiderMonthlyProduct, servicebilling.InsiderAnnualProduct} {
		t.Run(code, func(t *testing.T) {
			in := insiderIntent(code)
			plan, _ := servicebilling.LookupProductPlan(code)
			first, err := CheckoutParams(context.Background(), in, in.CreatedAt)
			if err != nil {
				t.Fatal(err)
			}
			second, err := CheckoutParams(context.Background(), in, in.CreatedAt.Add(time.Hour))
			if err != nil || !reflect.DeepEqual(first, second) {
				t.Fatal("retry changed checkout", err)
			}
			interval := "month"
			if plan.Months == 12 {
				interval = "year"
			}
			item := first.LineItems[0]
			if *first.Mode != "subscription" || *item.Quantity != 1 || *item.PriceData.UnitAmount != plan.UnitCents || *item.PriceData.TaxBehavior != "exclusive" || *item.PriceData.Recurring.Interval != interval || first.Metadata["product"] != code || first.SubscriptionData.Metadata["product"] != code || *first.SuccessURL != in.SuccessURL {
				t.Fatal("incorrect Insider subscription terms")
			}
		})
	}
}
func TestCheckoutRejectsUntrustedPlanOrReturn(t *testing.T) {
	for _, mutate := range []func(*CheckoutIntent){
		func(i *CheckoutIntent) { i.Quantity = 2 }, func(i *CheckoutIntent) { i.ProductCode = "unknown" }, func(i *CheckoutIntent) { i.TaxCode = "" },
		func(i *CheckoutIntent) { i.SuccessURL = "https://evil.test/account" }, func(i *CheckoutIntent) { i.CancelURL = "https://insider.btcpp.dev.evil.test/account" }, func(i *CheckoutIntent) { i.SuccessURL = "https://user@insider.btcpp.dev/account" },
	} {
		in := insiderIntent(servicebilling.InsiderMonthlyProduct)
		mutate(&in)
		if _, err := CheckoutParams(context.Background(), in, in.CreatedAt); err == nil {
			t.Fatal("invalid checkout accepted")
		}
		c := CheckoutClient{Retrieve: func(string, *stripe.CheckoutSessionParams) (*stripe.CheckoutSession, error) {
			t.Fatal("invalid saved intent reached provider")
			return nil, nil
		}}
		if _, _, err := c.Open(context.Background(), in, "cs_saved", in.CreatedAt); err == nil {
			t.Fatal("invalid retrieval accepted")
		}
	}
}
func TestCheckoutRejectsOtherProductSession(t *testing.T) {
	in := insiderIntent(servicebilling.InsiderMonthlyProduct)
	c := CheckoutClient{Create: func(*stripe.CheckoutSessionParams) (*stripe.CheckoutSession, error) {
		return &stripe.CheckoutSession{ID: "cs_test", Mode: "subscription", Status: "open", ExpiresAt: in.CreatedAt.Add(time.Hour).Unix(), ClientReferenceID: in.ID, Metadata: map[string]string{"account_id": in.PersonID, "checkout_intent": in.ID, "product": servicebilling.AlertsProduct}, URL: "https://checkout.stripe.com/c/pay/cs_test"}, nil
	}}
	if _, _, err := c.Open(context.Background(), in, "", in.CreatedAt); err == nil {
		t.Fatal("Alerts session accepted for Insider")
	}
}
