package getters

import (
	"context"
	"encoding/json"
	"testing"

	"btcpp-web/internal/types"
)

func TestDatabaseSmokeEasyshipShipmentCreationIsIdempotent(t *testing.T) {
	ctx := databaseSmokeContext(t)
	suffix := databaseSmokeSuffix()
	productID, err := CreateMerchProduct(ctx, MerchProductInput{
		Tag: "easyship-" + suffix, Slug: "easyship-" + suffix, Name: "Easyship test",
		Status: types.MerchProductStatusPublished, BasePriceCents: 2500, Currency: "USD",
		EasyshipCategory: "fashion", CountryOfOrigin: "US", RequiresShipping: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = ctx.DB.Exec(context.Background(), `DELETE FROM shop_orders WHERE buyer_email = 'easyship-smoke@example.test'`)
		_, _ = ctx.DB.Exec(context.Background(), `DELETE FROM merch_products WHERE id = $1::uuid`, productID)
	})
	variantID, err := CreateMerchVariant(ctx, MerchVariantInput{
		ProductID: productID, SKU: "EASY-" + suffix, Label: "Default",
		WeightGrams: 200, LengthMM: 250, WidthMM: 200, HeightMM: 120,
		InventoryPolicy: types.MerchInventoryPolicyUnlimited, Status: "active",
	})
	if err != nil {
		t.Fatal(err)
	}
	order, err := CreateShopOrder(ctx, ShopOrderInput{
		BuyerEmail: "easyship-smoke@example.test", BuyerName: "Easy Ship",
		PaymentProvider: "stripe", SubtotalCents: 2500, ShippingAmountCents: 700,
		TotalCents: 3200, ShippingAddress: &types.ShopAddress{
			Name: "Easy Ship", Line1: "1 Main St", City: "Austin", Region: "TX", PostalCode: "78701", Country: "US",
		},
	}, []ShopOrderItemInput{{
		ProductID: productID, VariantID: variantID, Quantity: 1, UnitPriceCents: 2500,
		LineTotalCents: 2500, ProductTagSnapshot: "easyship", ProductNameSnapshot: "Easyship test",
		VariantLabelSnapshot: "Default", SKUSnapshot: "EASY-" + suffix, FulfillmentMethod: types.ShopFulfillmentShip,
	}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := MarkShopOrderPaid(ctx, order.ID, "stripe", "cs_easyship_"+suffix, 0, 3200); err != nil {
		t.Fatal(err)
	}
	if err := CreateShippingRateQuote(ctx, ShippingRateQuoteInput{
		OrderID: order.ID, Provider: types.ShippingProviderEasyship, ProviderQuoteID: "service-123",
		CourierName: "USPS", ServiceName: "Ground", AmountCents: 700, Currency: "USD",
	}); err != nil {
		t.Fatal(err)
	}
	quote, err := GetLatestEasyshipRateQuote(ctx, order.ID)
	if err != nil {
		t.Fatal(err)
	}
	first, err := PrepareEasyshipShipment(ctx, order.ID, "admin@example.test", quote)
	if err != nil {
		t.Fatal(err)
	}
	second, err := PrepareEasyshipShipment(ctx, order.ID, "admin@example.test", quote)
	if err != nil {
		t.Fatal(err)
	}
	if first.ID != second.ID || first.CreateIdempotencyKey != second.CreateIdempotencyKey {
		t.Fatalf("prepare was not idempotent: first=%+v second=%+v", first, second)
	}
	parcelItems, err := ListEasyshipShipmentParcelItems(ctx, first.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(parcelItems) != 1 || parcelItems[0].WeightGrams != 200 || parcelItems[0].Quantity != 1 || parcelItems[0].Category != "fashion" {
		t.Fatalf("parcel item snapshot = %+v", parcelItems)
	}
	providerShipmentID := "ESUS" + suffix
	if err := CompleteEasyshipShipmentCreation(ctx, first.ID, &types.Shipment{
		ProviderShipmentID: providerShipmentID, CourierServiceID: "service-123",
		CourierName: "USPS", ServiceName: "Ground", LabelState: "not_created",
	}, json.RawMessage(`{"shipment":{"easyship_shipment_id":"test"}}`), "admin@example.test"); err != nil {
		t.Fatal(err)
	}
	loaded, err := GetShopOrderByID(ctx, order.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Shipments) != 1 || loaded.Shipments[0].ProviderShipmentID != providerShipmentID || loaded.Shipments[0].CourierServiceID != "service-123" {
		t.Fatalf("loaded shipments = %+v", loaded.Shipments)
	}
	if loaded.UnfulfilledShippingQuantity != 1 || loaded.UnfulfilledShippingSummary != "Easyship test ×1" {
		t.Fatalf("loaded fulfillment summary = %d %q", loaded.UnfulfilledShippingQuantity, loaded.UnfulfilledShippingSummary)
	}
}
