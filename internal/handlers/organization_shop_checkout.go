package handlers

import (
	"btcpp-web/external/easyship"
	"btcpp-web/external/getters"
	"btcpp-web/internal/config"
	"btcpp-web/internal/types"
	"encoding/json"
	"fmt"
	"github.com/google/uuid"
	"net/http"
	"net/url"
	"sort"
	"strings"
)

const shopDiscountSessionKey = "shop_discount_code"

func ShopDiscount(w http.ResponseWriter, r *http.Request, app *config.AppContext) {
	limitRequestBody(w, r, maxFormBodyBytes)
	if e := r.ParseForm(); e != nil {
		http.Error(w, "Invalid form", 400)
		return
	}
	if !wizardCSRF(w, r, app) {
		return
	}
	code := strings.ToUpper(strings.TrimSpace(r.FormValue("code")))
	if code == "" || r.FormValue("remove") == "1" {
		app.Session.Remove(r.Context(), shopDiscountSessionKey)
		app.Session.Remove(r.Context(), shopShippingRatesSessionKey)
		http.Redirect(w, r, "/shop/cart", 303)
		return
	}
	discount, e := getters.FindOrganizationDiscount(app, code)
	if e == nil {
		cart, _ := loadShopCart(app, r)
		e = applyShopDiscount(cart, discount)
	}
	if e != nil {
		http.Redirect(w, r, "/shop/cart?err="+url.QueryEscape(e.Error()), 303)
		return
	}
	app.Session.Put(r.Context(), shopDiscountSessionKey, code)
	app.Session.Remove(r.Context(), shopShippingRatesSessionKey)
	http.Redirect(w, r, "/shop/cart", 303)
}
func applyShopDiscount(cart []*shopCartItem, d *types.OrganizationShopDiscount) error {
	if d == nil || d.PercentOff < 1 || d.PercentOff > 99 {
		return fmt.Errorf("Invalid discount")
	}
	var total uint
	for _, item := range cart {
		// Always start at the catalog price, including when replacing an existing code.
		item.UnitPriceCents = item.OriginalUnitPriceCents
		item.DiscountAmountCents = 0
		if item.Product.FulfillmentOrganizationID == d.OrganizationID {
			perUnit := item.UnitPriceCents * uint(d.PercentOff) / 100
			item.UnitPriceCents -= perUnit
			item.DiscountAmountCents = perUnit * item.Qty
			total += item.DiscountAmountCents
		}
		item.LineTotalCents = item.UnitPriceCents * item.Qty
	}
	if total == 0 {
		return fmt.Errorf("This code does not apply to any items in your cart")
	}
	return nil
}
func shopOriginRevisions(cart []*shopCartItem) map[string]int64 {
	out := map[string]int64{}
	for _, item := range cart {
		if id := item.Product.FulfillmentOrganizationID; id != "" {
			out[id] = item.OriginRevision
		}
	}
	return out
}
func shopItemPickupEligible(item *shopCartItem, eventID string) bool {
	if item == nil || item.Product == nil {
		return false
	}
	if item.Product.FulfillmentOrganizationID == "" {
		return item.Product.ProductType != types.MerchProductTypeBadgeCanvas || item.Product.AllowEventPickup
	}
	for _, id := range item.EligibleEvents {
		if id == eventID {
			return true
		}
	}
	return false
}

type shopOriginGroup struct {
	ID, Label string
	Items     []*shopCartItem
}

