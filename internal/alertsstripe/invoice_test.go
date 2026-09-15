package alertsstripe

import (
	"context"
	"encoding/json"
	"github.com/stripe/stripe-go/v86"
	"testing"
)

func invoiceFixture(t *testing.T) (*stripe.Invoice, *stripe.Price, Binding) {
	t.Helper()
	raw := `{"id":"in_test","status":"paid","currency":"usd","customer":"cus_test","billing_reason":"subscription_cycle","total":400,"amount_paid":400,"automatic_tax":{"enabled":true,"status":"complete"},"status_transitions":{"paid_at":1772323200},"parent":{"type":"subscription_details","subscription_details":{"subscription":"sub_test","metadata":{"product":"alerts.repository-slots","account_id":"account","checkout_intent":"intent"}}},"total_taxes":[{"amount":36,"tax_behavior":"inclusive"}],"lines":{"has_more":false,"data":[{"currency":"usd","amount":400,"quantity":2,"period":{"start":1772323200,"end":1775001600},"parent":{"type":"subscription_item_details","subscription_item_details":{"subscription":"sub_test","proration":false}},"pricing":{"price_details":{"price":"price_test","product":"prod_test"}}}]}}`
	var inv stripe.Invoice
	if err := json.Unmarshal([]byte(raw), &inv); err != nil {
		t.Fatal(err)
	}
	price := &stripe.Price{ID: "price_test", Currency: "usd", UnitAmount: 200, TaxBehavior: "inclusive", Type: "recurring", Recurring: &stripe.PriceRecurring{Interval: "month", IntervalCount: 1}, Product: &stripe.Product{ID: "prod_test", TaxCode: &stripe.TaxCode{ID: "txcd_10103000"}}}
	return &inv, price, Binding{IntentID: "intent", PersonID: "account", CustomerID: "cus_test", SubscriptionID: "sub_test", PriceID: "price_test", ProductID: "prod_test", PaidSlots: 2}
}
func TestPaidMonthlyInvoice(t *testing.T) {
	inv, price, b := invoiceFixture(t)
	period, err := VerifyMonthlyInvoice(inv, price, b)
	if err != nil || period.Quantity != 2 || period.TotalCents != 400 || period.TaxCents != 36 || period.Start.Unix() != inv.Lines.Data[0].Period.Start || period.End.Unix() != inv.Lines.Data[0].Period.End {
		t.Fatal(period, err)
	}
}
func TestRejectUnverifiedOrChangedInvoice(t *testing.T) {
	cases := map[string]func(*stripe.Invoice, *stripe.Price){
		"unpaid": func(i *stripe.Invoice, p *stripe.Price) { i.Status = "open" },
		"wrong account": func(i *stripe.Invoice, p *stripe.Price) {
			i.Parent.SubscriptionDetails.Metadata["account_id"] = "other"
		},
		"wrong customer":     func(i *stripe.Invoice, p *stripe.Price) { i.Customer.ID = "cus_other" },
		"wrong subscription": func(i *stripe.Invoice, p *stripe.Price) { i.Parent.SubscriptionDetails.Subscription.ID = "sub_other" },
		"wrong mode":         func(i *stripe.Invoice, p *stripe.Price) { i.Livemode = true },
		"wrong quantity":     func(i *stripe.Invoice, p *stripe.Price) { i.Lines.Data[0].Quantity = 3 },
		"exclusive tax":      func(i *stripe.Invoice, p *stripe.Price) { p.TaxBehavior = "exclusive" },
		"underpaid":          func(i *stripe.Invoice, p *stripe.Price) { i.AmountPaid = 200 },
		"off stripe":         func(i *stripe.Invoice, p *stripe.Price) { i.AmountPaidOffStripe = 400 },
		"price changed":      func(i *stripe.Invoice, p *stripe.Price) { p.UnitAmount = 100 },
		"proration": func(i *stripe.Invoice, p *stripe.Price) {
			i.Lines.Data[0].Parent.SubscriptionItemDetails.Proration = true
		},
		"update":           func(i *stripe.Invoice, p *stripe.Price) { i.BillingReason = "subscription_update" },
		"incomplete lines": func(i *stripe.Invoice, p *stripe.Price) { i.Lines.HasMore = true },
		"tax pending":      func(i *stripe.Invoice, p *stripe.Price) { i.AutomaticTax.Status = "failed" },
		"refund":           func(i *stripe.Invoice, p *stripe.Price) { i.PostPaymentCreditNotesAmount = 400 },
		"bad period":       func(i *stripe.Invoice, p *stripe.Price) { i.Lines.Data[0].Period.End = i.Lines.Data[0].Period.Start },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			inv, price, b := invoiceFixture(t)
			mutate(inv, price)
			if _, err := VerifyMonthlyInvoice(inv, price, b); err == nil {
				t.Fatal("accepted invalid invoice")
			}
		})
	}
}

func TestPaidInvoiceUsesAuthenticatedLookups(t *testing.T) {
	inv, price, b := invoiceFixture(t)
	calls := 0
	c := Client{Invoice: func(id string, p *stripe.InvoiceParams) (*stripe.Invoice, error) {
		calls++
		if id != inv.ID || p.Context == nil {
			t.Fatal("invalid invoice lookup")
		}
		return inv, nil
	}, Price: func(id string, p *stripe.PriceParams) (*stripe.Price, error) {
		calls++
		if id != b.PriceID || len(p.Expand) != 1 || *p.Expand[0] != "product" {
			t.Fatal("missing product verification")
		}
		return price, nil
	}}
	if _, err := c.PaidInvoice(context.Background(), inv.ID, b); err != nil || calls != 2 {
		t.Fatal(calls, err)
	}
	if _, err := c.PaidInvoice(context.Background(), "not-an-invoice", b); err == nil || calls != 2 {
		t.Fatal("invalid input reached provider")
	}
	price.Product.TaxCode.ID = "txcd_merch"
	if _, err := c.PaidInvoice(context.Background(), inv.ID, b); err == nil {
		t.Fatal("wrong tax classification accepted")
	}
}
