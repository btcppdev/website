package servicestripe

import (
	"btcpp-web/internal/servicebilling"
	"encoding/json"
	"github.com/stripe/stripe-go/v86"
	"testing"
	"time"
)

func invoiceFixture(t *testing.T) (*stripe.Invoice, *stripe.Price, Binding) {
	t.Helper()
	raw := `{"id":"in_test","status":"paid","currency":"usd","customer":"cus_test","billing_reason":"subscription_cycle","total":4800,"amount_paid":4800,"automatic_tax":{"enabled":true,"status":"complete"},"status_transitions":{"paid_at":1772323200},"parent":{"type":"subscription_details","subscription_details":{"subscription":"sub_test","metadata":{"product":"alerts.repository-slots","account_id":"account","checkout_intent":"intent"}}},"total_taxes":[{"amount":36,"tax_behavior":"inclusive"}],"lines":{"has_more":false,"data":[{"currency":"usd","amount":4800,"quantity":2,"period":{"start":1772323200,"end":1803859200},"parent":{"type":"subscription_item_details","subscription_item_details":{"subscription":"sub_test","proration":false}},"pricing":{"price_details":{"price":"price_test","product":"prod_test"}}}]}}`
	var inv stripe.Invoice
	if err := json.Unmarshal([]byte(raw), &inv); err != nil {
		t.Fatal(err)
	}
	price := &stripe.Price{ID: "price_test", Currency: "usd", UnitAmount: 2400, TaxBehavior: "inclusive", Type: "recurring", Recurring: &stripe.PriceRecurring{Interval: "year", IntervalCount: 1}, Product: &stripe.Product{ID: "prod_test", TaxCode: &stripe.TaxCode{ID: "txcd_10103000"}}}
	return &inv, price, Binding{IntentID: "intent", PersonID: "account", CustomerID: "cus_test", SubscriptionID: "sub_test", PriceID: "price_test", ProductID: "prod_test", Quantity: 2, ProductCode: servicebilling.AlertsProduct, TaxCode: servicebilling.AlertsStripeTaxCode}
}

