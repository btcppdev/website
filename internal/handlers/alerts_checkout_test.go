package handlers

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"btcpp-web/external/getters"
	"btcpp-web/internal/servicebilling"
	"btcpp-web/internal/types"
	"github.com/stripe/stripe-go/v86"
)

func TestPrepareAlertsCheckout(t *testing.T) {
	address := &types.ShopAddress{Line1: "101 Main St", City: "Austin", Country: "US"}
	for _, term := range []struct {
		provider string
		months   int
	}{{"stripe", 1}, {"opennode", 12}, {"opennode", 24}, {"opennode", 120}} {
		t.Run(fmt.Sprintf("%s-%d", term.provider, term.months), func(t *testing.T) {
			now := time.Now().UTC().Truncate(time.Second)
			calls := 0
			calculate := func(p *stripe.TaxCalculationParams) (*stripe.TaxCalculation, error) {
				calls++
				line := p.LineItems[0]
				return &stripe.TaxCalculation{ID: "taxcalc_plan", Currency: "usd", AmountTotal: *line.Amount, ExpiresAt: time.Now().Add(time.Hour).Unix(), LineItems: &stripe.TaxCalculationLineItemList{Data: []*stripe.TaxCalculationLineItem{{Amount: *line.Amount, Quantity: *line.Quantity, Reference: *line.Reference, TaxBehavior: stripe.TaxCalculationLineItemTaxBehavior(*line.TaxBehavior), TaxCode: *line.TaxCode}}}}, nil
			}
			state := &getters.AlertsBillingState{Revision: "initial-account-revision"}
			quote, _, err := prepareAlertsCheckout(context.Background(), state, term.provider, 2, term.months, now, address, calculate)
			if err != nil {
				t.Fatal(err)
			}
			if quote.TotalCents != int64(term.months)*400 || quote.Quantity != 2 || quote.AccountRevision != state.Revision || quote.BillingMonths != term.months || quote.BaseOrderID != "" {
				t.Fatalf("incorrect new term: %+v", quote)
			}
			state.Orders = []getters.AlertsBillingOrder{{OrderID: "base", Status: "paid", Quote: *quote, Granted: true}}
			state.Revision = "after-initial-payment"
			halfway := now.Add(quote.PeriodEnd.Sub(now) / 2)
			increase, _, err := prepareAlertsCheckout(context.Background(), state, term.provider, 3, term.months, halfway, address, calculate)
			if err != nil {
				t.Fatal(err)
			}
			if increase.TotalCents != int64(term.months)*100 || increase.Quantity != 1 || increase.BaseOrderID != "base" || increase.AccountRevision != state.Revision || !increase.PeriodEnd.Equal(*quote.PeriodEnd) {
				t.Fatalf("incorrect increase: %+v", increase)
			}
			state.Orders = append(state.Orders, getters.AlertsBillingOrder{OrderID: "upgrade", Status: "paid", Granted: true, Quote: *increase})
			state.Revision = "after-upgrade"
			next, _, err := prepareAlertsCheckout(context.Background(), state, term.provider, 4, term.months, halfway, address, calculate)
			if err != nil || next.Quantity != 1 || next.BaseOrderID != "base" || next.TotalCents != increase.TotalCents {
				t.Fatal("second upgrade double-charges prior slots", next, err)
			}
			calls = 0
			for _, target := range []int64{1, 2, 3} {
				if _, _, err := prepareAlertsCheckout(context.Background(), state, term.provider, target, term.months, halfway, address, calculate); !errors.Is(err, ErrAlertsRenewalChange) {
					t.Fatal("decrease treated as immediate purchase", err)
				}
			}
			if calls != 0 {
				t.Fatal("renewal decrease contacted tax provider")
			}
			state.Orders[1].Quote.PeriodEnd = new(time.Time)
			*state.Orders[1].Quote.PeriodEnd = quote.PeriodEnd.Add(time.Hour)
			if _, _, err := prepareAlertsCheckout(context.Background(), state, term.provider, 4, term.months, halfway, address, calculate); !errors.Is(err, ErrAlertsPlanReview) {
				t.Fatal("mismatched expiry accepted", err)
			}
		})
	}
}

func TestPrepareAlertsCheckoutUnresolvedState(t *testing.T) {
	now := time.Now().UTC()
	full, _ := servicebilling.AlertsTerm("opennode", 1, 12, now)
	quote := getters.ServiceQuote{Provider: "opennode", Quantity: 1, UnitCents: 1200, TotalCents: 1200, PeriodStart: &full.Start, PeriodEnd: &full.End, ExpiresAt: now.Add(time.Minute)}
	for _, tc := range []struct {
		name  string
		order getters.AlertsBillingOrder
		want  error
	}{
		{"pending", getters.AlertsBillingOrder{Status: "pending", Quote: quote}, ErrAlertsPendingPurchase},
		{"unfulfilled", getters.AlertsBillingOrder{Status: "paid", Quote: quote}, ErrAlertsPendingPurchase},
		{"scheduled", getters.AlertsBillingOrder{Status: "paid", Granted: true, Quote: func() getters.ServiceQuote { q := quote; s := now.Add(time.Hour); q.PeriodStart = &s; return q }()}, ErrAlertsPlanReview},
		{"expired checkout with payment", getters.AlertsBillingOrder{Status: "pending", PaymentStarted: true, Quote: func() getters.ServiceQuote { q := quote; q.ExpiresAt = now.Add(-time.Minute); return q }()}, ErrAlertsPendingPurchase},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := prepareAlertsCheckout(context.Background(), &getters.AlertsBillingState{Revision: "r", Orders: []getters.AlertsBillingOrder{tc.order}}, "opennode", 2, 12, now, nil, func(*stripe.TaxCalculationParams) (*stripe.TaxCalculation, error) {
				t.Fatal("unresolved state contacted provider")
				return nil, nil
			})
			if !errors.Is(err, tc.want) {
				t.Fatal(err)
			}
		})
	}
}
