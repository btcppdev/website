package getters

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestServiceOrderShapePostgres(t *testing.T) {
	app := databaseSmokeContext(t)
	ctx := context.Background()
	for _, tc := range []struct {
		name, method, code, resource, start, end string
		tax                                      int
		valid                                    bool
	}{
		{"alerts term", "service", "alerts.repository-slots", "", "2026-09-01", "2036-09-01", 9, true},
		{"membership", "service", "insider.membership", "annual", "2026-09-01", "2027-09-01", 0, true},
		{"article", "digital", "insider.article", "publication-123", "", "", 9, true},
		{"physical unchanged", "ship", "", "", "", "", 0, true},
		{"missing product", "service", "", "", "2026-09-01", "2027-09-01", 0, false},
		{"missing end", "service", "alerts.repository-slots", "", "2026-09-01", "", 0, false},
		{"reversed period", "service", "alerts.repository-slots", "", "2027-09-01", "2026-09-01", 0, false},
		{"missing article", "digital", "insider.article", "", "", "", 0, false},
		{"article with period", "digital", "insider.article", "publication-123", "2026-09-01", "2027-09-01", 0, false},
		{"physical with service", "ship", "alerts.repository-slots", "", "", "", 0, false},
		{"tax above inclusive total", "service", "alerts.repository-slots", "", "2026-09-01", "2027-09-01", 101, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tx, err := app.DB.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback(ctx)
			var order string
			err = tx.QueryRow(ctx, `INSERT INTO shop_orders(public_id,buyer_email,checkout_kind)
				VALUES(gen_random_uuid()::text,'service@example.test','service') RETURNING id::text`).Scan(&order)
			if err != nil {
				t.Fatal(err)
			}
			_, err = tx.Exec(ctx, `INSERT INTO shop_order_items(order_id,quantity,unit_price_cents,line_total_cents,
				product_name_snapshot,fulfillment_method,service_product_code,service_resource_id,
				service_period_start,service_period_end,tax_amount_cents)
				VALUES($1,1,100,100,'Test product',$2,$3,$4,NULLIF($5,'')::timestamptz,NULLIF($6,'')::timestamptz,$7)`,
				order, tc.method, tc.code, tc.resource, tc.start, tc.end, tc.tax)
			if tc.valid {
				if err != nil {
					t.Fatal(err)
				}
			} else {
				var pgerr *pgconn.PgError
				if !errors.As(err, &pgerr) || pgerr.Code != "23514" || pgerr.ConstraintName != "shop_order_items_service_shape" {
					t.Fatalf("expected service shape constraint, got %v", err)
				}
			}
		})
	}
	for _, input := range []struct{ kind, method string }{{"service", "ship"}, {"digital", "ship"}, {"mixed", "service"}, {"merch", "digital"}} {
		_, err := CreateShopOrder(app, ShopOrderInput{CheckoutKind: input.kind}, []ShopOrderItemInput{{FulfillmentMethod: input.method}})
		if err == nil || err.Error() != "service purchases require service checkout" {
			t.Fatalf("merchandise checkout gate: %v", err)
		}
	}
}
