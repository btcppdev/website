package handlers

import (
	"btcpp-web/internal/types"
	"sort"
	"strings"
)

type shopCollection struct{ Slug, Label, Heading, Description string }

var shopCollections = []shopCollection{
	{"apparel", "Apparel", "Wear your bitcoin side.", "Everyday gear for builders, wherever the next block finds you."},
	{"accessories", "Accessories", "Finish the kit.", "Hats and everyday extras for life between conferences."},
	{"prints", "Prints", "Your achievements, on your wall.", "Turn a badge you have earned into something you can hang."},
	{"stickers", "Stickers & pins", "Small things. Big signal.", "Give your laptop, jacket, or bag a little bitcoin personality."},
	{"exclusive", "Attendee exclusives", "A little piece of being there.", "Explore merchandise from the bitcoin++ community and its events."},
	{"standard", "More merch", "More for your collection.", "Find your next bitcoin++ favorite."},
}

func shopCollectionSlug(raw string) string {
	switch shopCategorySlug(raw) {
	case "shirt", "shirts", "t-shirt", "t-shirts", "tee", "hoodie", "apparel":
		return "apparel"
	case "hat", "hats", "cap", "accessories":
		return "accessories"
	case "badge_canvas", "canvas", "print", "prints":
		return "prints"
	case "pin", "pins", "patch", "patches", "sticker", "stickers", "stickers-&-patches":
		return "stickers"
	case "exclusive", "attendee-exclusives":
		return "exclusive"
	default:
		return "standard"
	}
}

// Deliberate launch features. Unknown products still appear in the collection.
func shopFeaturedProduct(products []*types.MerchProduct) *types.MerchProduct {
	for _, tag := range []string{"badge-canvas", "core-hat", "libbit-hat", "bpp-hat"} {
		for _, p := range products {
			if (p.Tag == tag || (tag == "badge-canvas" && p.ProductType == types.MerchProductTypeBadgeCanvas)) && !merchProductSoldOut(p) {
				return p
			}
		}
	}
	return nil
}

func shopLandingProducts(products []*types.MerchProduct) []*types.MerchProduct {
	// Give each collection a place in the first row before repeating a category.
	ranked := append([]*types.MerchProduct(nil), products...)
	sort.SliceStable(ranked, func(i, j int) bool { return !merchProductSoldOut(ranked[i]) && merchProductSoldOut(ranked[j]) })
	var out []*types.MerchProduct
	seen := map[string]bool{}
	ids := map[string]bool{}
	for _, p := range ranked {
		category := shopCollectionSlug(p.ProductType)
		if !seen[category] && len(out) < 6 {
			out = append(out, p)
			seen[category] = true
			ids[p.ID] = true
		}
	}
	for _, p := range ranked {
		if len(out) >= 6 {
			break
		}
		if !ids[p.ID] {
			out = append(out, p)
			ids[p.ID] = true
		}
	}
	return out
}

func shopRecommendations(product *types.MerchProduct, products []*types.MerchProduct) []*types.MerchProduct {
	category := shopCollectionSlug(product.ProductType)
	pairings := map[string][]string{
		"apparel":     {"accessories", "stickers", "prints"},
		"accessories": {"apparel", "stickers", "prints"},
		"prints":      {"accessories", "apparel", "stickers"},
		"stickers":    {"apparel", "accessories", "prints"},
	}
	order := append(append([]string(nil), pairings[category]...), "exclusive", "standard", category)
	var out []*types.MerchProduct
	seen := map[string]bool{product.ID: true}
	for _, group := range order {
		// One from each complementary category first; fill remaining places below.
		for _, p := range products {
			if len(out) == 4 {
				return out
			}
			if !seen[p.ID] && !merchProductSoldOut(p) && shopCollectionSlug(p.ProductType) == group && (product.ProductType != types.MerchProductTypeBadgeCanvas || p.ProductType != types.MerchProductTypeBadgeCanvas) {
				out = append(out, p)
				seen[p.ID] = true
				break
			}
		}
	}
	for _, p := range products {
		if len(out) == 4 {
			break
		}
		if !seen[p.ID] && !merchProductSoldOut(p) && (product.ProductType != types.MerchProductTypeBadgeCanvas || p.ProductType != types.MerchProductTypeBadgeCanvas) {
			out = append(out, p)
			seen[p.ID] = true
		}
	}
	return out
}

func setShopCollection(page *shopPage, selected string) {
	page.CollectionHeading = "All merch."
	page.CollectionDescription = "Wear it. Carry it. Make it yours. Gear for the people building on bitcoin."
	selected = strings.TrimSpace(selected)
	if selected == "" {
		return
	}
	selected = shopCollectionSlug(selected)
	for _, c := range shopCollections {
		if c.Slug == selected {
			page.ActiveCategory = c.Slug
			page.CollectionHeading = c.Heading
			page.CollectionDescription = c.Description
			page.Title = c.Label + " · bitcoin++ shop"
			return
		}
	}
}
