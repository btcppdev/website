package getters

import (
	"testing"

	"btcpp-web/internal/types"
)

func TestPopulateShopOrderFulfillmentSummary(t *testing.T) {
	order := &types.ShopOrder{
		Status: types.ShopOrderStatusPaid,
		Items: []*types.ShopOrderItem{
			{ProductNameSnapshot: "Knots hat", Quantity: 4, FulfillmentMethod: types.ShopFulfillmentShip, Status: types.ShopItemStatusPending},
			{ProductNameSnapshot: "Core hat", Quantity: 3, FulfilledQuantity: 1, FulfillmentMethod: types.ShopFulfillmentEventPickup, Status: types.ShopItemStatusPending},
			{ProductNameSnapshot: "Old hat", Quantity: 1, FulfillmentMethod: types.ShopFulfillmentShip, Status: types.ShopItemStatusCancelled},
		},
	}

	populateShopOrderFulfillmentSummary(order)

	if order.UnfulfilledShippingQuantity != 4 || order.UnfulfilledShippingSummary != "Knots hat ×4" {
		t.Fatalf("shipping summary = %d %q", order.UnfulfilledShippingQuantity, order.UnfulfilledShippingSummary)
	}
	if order.EventPickupQuantity != 2 || order.EventPickupSummary != "Core hat ×2" {
		t.Fatalf("pickup summary = %d %q", order.EventPickupQuantity, order.EventPickupSummary)
	}
}

func TestPopulateShopOrderFulfillmentSummaryDoesNotQueuePendingShipping(t *testing.T) {
	order := &types.ShopOrder{
		Status: types.ShopOrderStatusPending,
		Items: []*types.ShopOrderItem{{
			ProductNameSnapshot: "Knots hat", Quantity: 4,
			FulfillmentMethod: types.ShopFulfillmentShip, Status: types.ShopItemStatusPending,
		}},
	}
	populateShopOrderFulfillmentSummary(order)
	if order.UnfulfilledShippingQuantity != 0 || order.UnfulfilledShippingSummary != "" {
		t.Fatalf("pending order entered shipping queue: %d %q", order.UnfulfilledShippingQuantity, order.UnfulfilledShippingSummary)
	}
}
