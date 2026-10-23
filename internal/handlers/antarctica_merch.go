package handlers

import (
	"btcpp-web/external/getters"
	"btcpp-web/internal/config"
	"btcpp-web/internal/types"
	"time"
)

type antarcticaMerch struct {
	ID, Name, Variant, Image, Price string
	Cents                           uint
	Max                             int
}

// Default to the existing hats. A disconnected design preview uses a labeled
// public-shop price snapshot, never a fabricated purchasable variant identity.
func antarcticaCheckoutMerch(ctx *config.AppContext) ([]antarcticaMerch, bool) {
	if ctx.DB == nil {
		return []antarcticaMerch{
			{"preview-core", "bitcoin-core hat", "", "/static/img/merch/core-hat.avif", "$38", 3800, 25},
			{"preview-libbitcoin", "libbitcoin hat", "", "/static/img/merch/libbit-hat.avif", "$38", 3800, 25},
			{"preview-libre", "libre relay hat", "", "/static/img/merch/librerelay-hat.avif", "$38", 3800, 25},
		}, true
	}
	products, err := getters.ListMerchProducts(ctx, false)
	if err != nil {
		ctx.Err.Printf("Antarctica merch catalog: %s", err)
		return nil, false
	}
	return antarcticaHatOptions(products, time.Now()), false
}

func antarcticaHatOptions(products []*types.MerchProduct, now time.Time) []antarcticaMerch {
	var out []antarcticaMerch
	for _, slug := range []string{"bitcoin-core-hat", "libbitcoin", "libre-relay"} {
		for _, p := range products {
			if p == nil || p.Slug != slug || p.Status != types.MerchProductStatusPublished || p.Currency != "USD" {
				continue
			}
			if p.AvailableFrom != nil && now.Before(*p.AvailableFrom) || p.AvailableUntil != nil && !now.Before(*p.AvailableUntil) {
				continue
			}
			for _, v := range p.Variants {
				if v == nil || !merchVariantAvailable(v, 1) {
					continue
				}
				max := 25
				if v.InventoryPolicy == types.MerchInventoryPolicyDeny && v.Stock < max {
					max = v.Stock
				}
				price := merchVariantPrice(p, v)
				out = append(out, antarcticaMerch{v.ID, p.Name, v.Label, merchImage(p), antarcticaMoney(int64(price)), price, max})
			}
		}
	}
	return out
}
