package handlers

import (
	"testing"
	"time"

	"btcpp-web/internal/types"
)

func TestCloneMerchProductsDoesNotShareMutableGraph(t *testing.T) {
	now := time.Date(2026, time.September, 3, 12, 0, 0, 0, time.UTC)
	original := []*types.MerchProduct{{
		ID:            "product-1",
		Name:          "Original",
		AvailableFrom: &now,
		Images:        []*types.MerchProductImage{{ID: "image-1", AltText: "Original image"}},
		Options: []*types.MerchProductOption{{
			ID: "option-1", Values: []*types.MerchProductOptionValue{{ID: "value-1", Value: "Small"}},
		}},
		Variants: []*types.MerchVariant{{
			ID: "variant-1", Stock: 4, OptionValues: []*types.MerchProductOptionValue{{ID: "value-1", Value: "Small"}},
		}},
	}}

	cloned := cloneMerchProducts(original)
	cloned[0].Name = "Changed"
	*cloned[0].AvailableFrom = now.Add(time.Hour)
	cloned[0].Images[0].AltText = "Changed image"
	cloned[0].Options[0].Values[0].Value = "Large"
	cloned[0].Variants[0].Stock = 0
	cloned[0].Variants[0].OptionValues[0].Value = "Large"

	if original[0].Name != "Original" || !original[0].AvailableFrom.Equal(now) || original[0].Images[0].AltText != "Original image" || original[0].Options[0].Values[0].Value != "Small" || original[0].Variants[0].Stock != 4 || original[0].Variants[0].OptionValues[0].Value != "Small" {
		t.Fatalf("clone mutated original graph: %+v", original[0])
	}
}

func TestMerchProductStockTotalsVariants(t *testing.T) {
	product := &types.MerchProduct{Variants: []*types.MerchVariant{
		{Stock: 7},
		nil,
		{Stock: 4},
	}}
	if got, want := merchProductStock(product), 11; got != want {
		t.Fatalf("merchProductStock() = %d, want %d", got, want)
	}
}

func TestAdminMerchProductURL(t *testing.T) {
	if got, want := adminMerchProductURL("product-id", "flash", "Product updated."), "/admin/merch/product-id?flash=Product+updated."; got != want {
		t.Fatalf("adminMerchProductURL() = %q, want %q", got, want)
	}
}

func TestMerchSocialCardNeedsRefreshRequiresExactFingerprint(t *testing.T) {
	desired := "merch/social/v3/current-fingerprint.jpg"
	tests := []struct {
		name         string
		current      string
		objectExists bool
		want         bool
	}{
		{name: "missing", want: true},
		{name: "legacy version", current: "merch/social/v2/current-fingerprint.jpg", objectExists: true, want: true},
		{name: "stale current version", current: "merch/social/v3/old-fingerprint.jpg", objectExists: true, want: true},
		{name: "current fingerprint missing object", current: desired, want: true},
		{name: "current fingerprint", current: desired, objectExists: true, want: false},
		{name: "surrounding whitespace", current: "  " + desired + "  ", objectExists: true, want: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := merchSocialCardNeedsRefresh(test.current, desired, test.objectExists); got != test.want {
				t.Fatalf("merchSocialCardNeedsRefresh(%q, %q, %t) = %t, want %t", test.current, desired, test.objectExists, got, test.want)
			}
		})
	}
}

func TestMerchSocialCardObjectKeyTracksRenderedProductData(t *testing.T) {
	raw := []byte("product-image")
	base := merchSocialCardObjectKey(raw, "Core Hat", "$35")
	if got := merchSocialCardObjectKey(raw, "Core Hat", "$35"); got != base {
		t.Fatalf("merchSocialCardObjectKey is not deterministic: %q != %q", got, base)
	}
	for name, got := range map[string]string{
		"image": merchSocialCardObjectKey([]byte("different-image"), "Core Hat", "$35"),
		"name":  merchSocialCardObjectKey(raw, "Libre Relay Hat", "$35"),
		"price": merchSocialCardObjectKey(raw, "Core Hat", "$40"),
	} {
		if got == base {
			t.Fatalf("merchSocialCardObjectKey did not change with %s", name)
		}
	}
}

func TestFilterShopOrdersForAdminCountsAndFiltersShippingQueue(t *testing.T) {
	orders := []*types.ShopOrder{
		{PublicID: "ships-two", UnfulfilledShippingQuantity: 2},
		{PublicID: "complete"},
		nil,
		{PublicID: "ships-one", UnfulfilledShippingQuantity: 1},
		{PublicID: "pickup-four", EventPickupQuantity: 4},
	}

	all, total, needsShipping, eventPickup := filterShopOrdersForAdmin(orders, "all")
	if len(all) != 4 || total != 4 || needsShipping != 2 || eventPickup != 1 {
		t.Fatalf("all orders = %d, total = %d, needs shipping = %d, event pickup = %d", len(all), total, needsShipping, eventPickup)
	}

	queue, total, needsShipping, _ := filterShopOrdersForAdmin(orders, "needs_shipping")
	if len(queue) != 2 || total != 4 || needsShipping != 2 {
		t.Fatalf("shipping queue = %d, total = %d, needs shipping = %d", len(queue), total, needsShipping)
	}
	if queue[0].PublicID != "ships-two" || queue[1].PublicID != "ships-one" {
		t.Fatalf("shipping queue order = %q, %q", queue[0].PublicID, queue[1].PublicID)
	}
	pickups, _, _, eventPickup := filterShopOrdersForAdmin(orders, "event_pickup")
	if len(pickups) != 1 || eventPickup != 1 || pickups[0].PublicID != "pickup-four" {
		t.Fatalf("pickup queue = %#v, event pickup = %d", pickups, eventPickup)
	}
}

func TestShopOrderUsesAutomatedStripeRefund(t *testing.T) {
	tests := []struct {
		name  string
		order *types.ShopOrder
		want  bool
	}{
		{name: "stripe checkout", order: &types.ShopOrder{PaymentProvider: "stripe", PaymentProviderID: "cs_test_123"}, want: true},
		{name: "stripe missing checkout", order: &types.ShopOrder{PaymentProvider: "stripe"}},
		{name: "opennode", order: &types.ShopOrder{PaymentProvider: "opennode", PaymentProviderID: "charge-123"}},
		{name: "bitcoin", order: &types.ShopOrder{PaymentProvider: "btc"}},
		{name: "nil"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := shopOrderUsesAutomatedStripeRefund(test.order); got != test.want {
				t.Fatalf("shopOrderUsesAutomatedStripeRefund() = %t, want %t", got, test.want)
			}
		})
	}
}
