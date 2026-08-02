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

func TestDatabaseSmokeTicketPickupCannotFulfillAnotherBuyersItem(t *testing.T) {
	ctx := databaseSmokeContext(t)
	suffix := databaseSmokeSuffix()
	confID, _ := insertSmokeConference(t, ctx)
	productID, err := CreateMerchProduct(ctx, MerchProductInput{
		Tag: "pickup-" + suffix, Slug: "pickup-" + suffix,
		Name: "Pickup test", Status: types.MerchProductStatusPublished,
		BasePriceCents: 1000, Currency: "USD", AllowEventPickup: true,
	})
	if err != nil {
		t.Fatalf("create product: %s", err)
	}
	firstEmail := "pickup-first-" + suffix + "@example.test"
	secondEmail := "pickup-second-" + suffix + "@example.test"
	ticketRef := "pickup-ticket-" + suffix
	t.Cleanup(func() {
		_, _ = ctx.DB.Exec(context.Background(), `DELETE FROM registrations WHERE ref_id = $1`, ticketRef)
		_, _ = ctx.DB.Exec(context.Background(), `DELETE FROM shop_orders WHERE buyer_email IN ($1, $2)`, firstEmail, secondEmail)
		_, _ = ctx.DB.Exec(context.Background(), `DELETE FROM merch_products WHERE id = $1::uuid`, productID)
	})
	variantID, err := CreateMerchVariant(ctx, MerchVariantInput{
		ProductID: productID, SKU: "PICKUP-" + suffix, Label: "Default",
		InventoryPolicy: types.MerchInventoryPolicyDeny, Status: "active",
	})
	if err != nil {
		t.Fatalf("create variant: %s", err)
	}
	if err := AdjustMerchInventory(ctx, variantID, "initial", 2, "", "test stock"); err != nil {
		t.Fatalf("seed inventory: %s", err)
	}

	createPaidPickup := func(email, checkoutID string) *types.ShopOrderItem {
		order, err := CreateShopOrder(ctx, ShopOrderInput{
			BuyerEmail: email, PaymentProvider: "stripe", Currency: "USD",
			SubtotalCents: 1000, TotalCents: 1000,
		}, []ShopOrderItemInput{{
			ProductID: productID, VariantID: variantID, Quantity: 1,
			UnitPriceCents: 1000, LineTotalCents: 1000,
			ProductTagSnapshot: "pickup", ProductNameSnapshot: "Pickup test",
			VariantLabelSnapshot: "Default", SKUSnapshot: "PICKUP-" + suffix,
			FulfillmentMethod: types.ShopFulfillmentEventPickup,
			SaleConferenceID:  confID, PickupConferenceID: confID,
		}})
		if err != nil {
			t.Fatalf("create pickup order: %s", err)
		}
		if transitioned, err := MarkShopOrderPaid(ctx, order.ID, "stripe", checkoutID, 0, 1000); err != nil || !transitioned {
			t.Fatalf("mark pickup order paid = (%t, %v)", transitioned, err)
		}
		return order.Items[0]
	}

	firstItem := createPaidPickup(firstEmail, "cs_first_"+suffix)
	secondItem := createPaidPickup(secondEmail, "cs_second_"+suffix)
	if _, err := ctx.DB.Exec(context.Background(), `
		INSERT INTO registrations (ref_id, checkout_id, conference_id, type, email, item_bought, currency, platform)
		VALUES ($1, $2, $3::uuid, $4, $5, 'Test ticket', 'USD', 'test')
	`, ticketRef, "ticket-checkout-"+suffix, confID, types.TicketTypeGeneral, firstEmail); err != nil {
		t.Fatalf("insert registration: %s", err)
	}

	if err := MarkShopOrderItemPickedUpForTicket(ctx, ticketRef, secondItem.ID, "check-in", "test"); err == nil {
		t.Fatal("ticket unexpectedly fulfilled another buyer's pickup")
	}
	assertShopItemStatus(t, ctx, secondItem.ID, types.ShopItemStatusReady)

	if err := MarkShopOrderItemPickedUpForTicket(ctx, ticketRef, firstItem.ID, "check-in", "test"); err != nil {
		t.Fatalf("fulfill matching pickup: %s", err)
	}
	assertShopItemStatus(t, ctx, firstItem.ID, types.ShopItemStatusFulfilled)
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

func assertShopItemStatus(t *testing.T, app *config.AppContext, itemID, want string) {
	t.Helper()
	var got string
	if err := app.DB.QueryRow(app.DatabaseContext(), `
		SELECT status FROM shop_order_items WHERE id = $1::uuid
	`, itemID).Scan(&got); err != nil {
		t.Fatalf("load shop item status: %s", err)
	}
	if got != want {
		t.Fatalf("shop item status = %q, want %q", got, want)
	}
}
