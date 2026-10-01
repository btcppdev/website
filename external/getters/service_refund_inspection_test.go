package getters

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestServiceRefundPaginationPostgres(t *testing.T) {
	ctx := context.Background()
	app, first, _ := refundTaxFixture(t, "alerts.repository-slots", 1, true)
	for i := 0; i < 101; i++ {
		next := first
		next.RefundReference = uuid.NewString()
		if err := RecordServiceRefund(ctx, app, next); err != nil {
			t.Fatal(err)
		}
	}
	page, err := PageServiceRefunds(ctx, app, first.AttemptID, "")
	if err != nil || len(page.Items) != 100 || page.Next == "" {
		t.Fatalf("first page %+v %v", page, err)
	}
	seen := map[string]bool{}
	for _, item := range page.Items {
		seen[item.RefundReference] = true
	}
	next, err := PageServiceRefunds(ctx, app, first.AttemptID, page.Next)
	if err != nil || len(next.Items) != 2 || next.Next != "" {
		t.Fatalf("last page %+v %v", next, err)
	}
	for _, item := range next.Items {
		if seen[item.RefundReference] {
			t.Fatal("duplicate item")
		}
		seen[item.RefundReference] = true
	}
	if len(seen) != 102 {
		t.Fatal("missing refunds")
	}
	if _, err := PageServiceRefunds(ctx, app, "", page.Next); !errors.Is(err, ErrServiceRefundCursor) {
		t.Fatal("cursor changed scope", err)
	}
	// Resolving records from a global page cannot shift the next page backwards.
	global, err := PageServiceRefunds(ctx, app, "", "")
	if err != nil {
		t.Fatal("global first page", err)
	}
	for {
		own := false
		for _, item := range global.Items {
			if item.AttemptID == first.AttemptID {
				own = true
			}
		}
		if own {
			break
		}
		if global.Next == "" {
			t.Fatal("fixture absent from global pages")
		}
		global, err = PageServiceRefunds(ctx, app, "", global.Next)
		if err != nil {
			t.Fatal(err)
		}
	}
	if global.Next == "" {
		t.Fatal("expected another page")
	}

	before, err := PageServiceRefunds(ctx, app, "", global.Next)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range global.Items {
		if item.AttemptID != first.AttemptID {
			continue
		}
		if _, err := app.DB.Exec(ctx, `UPDATE service_refund_jobs SET state='accepted' WHERE provider=$1 AND refund_reference=$2`, item.Provider, item.RefundReference); err != nil {
			t.Fatal(err)
		}
		if _, err := app.DB.Exec(ctx, `UPDATE service_refund_tax_jobs SET state='accepted' WHERE provider=$1 AND refund_reference=$2`, item.Provider, item.RefundReference); err != nil {
			t.Fatal(err)
		}
	}
	after, err := PageServiceRefunds(ctx, app, "", global.Next)
	if err != nil {
		t.Fatal(err)
	}
	if len(before.Items) != len(after.Items) {
		t.Fatal("resolved rows shifted page")
	}
	for i := range before.Items {
		if before.Items[i].RefundReference != after.Items[i].RefundReference {
			t.Fatal("cursor skipped item")
		}
	}
}

func TestServiceRefundPaginationRejectsInvalidCursor(t *testing.T) {
	for _, cursor := range []string{"!", "e30", strings.Repeat("x", 3000)} {
		if _, err := PageServiceRefunds(context.Background(), nil, "", cursor); !errors.Is(err, ErrServiceRefundCursor) {
			t.Fatal("bad cursor reached database", err)
		}
	}
}
