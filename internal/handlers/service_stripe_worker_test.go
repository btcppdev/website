package handlers

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestServiceStripeRecoveryDispatch(t *testing.T) {
	failure := errors.New("provider offline")
	var calls, reports []string
	var previous context.Context
	tasks := []serviceStripeRecoveryTask{}
	for _, product := range []string{"alerts", "insider"} {
		tasks = append(tasks, serviceStripeRecoveryTask{product, func(ctx context.Context) (int, error) {
			calls = append(calls, product)
			if ctx.Err() != nil {
				t.Fatal("inherited failed/cancelled product context")
			}
			deadline, ok := ctx.Deadline()
			if !ok || time.Until(deadline) > 90*time.Second {
				t.Fatal("missing product budget")
			}
			if previous != nil && previous.Err() != context.Canceled {
				t.Fatal("prior pass context not released")
			}
			previous = ctx
			if product == "alerts" {
				return 3, failure
			}
			return 2, nil
		}})
	}
	runServiceStripeRecoveryPass(context.Background(), tasks, func(product string, count int, err error) {
		reports = append(reports, product)
		if product == "alerts" && (count != 3 || !errors.Is(err, failure)) {
			t.Fatal("lost Alerts failure")
		}
		if product == "insider" && (count != 2 || err != nil) {
			t.Fatal("Insider affected by Alerts failure")
		}
	})
	if len(calls) != 2 || len(reports) != 2 || calls[0] != "alerts" || calls[1] != "insider" {
		t.Fatal(calls, reports)
	}
	if previous.Err() != context.Canceled {
		t.Fatal("last pass context not released")
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	runServiceStripeRecoveryPass(cancelled, tasks, func(string, int, error) { t.Fatal("cancelled pass ran") })
	if len(calls) != 2 {
		t.Fatal("cancelled pass contacted provider")
	}
}
