package alertsstripe

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stripe/stripe-go/v86"
)

func TestIncreasePaymentLink(t *testing.T) {
	for _, scenario := range []string{"valid", "foreign host", "userinfo", "http", "fragment", "empty path", "paid", "void", "partial", "remaining", "owner", "wrong invoice", "changed quote", "wrong price", "transport"} {
		t.Run(scenario, func(t *testing.T) {
			c, b, _, _, invoice, now, _ := increaseFixture(t)
			q, err := c.PreviewIncrease(context.Background(), b, 3, now)
			if err != nil {
				t.Fatal(err)
			}
			intent := IncreaseIntent{ID: uuid.NewString(), Preview: *q}
			invoice.ID = "in_upgrade"
			invoice.Status = "open"
			invoice.AmountRemaining = invoice.Total
			invoice.StatusTransitions = nil
			invoice.HostedInvoiceURL = "https://invoice.stripe.com/i/acct_test/token?s=em"
			id := invoice.ID
			c.Invoice = func(request string, p *stripe.InvoiceParams) (*stripe.Invoice, error) {
				if request != id {
					t.Fatal(request)
				}
				if _, ok := p.Context.Deadline(); !ok {
					t.Fatal("unbounded lookup")
				}
				return invoice, nil
			}
			switch scenario {
			case "foreign host":
				invoice.HostedInvoiceURL = "https://invoice.stripe.com.evil.test/i/token"
			case "userinfo":
				invoice.HostedInvoiceURL = "https://evil@invoice.stripe.com/i/token"
			case "http":
				invoice.HostedInvoiceURL = "http://invoice.stripe.com/i/token"
			case "fragment":
				invoice.HostedInvoiceURL += "#x"
			case "empty path":
				invoice.HostedInvoiceURL = "https://invoice.stripe.com/i/"
			case "paid":
				invoice.Status = "paid"
			case "void":
				invoice.Status = "void"
			case "partial":
				invoice.AmountPaid = 1
				invoice.AmountRemaining--
			case "remaining":
				invoice.AmountRemaining--
			case "owner":
				invoice.Parent.SubscriptionDetails.Metadata["account_id"] = "other"
			case "wrong invoice":
				invoice.ID = "in_other"
			case "changed quote":
				intent.Preview.TotalCents++
			case "wrong price":
				c.Price = func(string, *stripe.PriceParams) (*stripe.Price, error) { return &stripe.Price{ID: "price_other"}, nil }
			case "transport":
				c.Invoice = func(string, *stripe.InvoiceParams) (*stripe.Invoice, error) { return nil, context.DeadlineExceeded }
			}
			link, err := c.IncreasePaymentLink(context.Background(), id, b, intent)
			if scenario == "valid" {
				if err != nil || link != invoice.HostedInvoiceURL {
					t.Fatal(link, err)
				}
			} else if err == nil || link != "" {
				t.Fatal("unsafe link", link, err)
			}
		})
	}
}
