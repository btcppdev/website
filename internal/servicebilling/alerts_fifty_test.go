package servicebilling

import (
	"testing"
	"time"
)

func TestAlertsFiftyAnnualPlan(t *testing.T) {
	start := time.Date(2027, 1, 31, 12, 0, 0, 0, time.UTC)
	plan, end, err := ProductTerm(AlertsFiftyProduct, "stripe", 1, start)
	if err != nil || plan.Family != "alerts" || plan.UnitCents != 18000 || plan.Months != 12 || plan.TaxBehavior != "inclusive" || plan.StripeTaxCode != AlertsStripeTaxCode || !end.Equal(time.Date(2028, 1, 31, 12, 0, 0, 0, time.UTC)) {
		t.Fatalf("plan=%+v end=%v err=%v", plan, end, err)
	}
	if AlertsFiftyProjectLimit != 50 {
		t.Fatal("wrong project capacity")
	}
	for _, quantity := range []int64{0, 2, 50} {
		if _, _, err := ProductTerm(AlertsFiftyProduct, "stripe", quantity, start); err == nil {
			t.Fatalf("accepted %d bundles", quantity)
		}
	}
	// Defining this plan does not change per-project billing.
	slots, _, err := ProductTerm(AlertsProduct, "stripe", 2, start)
	if err != nil || slots.UnitCents != 1200 || slots.Months != 12 {
		t.Fatal(slots, err)
	}
}

func TestAlertsFiftyPrepaidPrices(t *testing.T) {
	start := time.Date(2028, 2, 29, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		months int
		cents  int64
		year   int
	}{{12, 18000, 2029}, {24, 36000, 2030}, {120, 180000, 2038}} {
		p, err := AlertsFiftyPrepaidTerm(tc.months, start)
		if err != nil || p.Slots != 1 || p.TotalCents != tc.cents || p.UnitCents != tc.cents || p.End.Year() != tc.year || p.End.Month() != time.February || p.End.Day() != 28 {
			t.Fatalf("%+v %v", p, err)
		}
	}
	for _, months := range []int{-1, 0, 1, 13, 121} {
		if _, err := AlertsFiftyPrepaidTerm(months, start); err == nil {
			t.Fatalf("accepted %d", months)
		}
	}
}
