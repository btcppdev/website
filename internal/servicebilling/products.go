package servicebilling

import (
	"errors"
	"time"
)

const (
	AlertsProduct               = "alerts.repository-slots"
	InsiderMonthlyProduct       = "insider.membership.monthly"
	InsiderAnnualProduct        = "insider.membership.annual"
	InsiderMonthlyCents   int64 = 2100
	InsiderAnnualCents    int64 = 21000
)

var ErrProductPlan = errors.New("unsupported service product or plan")

// ProductPlan is resolved from server code, never decoded from a checkout body.
// Family scopes subscription ownership; monthly and annual Insider plans belong
// to one membership, while Alerts capacity is a separate subscription.
type ProductPlan struct {
	Code, Family, Name, Currency, TaxBehavior string
	Months                                    int
	UnitCents, MaxQuantity                    int64
	StripeTaxCode                             string
}

func LookupProductPlan(code string) (ProductPlan, error) {
	switch code {
	case AlertsProduct:
		return ProductPlan{Code: code, Family: "alerts", Name: "Bitcoin++ Alerts additional repositories", Currency: "usd", TaxBehavior: "inclusive", Months: 12, UnitCents: AlertsAnnualSlotCents, MaxQuantity: maxOrderCents / AlertsAnnualSlotCents, StripeTaxCode: AlertsStripeTaxCode}, nil
	case InsiderMonthlyProduct:
		return ProductPlan{Code: code, Family: "insider", Name: "Bitcoin++ Insider one-month membership", Currency: "usd", TaxBehavior: "exclusive", Months: 1, UnitCents: InsiderMonthlyCents, MaxQuantity: 1}, nil
	case InsiderAnnualProduct:
		return ProductPlan{Code: code, Family: "insider", Name: "Bitcoin++ Insider annual membership", Currency: "usd", TaxBehavior: "exclusive", Months: 12, UnitCents: InsiderAnnualCents, MaxQuantity: 1}, nil
	default:
		return ProductPlan{}, ErrProductPlan
	}
}

// ProductTerm validates the provider/quantity and computes calendar-clamped dates.
// Amounts follow the product's tax behavior: Insider amounts exclude applicable
// tax, while Alerts amounts include it. Provider tax evidence remains separate.
func ProductTerm(code, provider string, quantity int64, start time.Time) (ProductPlan, time.Time, error) {
	plan, err := LookupProductPlan(code)
	if err != nil || quantity < 1 || quantity > plan.MaxQuantity || start.IsZero() || start.Year() < 1 || start.Year() > 9800 {
		return ProductPlan{}, time.Time{}, ErrProductPlan
	}
	if provider != "stripe" && !(provider == "opennode" && code == InsiderAnnualProduct) {
		return ProductPlan{}, time.Time{}, ErrProductPlan
	}
	return plan, monthBoundary(start.UTC(), plan.Months), nil
}
