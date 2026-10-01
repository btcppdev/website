package getters

import (
	"context"
	"errors"
	"testing"
	"time"

	"btcpp-web/internal/config"
)

func TestClosureReportRejectsInvalidScopeBeforeDatabase(t *testing.T) {
	for _, service := range []string{"", "other", "alerts.repository-slots"} {
		if _, err := PageBitcoinClosureReport(context.Background(), nil, service, time.Now(), ""); !errors.Is(err, ErrServiceRefund) {
			t.Fatalf("scope %q: %v", service, err)
		}
	}
	if _, err := PageBitcoinClosureReport(context.Background(), nil, "alerts", time.Now(), "invalid"); !errors.Is(err, ErrServiceRefundCursor) {
		t.Fatal(err)
	}
}

func TestClosureReportPostgres(t *testing.T) {
	ctx := context.Background()
	zero := int64(0)
	var app *config.AppContext
	targets := map[string]string{}
	for _, product := range []string{"alerts.repository-slots", "insider.membership.annual"} {
		var refund VerifiedServiceRefund
		app, refund, _ = bitcoinRefundProductFixture(t, &OpenNodeBitcoinEvidence{PriceSatoshis: 1001, OverpaidSatoshis: &zero}, product)
		targets[refund.AttemptID] = product
	}
	app, missing, _ := bitcoinRefundProductFixture(t, nil, "alerts.repository-slots")
	targets[missing.AttemptID] = "alerts.repository-slots"
	closure := time.Now().UTC().AddDate(0, 6, 0).Truncate(time.Second)
	seen := map[string]bool{}
	for _, service := range []string{"alerts", "insider"} {
		after := ""
		for pageNumber := 0; ; pageNumber++ {
			if pageNumber > 2000 {
				t.Fatal("pagination did not terminate")
			}
			page, err := pageBitcoinClosureReport(ctx, app, service, closure, after, 2)
			if err != nil {
				t.Fatal(err)
			}
			if len(page.Items) > 2 {
				t.Fatal("page unbounded")
			}
			for _, plan := range page.Items {
				if seen[plan.AttemptID] {
					t.Fatal("duplicate", plan.AttemptID)
				}
				seen[plan.AttemptID] = true
				if product, ok := targets[plan.AttemptID]; ok {
					if plan.ProductCode != product {
						t.Fatal("wrong product", plan)
					}
					if plan.AttemptID == missing.AttemptID {
						if len(plan.ReviewReasons) == 0 || plan.RemainingRefundSatoshis != nil {
							t.Fatal("missing evidence hidden", plan)
						}
					} else if plan.RemainingRefundSatoshis == nil || *plan.RemainingRefundSatoshis <= 0 {
						t.Fatal("missing obligation", plan)
					}
				}
			}
			if page.Next == "" {
				break
			}
			other := "alerts"
			if service == other {
				other = "insider"
			}
			if _, err := PageBitcoinClosureReport(ctx, nil, other, closure, page.Next); !errors.Is(err, ErrServiceRefundCursor) {
				t.Fatal("cross-service cursor accepted", err)
			}
			if _, err := PageBitcoinClosureReport(ctx, nil, service, closure.Add(time.Second), page.Next); !errors.Is(err, ErrServiceRefundCursor) {
				t.Fatal("changed closure accepted", err)
			}
			after = page.Next
		}
	}
	for id := range targets {
		if !seen[id] {
			t.Fatal("payment omitted", id)
		}
	}
	var count int
	for id := range targets {
		if err := app.DB.QueryRow(ctx, `SELECT count(*) FROM service_bitcoin_closure_reviews WHERE attempt_id=$1`, id).Scan(&count); err != nil || count != 0 {
			t.Fatal("inventory created review", count, err)
		}
	}
}
