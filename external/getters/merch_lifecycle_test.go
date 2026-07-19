package getters

import (
	"context"
	"testing"

	"btcpp-web/internal/config"
	"btcpp-web/internal/types"
)

func TestDatabaseSmokeShopReservationLifecycleAndPaymentReplay(t *testing.T) {
	ctx := databaseSmokeContext(t)
	suffix := databaseSmokeSuffix()
	productID, err := CreateMerchProduct(ctx, MerchProductInput{
		Tag: "lifecycle-" + suffix, Slug: "lifecycle-" + suffix,
		Name: "Lifecycle test", Status: types.MerchProductStatusPublished,
		BasePriceCents: 1000, Currency: "USD", RequiresShipping: true,
	})
	if err != nil {
		t.Fatalf("create product: %s", err)
	}
	t.Cleanup(func() {
		_, _ = ctx.DB.Exec(context.Background(), `DELETE FROM shop_orders WHERE buyer_email = 'lifecycle@example.test'`)
		_, _ = ctx.DB.Exec(context.Background(), `DELETE FROM merch_products WHERE id = $1::uuid`, productID)
	})
	variantID, err := CreateMerchVariant(ctx, MerchVariantInput{
		ProductID: productID, SKU: "LIFE-" + suffix, Label: "Default",
		InventoryPolicy: types.MerchInventoryPolicyDeny, Status: "active",
	})
	if err != nil {
		t.Fatalf("create variant: %s", err)
	}
	if err := AdjustMerchInventory(ctx, variantID, "initial", 3, "", "test stock"); err != nil {
		t.Fatalf("seed inventory: %s", err)
	}

	createOrder := func() *types.ShopOrder {
		order, err := CreateShopOrder(ctx, ShopOrderInput{
			BuyerEmail: "lifecycle@example.test", PaymentProvider: "stripe",
			SubtotalCents: 2000, TotalCents: 2000,
		}, []ShopOrderItemInput{
			{
				Quantity: 1, UnitPriceCents: 0, LineTotalCents: 0,
				ProductTagSnapshot: "ticket", ProductNameSnapshot: "Test ticket",
				VariantLabelSnapshot: types.TicketTypeGeneral, SKUSnapshot: "ticket-" + suffix,
				FulfillmentMethod: types.ShopFulfillmentPOSTakeaway,
			},
			{
				ProductID: productID, VariantID: variantID, Quantity: 2,
				UnitPriceCents: 1000, LineTotalCents: 2000,
				ProductTagSnapshot: "lifecycle", ProductNameSnapshot: "Lifecycle test",
				VariantLabelSnapshot: "Default", SKUSnapshot: "LIFE-" + suffix,
				FulfillmentMethod: types.ShopFulfillmentShip,
			},
		})
		if err != nil {
			t.Fatalf("create order: %s", err)
		}
		return order
	}

	order := createOrder()
	assertVariantStock(t, ctx, variantID, 1)
	if err := CancelShopOrder(ctx, order.ID, "", "test cancel"); err != nil {
		t.Fatalf("cancel order: %s", err)
	}
	if err := CancelShopOrder(ctx, order.ID, "", "replay"); err != nil {
		t.Fatalf("cancel replay: %s", err)
	}
	assertVariantStock(t, ctx, variantID, 3)

	paid := createOrder()
	transitioned, err := MarkShopOrderPaid(ctx, paid.ID, "stripe", "cs_test_"+suffix, 0, 2000)
	if err != nil || !transitioned {
		t.Fatalf("first payment = (%t, %v), want transitioned", transitioned, err)
	}
	assertShopItemFulfillment(t, ctx, paid.ID, types.ShopFulfillmentPOSTakeaway, types.ShopItemStatusFulfilled, 1)

	// Replays should also reconcile paid orders written before pos_takeaway
	// fulfillment was part of the payment transition.
	if _, err := ctx.DB.Exec(ctx.DatabaseContext(), `
		UPDATE shop_order_items
		SET status = 'pending', fulfilled_quantity = 0
		WHERE order_id = $1::uuid AND fulfillment_method = 'pos_takeaway'
	`, paid.ID); err != nil {
		t.Fatalf("make paid takeaway item stale: %s", err)
	}
	transitioned, err = MarkShopOrderPaid(ctx, paid.ID, "stripe", "cs_test_"+suffix, 0, 2000)
	if err != nil || transitioned {
		t.Fatalf("payment replay = (%t, %v), want no transition", transitioned, err)
	}
	assertShopItemFulfillment(t, ctx, paid.ID, types.ShopFulfillmentPOSTakeaway, types.ShopItemStatusFulfilled, 1)
	assertVariantStock(t, ctx, variantID, 1)
}

func assertShopItemFulfillment(t *testing.T, app *config.AppContext, orderID, method, wantStatus string, wantQuantity int) {
	t.Helper()
	var status string
	var fulfilledQuantity int
	if err := app.DB.QueryRow(app.DatabaseContext(), `
		SELECT status, fulfilled_quantity
		FROM shop_order_items
		WHERE order_id = $1::uuid AND fulfillment_method = $2
	`, orderID, method).Scan(&status, &fulfilledQuantity); err != nil {
		t.Fatalf("load %s item fulfillment: %s", method, err)
	}
	if status != wantStatus || fulfilledQuantity != wantQuantity {
		t.Fatalf("%s item fulfillment = (%s, %d), want (%s, %d)", method, status, fulfilledQuantity, wantStatus, wantQuantity)
	}
}

func assertVariantStock(t *testing.T, app *config.AppContext, variantID string, want int) {
	t.Helper()
	var got int
	if err := app.DB.QueryRow(app.DatabaseContext(), `
		SELECT coalesce(sum(quantity_delta), 0)::int
		FROM merch_inventory_events WHERE variant_id = $1::uuid
	`, variantID).Scan(&got); err != nil {
		t.Fatalf("load stock: %s", err)
	}
	if got != want {
		t.Fatalf("stock = %d, want %d", got, want)
	}
}
