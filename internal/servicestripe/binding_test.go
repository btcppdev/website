package servicestripe

import (
	"btcpp-web/internal/servicebilling"
	"context"
	"github.com/stripe/stripe-go/v86"
	"testing"
)

func TestCompletedProductBinding(t *testing.T) {
	for _, code := range []string{servicebilling.InsiderMonthlyProduct, servicebilling.InsiderAnnualProduct} {
		for _, change := range []string{"valid", "current-tax-code", "product", "account", "quantity", "tax", "interval", "price", "incomplete", "mode", "pagination"} {
			t.Run(code+"/"+change, func(t *testing.T) {
				in := insiderIntent(code)
				_, price, _ := insiderInvoice(t, code)
				s := &stripe.CheckoutSession{ID: "cs_test", Mode: "subscription", Status: "complete", ClientReferenceID: in.ID, Metadata: map[string]string{"product": code, "account_id": in.PersonID, "checkout_intent": in.ID}, Customer: &stripe.Customer{ID: "cus_test"}, Subscription: &stripe.Subscription{ID: "sub_test"}, LineItems: &stripe.LineItemList{Data: []*stripe.LineItem{{Quantity: 1, Price: price}}}}
				switch change {
				case "current-tax-code":
					price.Product.TaxDetails = &stripe.ProductTaxDetails{TaxCode: price.Product.TaxCode.ID}
					price.Product.TaxCode = nil
				case "product":
					s.Metadata["product"] = servicebilling.AlertsProduct
				case "account":
					s.Metadata["account_id"] = "someone-else"
				case "quantity":
					s.LineItems.Data[0].Quantity = 2
				case "tax":
					price.TaxBehavior = "inclusive"
				case "interval":
					price.Recurring.IntervalCount = 2
				case "price":
					price.UnitAmount++
				case "incomplete":
					s.Status = "open"
				case "mode":
					s.Livemode = true
				case "pagination":
					s.LineItems.HasMore = true
				}
				c := CheckoutClient{Retrieve: func(id string, p *stripe.CheckoutSessionParams) (*stripe.CheckoutSession, error) {
					if id != "cs_test" || len(p.Expand) != 1 || *p.Expand[0] != "line_items.data.price.product" {
						t.Fatal("missing authenticated lookup/expansion")
					}
					return s, nil
				}}
				b, err := c.CompletedBinding(context.Background(), in, "cs_test")
				if change != "valid" && change != "current-tax-code" {
					if err == nil {
						t.Fatal("mismatched binding accepted")
					}
					return
				}
				if err != nil || b.ProductCode != code || b.TaxCode != in.TaxCode || b.Quantity != 1 || b.IntentID != in.ID {
					t.Fatal(b, err)
				}
			})
		}
	}
}