func shopOriginGroups(cart []*shopCartItem) []shopOriginGroup {
	groups := map[string]*shopOriginGroup{}
	for _, item := range cart {
		id := item.Product.FulfillmentOrganizationID
		group := groups[id]
		if group == nil {
			group = &shopOriginGroup{ID: id, Label: firstNonEmpty(item.OriginLabel, "bitcoin++")}
			groups[id] = group
		}
		group.Items = append(group.Items, item)
	}
	ids := make([]string, 0, len(groups))
	for id := range groups {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	out := make([]shopOriginGroup, 0, len(ids))
	for _, id := range ids {
		out = append(out, *groups[id])
	}
	return out
}
func shopCartShippingOrigin(app *config.AppContext, cart []*shopCartItem) (*types.EasyshipSettings, error) {
	groups := shopOriginGroups(cart)
	if len(groups) != 1 {
		return nil, fmt.Errorf("Each shipping quote must use one origin")
	}
	if groups[0].ID == "" {
		return getters.GetEasyshipSettings(app)
	}
	origin := groups[0].Items[0].ShipFrom
	if origin == nil || !origin.IsConfigured() {
		return nil, fmt.Errorf("Shipping is not configured for %s", groups[0].Label)
	}
	return origin, nil
}

type shopOriginRate struct {
	OrganizationID string        `json:"organization_id"`
	Label          string        `json:"label"`
	Rate           easyship.Rate `json:"rate"`
}
type shopShippingBundle struct {
	Rates []shopOriginRate `json:"organization_rates"`
}

func shopAvailableShippingRates(app *config.AppContext, r *http.Request, cart []*shopCartItem, fulfillment string, fallback uint) ([]easyship.Rate, easyship.Address, error) {
	groups := shopOriginGroups(cart)
	if len(groups) <= 1 || fulfillment != types.ShopFulfillmentShip {
		return shopSingleOriginShippingRates(app, r, cart, fulfillment, fallback)
	}
	// Offer the cheapest complete combinations. Each option includes every origin;
	// no package is silently dropped when one warehouse cannot quote shipping.
	var dest easyship.Address
	combinations := []shopShippingBundle{{}}
	for _, group := range groups {
		rates, address, e := shopSingleOriginShippingRates(app, r, group.Items, fulfillment, shopFallbackShippingCents(group.Items))
		if e != nil {
			return nil, address, e
		}
		dest = address
		if len(rates) == 0 {
			return nil, dest, fmt.Errorf("No shipping services are available for %s", group.Label)
		}
		combinations = combineShopShippingRates(combinations, group, rates)
	}
	out := make([]easyship.Rate, 0, len(combinations))
	for _, bundle := range combinations {
		raw, _ := json.Marshal(bundle)
		labels := []string{}
		var amount uint
		for _, part := range bundle.Rates {
			amount += part.Rate.AmountCents
			labels = append(labels, fmt.Sprintf("%s: %s %s (%s)", part.Label, part.Rate.CourierName, part.Rate.ServiceName, merchMoney(part.Rate.AmountCents, nil)))
		}
		out = append(out, easyship.Rate{ProviderQuoteID: "origins-" + uuid.NewString(), CourierName: fmt.Sprintf("%d separate shipments", len(bundle.Rates)), ServiceName: strings.Join(labels, " · "), AmountCents: amount, Currency: "USD", Raw: raw})
	}
	return out, dest, nil
}
func combineShopShippingRates(current []shopShippingBundle, group shopOriginGroup, rates []easyship.Rate) []shopShippingBundle {
	next := []shopShippingBundle{}
	sort.SliceStable(rates, func(i, j int) bool { return rates[i].AmountCents < rates[j].AmountCents })
	if len(rates) > 4 {
		rates = rates[:4]
	}
	for _, bundle := range current {
		for _, rate := range rates {
			parts := append([]shopOriginRate(nil), bundle.Rates...)
			parts = append(parts, shopOriginRate{OrganizationID: group.ID, Label: group.Label, Rate: rate})
			next = append(next, shopShippingBundle{Rates: parts})
		}
	}
	total := func(b shopShippingBundle) uint {
		var n uint
		for _, part := range b.Rates {
			n += part.Rate.AmountCents
		}
		return n
	}
	sort.SliceStable(next, func(i, j int) bool { return total(next[i]) < total(next[j]) })
	if len(next) > 12 {
		next = next[:12]
	}
	return next
}
func shopCheckoutTaxQuotes(app *config.AppContext, r *http.Request, cart []*shopCartItem, address *types.ShopAddress, shipping uint, fulfillment string) ([]*getters.TaxQuoteInput, uint, error) {
	if fulfillment != types.ShopFulfillmentShip {
		for _, item := range cart {
			if !shopItemPickupEligible(item, r.FormValue("pickup_conf_id")) {
				return nil, 0, fmt.Errorf("%s is not available for pickup at this event", item.Product.Name)
			}
		}
		origin := &types.EasyshipSettings{Line1: address.Line1, Line2: address.Line2, City: address.City, Region: address.Region, PostalCode: address.PostalCode, CountryAlpha2: address.Country}
		quote, e := shopStripeTaxQuote(cart, address, 0, "USD", origin)
		if e != nil {
			return nil, 0, e
		}
		return []*getters.TaxQuoteInput{quote}, quote.SalesTaxAmountCents, nil
	}
	groups := shopOriginGroups(cart)
	amounts := map[string]uint{}
	if len(groups) == 1 {
		amounts[groups[0].ID] = shipping
	} else {
		selected, e := shopShippingQuote(app, r, cart, fulfillment, r.FormValue("shipping_rate_id"), shipping)
		if e != nil {
			return nil, 0, e
		}
		var bundle shopShippingBundle
		if e = json.Unmarshal([]byte(selected.RawResponse), &bundle); e != nil {
			return nil, 0, e
		}
		for _, part := range bundle.Rates {
			if _, exists := amounts[part.OrganizationID]; exists {
				return nil, 0, errShopShippingRatesExpired
			}
			amounts[part.OrganizationID] = part.Rate.AmountCents
		}
		if len(amounts) != len(groups) {
			return nil, 0, errShopShippingRatesExpired
		}
	}
	var total uint
	quotes := []*getters.TaxQuoteInput{}
	for _, group := range groups {
		cost, ok := amounts[group.ID]
		if !ok {
			return nil, 0, errShopShippingRatesExpired
		}
		origin, e := shopCartShippingOrigin(app, group.Items)
		if e != nil {
			return nil, 0, e
		}
		if !origin.IsConfigured() {
			origin = nil
		}
		quote, e := shopStripeTaxQuote(group.Items, address, cost, "USD", origin)
		if e != nil {
			return nil, 0, e
		}
		quote.OriginKey = firstNonEmpty(group.ID, "default")
		total += quote.SalesTaxAmountCents
		quotes = append(quotes, quote)
	}
	return quotes, total, nil
}
func persistShopTaxQuotes(app *config.AppContext, orderID string, quotes []*getters.TaxQuoteInput) error {
	for _, q := range quotes {
		q.OrderID = orderID
		if e := getters.CreateTaxQuote(app, *q); e != nil {
			return e
		}
	}
	return nil
}
