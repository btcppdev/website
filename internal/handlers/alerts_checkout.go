package handlers

import (
	"context"
	"errors"
	"time"

	"btcpp-web/external/getters"
	"btcpp-web/internal/config"
	"btcpp-web/internal/servicebilling"
	"btcpp-web/internal/types"
	"github.com/stripe/stripe-go/v86"
	stripeTaxCalculation "github.com/stripe/stripe-go/v86/tax/calculation"
)

var ErrAlertsPendingPurchase = errors.New("finish or reconcile the existing Alerts purchase first")
var ErrAlertsPlanReview = errors.New("existing Alerts periods require reconciliation before changing the plan")
var ErrAlertsRenewalChange = errors.New("equal or lower allowances must be handled at renewal")

// PrepareAlertsCheckout uses account-scoped billing evidence to quote a desired
// total number of PAID slots (the three free projects are not included). Prices,
// existing quantities, and upgrade periods are never supplied by the browser.
// Confirmation must persist this exact quote with CreateTaxedServicePurchase;
// AccountRevision prevents a second confirmation from silently changing it.
func PrepareAlertsCheckout(ctx context.Context, app *config.AppContext, personID, provider string, paidSlots int64, months int, address *types.ShopAddress) (*getters.ServiceQuote, *getters.TaxQuoteInput, error) {
	state, err := getters.LoadAlertsBillingState(app, personID)
	if err != nil {
		return nil, nil, err
	}
	return prepareAlertsCheckout(ctx, state, provider, paidSlots, months, time.Now().UTC().Truncate(time.Second), address, stripeTaxCalculation.New)
}

func prepareAlertsCheckout(ctx context.Context, state *getters.AlertsBillingState, provider string, paidSlots int64, months int, at time.Time, address *types.ShopAddress, calculate func(*stripe.TaxCalculationParams) (*stripe.TaxCalculation, error)) (*getters.ServiceQuote, *getters.TaxQuoteInput, error) {
	if state == nil || state.Revision == "" {
		return nil, nil, getters.ErrAlertsBillingChanged
	}
	// Validate requested terms even if the account already has a different plan.
	if _, err := servicebilling.AlertsTerm(provider, paidSlots, months, at); err != nil {
		return nil, nil, err
	}
	var active []getters.AlertsBillingOrder
	for _, order := range state.Orders {
		q := order.Quote
		if order.Status == "pending" && (q.ExpiresAt.After(at) || order.PaymentStarted) {
			return nil, nil, ErrAlertsPendingPurchase
		}
		if order.Status != "paid" || order.Revoked {
			continue
		}
		if q.PeriodStart == nil || q.PeriodEnd == nil {
			return nil, nil, ErrAlertsPlanReview
		}
		if !q.PeriodEnd.After(at) {
			continue
		}
		if !order.Granted {
			return nil, nil, ErrAlertsPendingPurchase
		}
		if q.PeriodStart.After(at) {
			return nil, nil, ErrAlertsPlanReview
		}
		active = append(active, order)
	}
	var quote *getters.ServiceQuote
	var tax *getters.TaxQuoteInput
	var err error
	var base *getters.AlertsBillingOrder
	if len(active) == 0 {
		quote, tax, err = alertsTermTaxQuote(ctx, provider, paidSlots, months, at, address, calculate)
	} else {
		var current int64
		for i := range active {
			order := &active[i]
			if order.Quote.Quantity < 1 || order.Quote.Quantity > 2147483647-current {
				return nil, nil, ErrAlertsPlanReview
			}
			current += order.Quote.Quantity
			if order.Quote.BaseOrderID == "" {
				if base != nil {
					return nil, nil, ErrAlertsPlanReview
				}
				base = order
			}
		}
		if base == nil {
			return nil, nil, ErrAlertsPlanReview
		}
		q := base.Quote
		// Older full-term quotes lack BillingMonths. Infer only an exact supported
		// full-price term, then verify its calendar boundary using central pricing.
		baseMonths := q.BillingMonths
		if baseMonths == 0 && q.UnitCents%servicebilling.AlertsMonthlySlotCents == 0 {
			baseMonths = int(q.UnitCents / servicebilling.AlertsMonthlySlotCents)
		}
		full, e := servicebilling.AlertsTerm(q.Provider, q.Quantity, baseMonths, *q.PeriodStart)
		if e != nil || full.TotalCents != q.TotalCents || full.UnitCents != q.UnitCents || !full.End.Equal(*q.PeriodEnd) || provider != q.Provider || months != baseMonths {
			return nil, nil, ErrAlertsPlanReview
		}
		for _, order := range active {
			if order.OrderID == base.OrderID {
				continue
			}
			if order.Quote.BaseOrderID != base.OrderID || order.Quote.Provider != provider || order.Quote.BillingMonths != baseMonths || !order.Quote.PeriodEnd.Equal(*q.PeriodEnd) {
				return nil, nil, ErrAlertsPlanReview
			}
		}
		if paidSlots <= current {
			return nil, nil, ErrAlertsRenewalChange
		}
		quote, tax, err = alertsIncreaseTaxQuote(ctx, provider, paidSlots-current, baseMonths, *q.PeriodStart, *q.PeriodEnd, at, address, calculate)
	}
	if err != nil {
		return nil, nil, err
	}
	quote.AccountRevision = state.Revision
	quote.BillingMonths = months
	if base != nil {
		quote.BaseOrderID = base.OrderID
	}
	return quote, tax, nil
}
