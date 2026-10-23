package handlers

import (
	"btcpp-web/internal/types"
	"testing"
	"time"
)

func TestAntarcticaHatOptions(t *testing.T) {
	p := &types.MerchProduct{Slug: "bitcoin-core-hat", Name: "Core hat", Status: "published", Currency: "USD", BasePriceCents: 3800, Variants: []*types.MerchVariant{
		{ID: "available", Label: "Default", Status: "active", InventoryPolicy: "deny", Stock: 3, PriceDeltaCents: 200},
		{ID: "sold-out", Status: "active", InventoryPolicy: "deny", Stock: 0},
		{ID: "inactive", Status: "inactive", InventoryPolicy: "unlimited"},
	}}
	options := antarcticaHatOptions([]*types.MerchProduct{p}, time.Now())
	if len(options) != 1 || options[0].ID != "available" || options[0].Cents != 4000 || options[0].Max != 3 {
		t.Fatal(options)
	}
	future := time.Now().Add(time.Hour)
	p.AvailableFrom = &future
	if len(antarcticaHatOptions([]*types.MerchProduct{p}, time.Now())) != 0 {
		t.Fatal("future product offered")
	}
	p.AvailableFrom = nil
	p.Currency = "EUR"
	if len(antarcticaHatOptions([]*types.MerchProduct{p}, time.Now())) != 0 {
		t.Fatal("non USD product mispriced")
	}
}
