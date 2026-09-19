package servicebilling

import (
	"testing"
	"time"
)

func TestProductTermsKeepPricingAndCapacitySeparate(t *testing.T) {
	start := time.Date(2028, 1, 31, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		code, provider, family, tax string
		quantity, cents             int64
		end                         string
	}{
		{AlertsProduct, "stripe", "alerts", "inclusive", 4, 200, "2028-02-29"},
		{InsiderMonthlyProduct, "stripe", "insider", "exclusive", 1, 2100, "2028-02-29"},
		{InsiderAnnualProduct, "stripe", "insider", "exclusive", 1, 21000, "2029-01-31"},
		{InsiderAnnualProduct, "opennode", "insider", "exclusive", 1, 21000, "2029-01-31"},
	} {
		t.Run(tc.code+tc.provider, func(t *testing.T) {
			p, end, err := ProductTerm(tc.code, tc.provider, tc.quantity, start)
			if err != nil || p.Family != tc.family || p.TaxBehavior != tc.tax || p.UnitCents != tc.cents || end.Format("2006-01-02") != tc.end {
				t.Fatalf("plan=%+v end=%v err=%v", p, end, err)
			}
		})
	}
	for _, tc := range []struct {
		code, provider string
		q              int64
	}{
		{InsiderMonthlyProduct, "stripe", 2}, {InsiderAnnualProduct, "stripe", 2},
		{InsiderMonthlyProduct, "opennode", 1}, {AlertsProduct, "opennode", 1},
		{AlertsProduct, "stripe", 0}, {AlertsProduct, "stripe", maxOrderCents},
		{"insider.article", "stripe", 1}, {"unknown", "stripe", 1},
	} {
		if _, _, err := ProductTerm(tc.code, tc.provider, tc.q, start); err == nil {
			t.Fatalf("accepted invalid recurring plan: %+v", tc)
		}
	}
	// Resolve a fresh value; callers cannot mutate the global pricing registry.
	p, _ := LookupProductPlan(AlertsProduct)
	p.UnitCents = 1
	again, _ := LookupProductPlan(AlertsProduct)
	if again.UnitCents != 200 {
		t.Fatal("caller modified server pricing")
	}
}

func TestProductTermClampsLeapDay(t *testing.T) {
	start := time.Date(2028, 2, 29, 23, 45, 0, 0, time.UTC)
	_, end, err := ProductTerm(InsiderAnnualProduct, "stripe", 1, start)
	if err != nil || !end.Equal(time.Date(2029, 2, 28, 23, 45, 0, 0, time.UTC)) {
		t.Fatal(end, err)
	}
}
