package getters

import (
	"btcpp-web/internal/types"
	"fmt"
	"github.com/google/uuid"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestBadgeCanvasOrderOwnershipAndSnapshot(t *testing.T) {
	f := newMergeAccountsFixture(t)
	ctx := f.app
	suffix := uuid.NewString()
	product, err := CreateMerchProduct(ctx, MerchProductInput{Tag: "canvas-" + suffix, Slug: "canvas-" + suffix, Name: "Badge canvas", ProductType: types.MerchProductTypeBadgeCanvas, Status: types.MerchProductStatusPublished, BasePriceCents: 2500, Currency: "USD", RequiresShipping: true})
	if err != nil {
		t.Fatal(err)
	}
	variant, err := CreateMerchVariant(ctx, MerchVariantInput{ProductID: product, SKU: "CANVAS-" + suffix, Label: "9×9", InventoryPolicy: types.MerchInventoryPolicyUnlimited, Status: "active"})
	if err != nil {
		t.Fatal(err)
	}
	own := insertMergeBadgeGrant(t, ctx, f.org, f.canonical, f.canonical, "own", "issued")
	foreign := insertMergeBadgeGrant(t, ctx, f.org, f.source, f.source, "foreign", "issued")
	pending := insertMergeBadgeGrant(t, ctx, f.org, f.canonical, f.canonical, "pending", "granted")
	revoked := insertMergeBadgeGrant(t, ctx, f.org, f.canonical, f.canonical, "revoked", "revoked")
	accepted := insertMergeBadgeGrant(t, ctx, f.org, f.canonical, f.canonical, "accepted", "accepted")
	badges, err := ListPersonCanvasBadges(ctx, f.canonical)
	if err != nil || len(badges) != 2 {
		t.Fatal("eligibility list", len(badges), err)
	}
	base := ShopOrderItemInput{ProductID: product, VariantID: variant, Quantity: 1, UnitPriceCents: 2500, LineTotalCents: 2500, ProductNameSnapshot: "Badge canvas", VariantLabelSnapshot: "9×9", SKUSnapshot: "CANVAS-" + suffix, FulfillmentMethod: types.ShopFulfillmentShip}
	for _, tc := range []struct{ name, person, grant, fulfillment string }{
		{"foreign", f.canonical, foreign, types.ShopFulfillmentShip}, {"pending", f.canonical, pending, types.ShopFulfillmentShip}, {"revoked", f.canonical, revoked, types.ShopFulfillmentShip}, {"missing", f.canonical, "", types.ShopFulfillmentShip}, {"anonymous", "", own, types.ShopFulfillmentShip}, {"pickup disabled", f.canonical, own, types.ShopFulfillmentEventPickup},
	} {
		t.Run(tc.name, func(t *testing.T) {
			item := base
			item.BadgeReference = tc.grant
			item.FulfillmentMethod = tc.fulfillment
			if _, err := CreateShopOrder(ctx, ShopOrderInput{BuyerPersonID: tc.person, BuyerEmail: "canvas-test@example.test", TotalCents: 2500}, []ShopOrderItemInput{item}); err == nil {
				t.Fatal("unauthorized canvas order accepted")
			}
		})
	}
	one, two := base, base
	one.BadgeReference = own
	two.BadgeReference = accepted
	order, err := CreateShopOrder(ctx, ShopOrderInput{BuyerPersonID: f.canonical, BuyerEmail: "canvas-test@example.test", SubtotalCents: 5000, TotalCents: 5000}, []ShopOrderItemInput{one, two})
	if err != nil {
		t.Fatal(err)
	}
	if len(order.Items) != 2 || order.Items[0].BadgeCanvas == nil {
		t.Fatal("missing order snapshot")
	}
	if ok, err := PersonOwnsShopOrder(ctx, f.canonical, order.ID); err != nil || !ok {
		t.Fatal("order not tied to authenticated buyer", err)
	}
	_, err = ctx.DB.Exec(ctx.DatabaseContext(), `UPDATE organization_badge_grants SET badge_name='Changed',badge_image_url='https://example.test/changed.png',state='revoked' WHERE id=$1`, own)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := GetShopOrderByID(ctx, order.ID)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, item := range loaded.Items {
		if item.BadgeCanvas != nil && item.BadgeCanvas.Reference == own {
			found = true
			if item.BadgeCanvas.BadgeName != "Test badge" || item.BadgeCanvas.ArtworkURL != "https://example.test/badge.png" || item.BadgeCanvas.AwardEventID != strings.Repeat("b", 64) {
				t.Fatal("print snapshot changed", item.BadgeCanvas)
			}
		}
	}
	if !found {
		t.Fatal("snapshot missing after reload")
	}
	if err := CancelShopOrder(ctx, order.ID, "", "test cleanup"); err != nil {
		t.Fatal(err)
	}
}

func TestCanvasOrdersIncludeNonOrganizationStudioAwards(t *testing.T) {
	f := newMergeAccountsFixture(t)
	ctx := f.app
	var revoked atomic.Bool
	event := strings.Repeat("e", 64)
	studio := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path != "/api/public/btcpp/people/"+f.canonical+"/badges" {
			fmt.Fprint(w, `{"issued":[]}`)
			return
		}
		revocation := "null"
		if revoked.Load() {
			revocation = `{"reason":"revoked"}`
		}
		fmt.Fprintf(w, `{"issued":[{"definition":{"issuer_pubkey":%q,"identifier":"independent","name":"Independent issuer badge","image_url":"https://example.test/independent.png"},"award":{"event_id":%q,"revocation":%s}}]}`, strings.Repeat("d", 64), event, revocation)
	}))
	defer studio.Close()
	ctx.Env.BadgeStudioURL = studio.URL
	suffix := uuid.NewString()
	product, err := CreateMerchProduct(ctx, MerchProductInput{Tag: suffix, Slug: suffix, Name: "Canvas", ProductType: types.MerchProductTypeBadgeCanvas, Status: types.MerchProductStatusPublished, BasePriceCents: 2500, Currency: "USD", RequiresShipping: true})
	if err != nil {
		t.Fatal(err)
	}
	variant, err := CreateMerchVariant(ctx, MerchVariantInput{ProductID: product, SKU: suffix, Label: "9×9", InventoryPolicy: types.MerchInventoryPolicyUnlimited, Status: "active"})
	if err != nil {
		t.Fatal(err)
	}
	item := ShopOrderItemInput{ProductID: product, VariantID: variant, BadgeReference: "studio:" + event, Quantity: 1, UnitPriceCents: 2500, LineTotalCents: 2500, ProductNameSnapshot: "Canvas", FulfillmentMethod: types.ShopFulfillmentShip}
	input := ShopOrderInput{BuyerPersonID: f.canonical, BuyerEmail: "canvas-test@example.test", SubtotalCents: 2500, TotalCents: 2500}
	order, err := CreateShopOrder(ctx, input, []ShopOrderItemInput{item})
	if err != nil {
		t.Fatal(err)
	}
	if order.Items[0].BadgeCanvas == nil || order.Items[0].BadgeCanvas.BadgeName != "Independent issuer badge" {
		t.Fatal("non-organization Studio award missing")
	}
	if _, err := ctx.DB.Exec(ctx.DatabaseContext(), `UPDATE merch_products SET allow_event_pickup=true WHERE id=$1`, product); err != nil {
		t.Fatal(err)
	}
	pickup := item
	pickup.FulfillmentMethod = types.ShopFulfillmentEventPickup
	pickup.PickupConferenceID = f.conf
	pickupOrder, err := CreateShopOrder(ctx, input, []ShopOrderItemInput{pickup})
	if err != nil {
		t.Fatal("canvas event pickup", err)
	}
	if pickupOrder.Items[0].PickupConferenceID != f.conf || pickupOrder.Items[0].BadgeCanvas == nil {
		t.Fatal("pickup lost event or badge design")
	}
	input.BuyerPersonID = f.source
	if _, err := CreateShopOrder(ctx, input, []ShopOrderItemInput{item}); err == nil {
		t.Fatal("other person bought a Studio award")
	}
	input.BuyerPersonID = f.canonical
	revoked.Store(true)
	if _, err := CreateShopOrder(ctx, input, []ShopOrderItemInput{item}); err == nil {
		t.Fatal("revoked Studio award was printable")
	}
}
