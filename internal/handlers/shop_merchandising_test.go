package handlers

import (
	"btcpp-web/internal/types"
	"fmt"
	"testing"
)

func TestShopBrowsingGroupsAndRecommendations(t *testing.T) {
	makeProduct := func(id, kind string) *types.MerchProduct {
		return &types.MerchProduct{ID: id, ProductType: kind, Variants: []*types.MerchVariant{{Status: "active", InventoryPolicy: types.MerchInventoryPolicyUnlimited}}}
	}
	canvas := makeProduct("canvas", types.MerchProductTypeBadgeCanvas)
	products := []*types.MerchProduct{canvas, makeProduct("pin", "pins"), makeProduct("sticker", "stickers"), makeProduct("shirt", "apparel"), makeProduct("hat", "accessories")}
	for i := 0; i < 6; i++ {
		products = append(products, makeProduct(fmt.Sprint(i), types.MerchProductTypeBadgeCanvas))
	}
	soldOut := makeProduct("sold-out", "apparel")
	soldOut.Variants = nil
	products = append(products, soldOut)
	categories := shopCategories(products)
	if len(categories) != 4 || categories[3].Slug != "stickers" || categories[3].Count != 2 {
		t.Fatal("stickers and pins should share one collection", categories)
	}
	recommendations := shopRecommendations(canvas, products)
	if len(recommendations) != 4 || recommendations[0].ID != "hat" {
		t.Fatal("expected complementary recommendations", recommendations)
	}
	for _, p := range recommendations {
		if p.ProductType == types.MerchProductTypeBadgeCanvas || p.ID == soldOut.ID {
			t.Fatal("unsuitable canvas recommendation", p.ID)
		}
	}
	if len(shopLandingProducts(products)) != 6 {
		t.Fatal("landing grid should be capped at six")
	}
	if shopFeaturedProduct(products) != canvas {
		t.Fatal("canvas should be the launch feature")
	}
	page := &shopPage{}
	setShopCollection(page, "pins")
	if page.ActiveCategory != "stickers" || page.CollectionHeading == "All merch." {
		t.Fatal("legacy pin category should use the combined collection")
	}
}
