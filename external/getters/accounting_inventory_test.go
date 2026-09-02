package getters

import (
	"context"
	"testing"
	"time"

	"btcpp-web/internal/types"
)

func TestDatabaseSmokeAccountingInventoryDeduplicatesTicketsAndTracksRefunds(t *testing.T) {
	ctx := databaseSmokeContext(t)
	suffix := databaseSmokeSuffix()
	confID, confTag := insertSmokeConference(t, ctx)
	productID, err := CreateMerchProduct(ctx, MerchProductInput{
		Tag: "accounting-" + suffix, Slug: "accounting-" + suffix,
		Name: "Accounting test merch", Status: types.MerchProductStatusPublished,
		BasePriceCents: 1000, Currency: "USD", RequiresShipping: true,
	})
	if err != nil {
		t.Fatalf("create accounting product: %v", err)
	}
	t.Cleanup(func() {
		_, _ = ctx.DB.Exec(context.Background(), `DELETE FROM shop_orders WHERE buyer_email = 'accounting-inventory@example.test'`)
		_, _ = ctx.DB.Exec(context.Background(), `DELETE FROM merch_products WHERE id = $1::uuid`, productID)
	})
	variantID, err := CreateMerchVariant(ctx, MerchVariantInput{
		ProductID: productID, SKU: "ACCT-" + suffix, Label: "Default",
		InventoryPolicy: types.MerchInventoryPolicyDeny, Status: "active",
	})
	if err != nil {
		t.Fatalf("create accounting variant: %v", err)
	}
	if err := AdjustMerchInventory(ctx, variantID, "initial", 3, "", "accounting API test stock"); err != nil {
		t.Fatalf("stock accounting variant: %v", err)
	}

	order, err := CreateShopOrder(ctx, ShopOrderInput{
		BuyerEmail: "accounting-inventory@example.test", PaymentProvider: "stripe",
		SubtotalCents: 2000, TotalCents: 2000,
	}, []ShopOrderItemInput{
		{
			Quantity: 1, UnitPriceCents: 0, LineTotalCents: 0,
			ProductTagSnapshot: "ticket", ProductNameSnapshot: "Accounting test ticket",
			VariantLabelSnapshot: types.TicketTypeGeneral, SKUSnapshot: "ticket-" + suffix,
			FulfillmentMethod: types.ShopFulfillmentPOSTakeaway, SaleConferenceID: confID,
		},
		{
			ProductID: productID, VariantID: variantID, Quantity: 2,
			UnitPriceCents: 1000, LineTotalCents: 2000,
			ProductTagSnapshot: "accounting-" + suffix, ProductNameSnapshot: "Accounting test merch",
			VariantLabelSnapshot: "Default", SKUSnapshot: "ACCT-" + suffix,
			FulfillmentMethod: types.ShopFulfillmentShip, SaleConferenceID: confID,
		},
	})
	if err != nil {
		t.Fatalf("create accounting order: %v", err)
	}
	if transitioned, err := MarkShopOrderPaid(ctx, order.ID, "stripe", "acct_"+suffix, 0, 2000); err != nil || !transitioned {
		t.Fatalf("mark accounting order paid = (%t, %v)", transitioned, err)
	}
	registeredAt := time.Now().UTC().Add(-time.Minute)
	var registrationID string
	if err := ctx.DB.QueryRow(ctx.DatabaseContext(), `
		INSERT INTO registrations (
			ref_id, checkout_id, conference_id, type, email, item_bought,
			amount_paid, currency, platform, registered_at, revoked
		) VALUES ($1, $2, $3::uuid, $4, $5, $6, 42.00, 'USD', 'stripe', $7, false)
		RETURNING id::text
	`, "acct-ticket-"+suffix, "acct-checkout-"+suffix, confID, types.TicketTypeGeneral,
		"accounting-inventory@example.test", "Accounting test ticket", registeredAt).Scan(&registrationID); err != nil {
		t.Fatalf("insert accounting registration: %v", err)
	}
	t.Cleanup(func() {
		_, _ = ctx.DB.Exec(context.Background(), `DELETE FROM registrations WHERE id = $1::uuid`, registrationID)
	})

	variants, err := ListAccountingInventoryVariants(ctx, time.Time{}, "", 1000)
	if err != nil {
		t.Fatalf("list accounting variants: %v", err)
	}
	var foundVariant *types.AccountingInventoryVariant
	for _, item := range variants {
		if item.SourceID == variantID {
			foundVariant = item
		}
	}
	if foundVariant == nil || foundVariant.OnHand != 1 {
		t.Fatalf("accounting variant = %+v, want on-hand 1", foundVariant)
	}

	sales, err := ListAccountingInventorySales(ctx, time.Time{}, "", 2000)
	if err != nil {
		t.Fatalf("list accounting sales: %v", err)
	}
	var merchSale, ticketSale *types.AccountingInventorySale
	for _, item := range sales {
		switch {
		case item.SourceID == "registration:"+registrationID:
			ticketSale = item
		case item.SKU == "ACCT-"+suffix:
			merchSale = item
		case item.SKU == "ticket-"+suffix:
			t.Fatalf("ticket-shaped shop order item was exposed alongside its registration: %+v", item)
		}
	}
	if merchSale == nil || merchSale.SellableSourceID != variantID || merchSale.Quantity != 2 || merchSale.RevenueCents != 2000 {
		t.Fatalf("merch accounting sale = %+v", merchSale)
	}
	wantTicketSellable := "sku:ticket:" + confTag + ":" + types.TicketTypeGeneral
	if ticketSale == nil || ticketSale.SellableSourceID != wantTicketSellable || ticketSale.Quantity != 1 || ticketSale.RevenueCents != 4200 {
		t.Fatalf("ticket accounting sale = %+v, want sellable %s", ticketSale, wantTicketSellable)
	}

	if err := RecordShopRefund(ctx, order.ID, merchSale.SourceID[len("shop_order_item:"):], "admin@example.test", "stripe", "refund_"+suffix, "test refund", 1, 1000, true); err != nil {
		t.Fatalf("refund accounting sale: %v", err)
	}
	sales, err = ListAccountingInventorySales(ctx, time.Time{}, "", 2000)
	if err != nil {
		t.Fatalf("reload accounting sales: %v", err)
	}
	for _, item := range sales {
		if item.SourceID == merchSale.SourceID {
			if item.RefundedQuantity != 1 || item.RevenueCents != 1000 {
				t.Fatalf("refunded accounting sale = %+v", item)
			}
			return
		}
	}
	t.Fatal("refunded accounting sale disappeared")
}