func insiderInvoice(t *testing.T, code string) (*stripe.Invoice, *stripe.Price, Binding) {
	inv, price, b := invoiceFixture(t)
	plan, err := servicebilling.LookupProductPlan(code)
	if err != nil {
		t.Fatal(err)
	}
	b.ProductCode = code
	b.TaxCode = "txcd_fixture_publication"
	b.Quantity = 1
	inv.Parent.SubscriptionDetails.Metadata["product"] = code
	inv.Lines.Data[0].Quantity = 1
	inv.Lines.Data[0].Amount = plan.UnitCents
	inv.TotalTaxes[0].TaxBehavior = "exclusive"
	inv.TotalTaxes[0].Amount = 210
	inv.Total = plan.UnitCents + 210
	inv.AmountPaid = inv.Total
	price.UnitAmount = plan.UnitCents
	price.TaxBehavior = "exclusive"
	price.Product.TaxCode.ID = b.TaxCode
	price.Recurring.Interval = "month"
	start := time.Unix(inv.Lines.Data[0].Period.Start, 0).UTC()
	inv.Lines.Data[0].Period.End = start.AddDate(0, 1, 0).Unix()
	if plan.Months == 12 {
		price.Recurring.Interval = "year"
		start := time.Unix(inv.Lines.Data[0].Period.Start, 0).UTC()
		inv.Lines.Data[0].Period.End = start.AddDate(1, 0, 0).Unix()
	}
	return inv, price, b
}
func TestVerifyProductInvoices(t *testing.T) {
	for _, code := range []string{servicebilling.InsiderMonthlyProduct, servicebilling.InsiderAnnualProduct} {
		t.Run(code, func(t *testing.T) {
			inv, price, b := insiderInvoice(t, code)
			got, err := VerifyPaidInvoice(inv, price, b)
			if err != nil || got.Quantity != 1 || got.TotalCents != inv.Total || got.TaxCents != 210 {
				t.Fatal(got, err)
			}
			inv.TotalTaxes = nil
			inv.Total = price.UnitAmount
			inv.AmountPaid = inv.Total
			if got, err = VerifyPaidInvoice(inv, price, b); err != nil || got.TaxCents != 0 {
				t.Fatal("zero-tax invoice", got, err)
			}
		})
	}
}
func TestRejectCrossProductAndAlteredInvoices(t *testing.T) {
	cases := map[string]func(*stripe.Invoice, *stripe.Price, *Binding){
		"Alerts metadata": func(i *stripe.Invoice, p *stripe.Price, b *Binding) {
			i.Parent.SubscriptionDetails.Metadata["product"] = servicebilling.AlertsProduct
		},
		"annual metadata on monthly plan": func(i *stripe.Invoice, p *stripe.Price, b *Binding) {
			i.Parent.SubscriptionDetails.Metadata["product"] = servicebilling.InsiderAnnualProduct
		},
		"multiple memberships":         func(i *stripe.Invoice, p *stripe.Price, b *Binding) { b.Quantity = 2; i.Lines.Data[0].Quantity = 2 },
		"unknown product":              func(i *stripe.Invoice, p *stripe.Price, b *Binding) { b.ProductCode = "unknown" },
		"missing configured tax code":  func(i *stripe.Invoice, p *stripe.Price, b *Binding) { b.TaxCode = "" },
		"wrong tax classification":     func(i *stripe.Invoice, p *stripe.Price, b *Binding) { p.Product.TaxCode.ID = "txcd_wrong" },
		"inclusive tax":                func(i *stripe.Invoice, p *stripe.Price, b *Binding) { i.TotalTaxes[0].TaxBehavior = "inclusive" },
		"extra charge":                 func(i *stripe.Invoice, p *stripe.Price, b *Binding) { i.Total++; i.AmountPaid++ },
		"wrong interval":               func(i *stripe.Invoice, p *stripe.Price, b *Binding) { p.Recurring.Interval = "year" },
		"year access on monthly price": func(i *stripe.Invoice, p *stripe.Price, b *Binding) { i.Lines.Data[0].Period.End += 330 * 86400 },
		"overflowing tax":              func(i *stripe.Invoice, p *stripe.Price, b *Binding) { i.TotalTaxes[0].Amount = 9223372036854775807 },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			inv, price, b := insiderInvoice(t, servicebilling.InsiderMonthlyProduct)
			mutate(inv, price, &b)
			if _, err := VerifyPaidInvoice(inv, price, b); err == nil {
				t.Fatal("accepted altered invoice")
			}
		})
	}
}

func TestInvoiceProductTaxCodeShapes(t *testing.T) {
	for _, tc := range []struct {
		name, legacy, current string
		valid                 bool
	}{
		{"legacy", "txcd_10103000", "", true},
		{"current", "", "txcd_10103000", true},
		{"both", "txcd_10103000", "txcd_10103000", true},
		{"missing", "", "", false},
		{"wrong current", "", "txcd_wrong", false},
		{"conflicting current", "txcd_10103000", "txcd_wrong", false},
		{"conflicting legacy", "txcd_wrong", "txcd_10103000", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			inv, price, b := invoiceFixture(t)
			raw := `{"id":"prod_test"`
			if tc.legacy != "" {
				raw += `,"tax_code":"` + tc.legacy + `"`
			}
			if tc.current != "" {
				raw += `,"tax_details":{"tax_code":"` + tc.current + `"}`
			}
			raw += `}`
			price.Product = &stripe.Product{}
			if err := json.Unmarshal([]byte(raw), price.Product); err != nil {
				t.Fatal(err)
			}
			_, err := VerifyPaidInvoice(inv, price, b)
			if (err == nil) != tc.valid {
				t.Fatalf("valid=%v error=%v", tc.valid, err)
			}
		})
	}
}
