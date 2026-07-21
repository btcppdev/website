package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"math"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"btcpp-web/external/easyship"
	"btcpp-web/external/getters"
	"btcpp-web/external/spaces"
	"btcpp-web/internal/auth"
	"btcpp-web/internal/config"
	"btcpp-web/internal/emails"
	"btcpp-web/internal/helpers"
	"btcpp-web/internal/imgproc"
	"btcpp-web/internal/types"

	"github.com/gorilla/mux"
	stripe "github.com/stripe/stripe-go/v76"
	"github.com/stripe/stripe-go/v76/checkout/session"
	stripeRefund "github.com/stripe/stripe-go/v76/refund"
	stripeTaxCalculation "github.com/stripe/stripe-go/v76/tax/calculation"
	stripeTaxTransaction "github.com/stripe/stripe-go/v76/tax/transaction"
)

const shopCartSessionKey = "shop_cart_v1"
const shopActiveCheckoutSessionKey = "shop_active_checkout_v1"
const shopShippingRatesSessionKey = "shop_shipping_rates_v1"

const shopShippingRatesTTL = 20 * time.Minute
const shopEventPickupCloseDays = 7

var errShopShippingRatesExpired = errors.New("shipping services expired")

const shopFlatRateShippingCents uint = 2480

type shopCartLine struct {
	VariantID string `json:"variant_id"`
	Qty       uint   `json:"qty"`
}

type shopCartItem struct {
	Product        *types.MerchProduct
	Variant        *types.MerchVariant
	Qty            uint
	UnitPriceCents uint
	LineTotalCents uint
}

type shopPage struct {
	Title                     string
	Year                      int
	Products                  []*types.MerchProduct
	Product                   *types.MerchProduct
	Related                   []*types.MerchProduct
	Categories                []shopCategory
	Cart                      []*shopCartItem
	CartCount                 uint
	SubtotalCents             uint
	ShippingCents             uint
	TaxCents                  uint
	TotalCents                uint
	PickupConf                *types.Conf
	Confs                     []*types.Conf
	Order                     *types.ShopOrder
	Orders                    []*types.ShopOrder
	RefundContact             *types.ShopRefundContact
	ManualRefund              bool
	OrderView                 string
	OrderCount                uint
	NeedsShippingCount        uint
	EventPickupOrderCount     uint
	Flash                     string
	Error                     string
	Admin                     bool
	SpacesReady               bool
	ShopStats                 *types.ShopOperationalStats
	EasyshipRateQuote         *types.ShippingRateQuote
	EasyshipShipment          *types.Shipment
	CanCreateEasyshipShipment bool
	PendingCheckout           *types.ShopOrder
	Email                     string
	Checkout                  *shopCheckoutDetails
}

type shopCheckoutDetails struct {
	Name                    string
	Email                   string
	Fulfillment             string
	Address1                string
	Address2                string
	City                    string
	Region                  string
	PostalCode              string
	Country                 string
	Phone                   string
	PaymentMethod           string
	ShippingRateID          string
	ShippingRateAmountCents uint
	PickupConferenceID      string
}

type shopShippingRateResponse struct {
	ID          string `json:"id"`
	Courier     string `json:"courier"`
	Service     string `json:"service"`
	AmountCents uint   `json:"amount_cents"`
	Currency    string `json:"currency"`
	MinDays     *int   `json:"min_days,omitempty"`
	MaxDays     *int   `json:"max_days,omitempty"`
}

type shopShippingRateSet struct {
	AddressKey  string
	CartKey     string
	Destination easyship.Address
	Rates       []easyship.Rate
	ExpiresAt   time.Time
}

type shopCategory struct {
	Slug  string
	Label string
	Count int
	Tone  int
}

func StartShopMaintenance(ctx *config.AppContext) {
	if ctx == nil || ctx.DB == nil {
		return
	}
	go func() {
		expire := func() {
			count, err := getters.ExpirePendingShopOrders(ctx, 100)
			if err != nil {
				ctx.Err.Printf("shop maintenance expire pending orders: %s", err)
				return
			}
			if count > 0 {
				ctx.Infos.Printf("shop maintenance expired %d abandoned orders", count)
			}
		}
		processEasyship := func() {
			count, err := getters.ProcessEasyshipWebhookEvents(ctx, 25)
			if err != nil {
				ctx.Err.Printf("shop maintenance process Easyship webhooks: %s", err)
				return
			}
			if count > 0 {
				ctx.Infos.Printf("shop maintenance processed %d Easyship webhook events", count)
			}
		}
		expire()
		processEasyship()
		expireTicker := time.NewTicker(time.Minute)
		webhookTicker := time.NewTicker(5 * time.Second)
		defer expireTicker.Stop()
		defer webhookTicker.Stop()
		for {
			select {
			case <-expireTicker.C:
				expire()
			case <-webhookTicker.C:
				processEasyship()
			}
		}
	}()
}

func ShopHome(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	products, err := getters.ListMerchProducts(ctx, false)
	if err != nil {
		ctx.Err.Printf("/shop products: %s", err)
		http.Error(w, "Unable to load shop", http.StatusInternalServerError)
		return
	}
	page := baseShopPage(ctx, r, "bitcoin++ shop")
	page.Products = products
	page.Categories = shopCategories(products)
	if len(products) > 0 {
		page.Product = products[0]
	}
	renderShopTemplate(w, r, ctx, "shop/index.tmpl", page)
}

func ShopCollection(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	products, err := getters.ListMerchProducts(ctx, false)
	if err != nil {
		ctx.Err.Printf("/shop/all products: %s", err)
		http.Error(w, "Unable to load shop", http.StatusInternalServerError)
		return
	}
	cat := strings.TrimSpace(r.URL.Query().Get("cat"))
	if cat != "" {
		filtered := products[:0]
		for _, p := range products {
			if shopCategorySlug(p.ProductType) == cat {
				filtered = append(filtered, p)
			}
		}
		products = filtered
	}
	page := baseShopPage(ctx, r, "shop all")
	page.Products = products
	page.Categories = shopCategories(products)
	renderShopTemplate(w, r, ctx, "shop/collection.tmpl", page)
}

func ShopItem(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	slug := strings.TrimSpace(mux.Vars(r)["slug"])
	product, err := getters.GetMerchProductBySlug(ctx, slug, false)
	if err != nil {
		ctx.Err.Printf("/shop/%s: %s", slug, err)
		http.NotFound(w, r)
		return
	}
	products, _ := getters.ListMerchProducts(ctx, false)
	var related []*types.MerchProduct
	for _, p := range products {
		if p.ID != product.ID && shopCategorySlug(p.ProductType) == shopCategorySlug(product.ProductType) {
			related = append(related, p)
		}
	}
	for _, p := range products {
		if len(related) >= 4 {
			break
		}
		if p.ID != product.ID && shopCategorySlug(p.ProductType) != shopCategorySlug(product.ProductType) {
			related = append(related, p)
		}
	}
	page := baseShopPage(ctx, r, product.Name)
	page.Product = product
	page.Related = related
	renderShopTemplate(w, r, ctx, "shop/item.tmpl", page)
}

func ShopCart(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	page := baseShopPage(ctx, r, "cart")
	cart, err := loadShopCart(ctx, r)
	if err != nil {
		page.Error = "Some cart items could not be loaded."
	}
	page.Cart = cart
	fillCartTotals(page)
	renderShopTemplate(w, r, ctx, "shop/cart.tmpl", page)
}

func ShopCartAdd(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	limitRequestBody(w, r, maxFormBodyBytes)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	variantID := strings.TrimSpace(r.FormValue("variant_id"))
	qty := parseUintForm(r.FormValue("qty"), 1)
	if qty == 0 {
		qty = 1
	}
	variant, _, err := getters.GetMerchVariant(ctx, variantID)
	if err != nil {
		http.Error(w, "unknown variant", http.StatusBadRequest)
		return
	}
	if !merchVariantAvailable(variant, qty) {
		http.Redirect(w, r, "/shop/cart?err="+url.QueryEscape("That item is sold out."), http.StatusSeeOther)
		return
	}
	lines := readShopCart(ctx, r)
	found := false
	for i := range lines {
		if lines[i].VariantID == variantID {
			lines[i].Qty += qty
			if lines[i].Qty > 20 {
				lines[i].Qty = 20
			}
			if !merchVariantAvailable(variant, lines[i].Qty) {
				http.Redirect(w, r, "/shop/cart?err="+url.QueryEscape("There are not enough left in stock for that quantity."), http.StatusSeeOther)
				return
			}
			found = true
			break
		}
	}
	if !found {
		lines = append(lines, shopCartLine{VariantID: variantID, Qty: qty})
	}
	saveShopCart(ctx, r, lines)
	redirect := r.FormValue("redirect")
	if redirect == "" {
		redirect = "/shop/cart"
	}
	http.Redirect(w, r, redirect, http.StatusSeeOther)
}

func ShopCartUpdate(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	limitRequestBody(w, r, maxFormBodyBytes)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	lines := readShopCart(ctx, r)
	next := make([]shopCartLine, 0, len(lines))
	for _, line := range lines {
		qty := parseUintForm(r.FormValue("qty_"+line.VariantID), line.Qty)
		if qty == 0 {
			continue
		}
		if qty > 20 {
			qty = 20
		}
		variant, _, err := getters.GetMerchVariant(ctx, line.VariantID)
		if err != nil || !merchVariantAvailable(variant, qty) {
			http.Redirect(w, r, "/shop/cart?err="+url.QueryEscape("One of those items is sold out or does not have enough stock."), http.StatusSeeOther)
			return
		}
		next = append(next, shopCartLine{VariantID: line.VariantID, Qty: qty})
	}
	saveShopCart(ctx, r, next)
	http.Redirect(w, r, "/shop/cart", http.StatusSeeOther)
}

func ShopCheckout(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	page := baseShopPage(ctx, r, "checkout")
	page.PendingCheckout = pendingShopCheckout(ctx, r, page.Email)
	cart, err := loadShopCart(ctx, r)
	if err != nil || len(cart) == 0 {
		http.Redirect(w, r, "/shop/cart?err="+url.QueryEscape("Your cart is empty."), http.StatusSeeOther)
		return
	}
	page.Cart = cart
	page.Checkout = defaultShopCheckoutDetails(page.Email)
	fillCartTotals(page)
	renderShopTemplate(w, r, ctx, "shop/checkout.tmpl", page)
}

func ShopShippingRates(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	limitRequestBody(w, r, maxFormBodyBytes)
	w.Header().Set("Content-Type", "application/json")
	if err := parseShopRequestForm(r); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Unable to read the shipping address."})
		return
	}
	cart, err := loadShopCart(ctx, r)
	if err != nil || len(cart) == 0 {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Your cart is empty."})
		return
	}
	if err := validateShopCartInventory(cart); err != nil {
		w.WriteHeader(http.StatusConflict)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	fulfillment := strings.TrimSpace(r.FormValue("fulfillment"))
	if fulfillment == "pickup" {
		fulfillment = types.ShopFulfillmentEventPickup
	}
	if fulfillment == types.ShopFulfillmentEventPickup {
		if err := validateShopPickupSelection(nextShopPickupConf(ctx), r.FormValue("pickup_conf_id")); err != nil {
			w.WriteHeader(http.StatusUnprocessableEntity)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}
	}
	rates, dest, err := shopAvailableShippingRates(ctx, r, cart, fulfillment, shopFallbackShippingCents(cart))
	if err != nil {
		w.WriteHeader(http.StatusUnprocessableEntity)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	if fulfillment == types.ShopFulfillmentShip {
		if err := saveShopShippingRateSet(ctx, r, cart, dest, rates); err != nil {
			ctx.Err.Printf("/shop/shipping-rates save quote set: %s", err)
			w.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "Shipping services could not be saved. Please try again."})
			return
		}
	}
	out := make([]shopShippingRateResponse, 0, len(rates))
	for _, rate := range rates {
		out = append(out, shopShippingRateResponse{
			ID: rate.ProviderQuoteID, Courier: rate.CourierName, Service: rate.ServiceName,
			AmountCents: rate.AmountCents, Currency: rate.Currency,
			MinDays: rate.MinDays, MaxDays: rate.MaxDays,
		})
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"rates": out})
}

func ShopTaxQuote(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	limitRequestBody(w, r, maxFormBodyBytes)
	w.Header().Set("Content-Type", "application/json")
	if err := parseShopRequestForm(r); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Unable to read the tax address."})
		return
	}
	cart, err := loadShopCart(ctx, r)
	if err != nil || len(cart) == 0 {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Your cart is empty."})
		return
	}
	fulfillment := normalizedShopFulfillment(r.FormValue("fulfillment"))
	pickupConf := nextShopPickupConf(ctx)
	if fulfillment == types.ShopFulfillmentEventPickup {
		if err := validateShopPickupSelection(pickupConf, r.FormValue("pickup_conf_id")); err != nil {
			w.WriteHeader(http.StatusUnprocessableEntity)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}
	}
	shippingCents := uint(0)
	if fulfillment == types.ShopFulfillmentShip {
		shippingQuote, err := shopShippingQuote(ctx, r, cart, fulfillment,
			r.FormValue("shipping_rate_id"), parseUintForm(r.FormValue("shipping_rate_amount_cents"), 0))
		if err != nil || shippingQuote == nil {
			status := http.StatusUnprocessableEntity
			message := "Shipping must be selected before tax can be calculated."
			code := ""
			if err != nil {
				message = err.Error()
			}
			if errors.Is(err, errShopShippingRatesExpired) {
				status = http.StatusConflict
				message = "Shipping services changed. Please select a current option."
				code = "shipping_rates_expired"
			}
			w.WriteHeader(status)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": message, "code": code})
			return
		}
		shippingCents = shippingQuote.AmountCents
	}
	address, err := shopTaxAddress(ctx, r, fulfillment, pickupConf)
	if err != nil {
		w.WriteHeader(http.StatusUnprocessableEntity)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	quote, err := shopStripeTaxQuote(cart, address, shippingCents)
	if err != nil {
		ctx.Err.Printf("/shop/tax-quote: %s", err)
		w.WriteHeader(http.StatusUnprocessableEntity)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Tax could not be calculated for this order."})
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]any{
		"tax_cents":   quote.SalesTaxAmountCents,
		"total_cents": shopCartSubtotal(cart) + shippingCents + quote.SalesTaxAmountCents,
	})
}

func parseShopRequestForm(r *http.Request) error {
	contentType := strings.ToLower(strings.TrimSpace(r.Header.Get("Content-Type")))
	if strings.HasPrefix(contentType, "multipart/form-data") {
		return r.ParseMultipartForm(maxFormBodyBytes)
	}
	return r.ParseForm()
}

func ShopCheckoutCreate(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	limitRequestBody(w, r, maxFormBodyBytes)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	cart, err := loadShopCart(ctx, r)
	if err != nil || len(cart) == 0 {
		http.Redirect(w, r, "/shop/cart?err="+url.QueryEscape("Your cart is empty."), http.StatusSeeOther)
		return
	}
	page := baseShopPage(ctx, r, "checkout")
	page.Cart = cart
	page.Checkout = shopCheckoutDetailsFromRequest(r, page.Email)
	fillCartTotals(page)

	name := page.Checkout.Name
	email := page.Checkout.Email
	if name == "" || !strings.Contains(email, "@") {
		page.Error = "Name and email are required."
		renderShopTemplate(w, r, ctx, "shop/checkout.tmpl", page)
		return
	}
	if err := validateShopCartInventory(cart); err != nil {
		page.Error = err.Error()
		renderShopTemplate(w, r, ctx, "shop/checkout.tmpl", page)
		return
	}
	page.PendingCheckout = pendingShopCheckout(ctx, r, email)
	if page.PendingCheckout != nil {
		page.Error = "A payment is already pending for this cart. Cancel it before starting another payment."
		renderShopTemplate(w, r, ctx, "shop/checkout.tmpl", page)
		return
	}
	fulfillment := normalizedShopFulfillment(page.Checkout.Fulfillment)
	if fulfillment == types.ShopFulfillmentEventPickup {
		if err := validateShopPickupSelection(page.PickupConf, page.Checkout.PickupConferenceID); err != nil {
			page.Error = err.Error()
			page.Checkout.Fulfillment = types.ShopFulfillmentShip
			renderShopTemplate(w, r, ctx, "shop/checkout.tmpl", page)
			return
		}
	}
	var shippingAddress *types.ShopAddress
	if fulfillment == types.ShopFulfillmentShip {
		shippingAddress = &types.ShopAddress{
			Name:       name,
			Line1:      page.Checkout.Address1,
			Line2:      page.Checkout.Address2,
			City:       page.Checkout.City,
			Region:     page.Checkout.Region,
			PostalCode: page.Checkout.PostalCode,
			Country:    page.Checkout.Country,
			Phone:      page.Checkout.Phone,
		}
		if shippingAddress.Line1 == "" || shippingAddress.City == "" || shippingAddress.PostalCode == "" || shippingAddress.Country == "" || shippingAddress.Phone == "" {
			page.Error = "A complete shipping address and phone number are required for shipped orders."
			renderShopTemplate(w, r, ctx, "shop/checkout.tmpl", page)
			return
		}
	}
	pickupConfID := ""
	if fulfillment == types.ShopFulfillmentEventPickup && page.PickupConf != nil {
		pickupConfID = page.PickupConf.Ref
		page.ShippingCents = 0
		page.TotalCents = page.SubtotalCents + page.TaxCents
	}
	shippingQuote, err := shopShippingQuote(ctx, r, cart, fulfillment, page.Checkout.ShippingRateID, page.Checkout.ShippingRateAmountCents)
	if err != nil {
		page.Error = err.Error()
		renderShopTemplate(w, r, ctx, "shop/checkout.tmpl", page)
		return
	}
	if fulfillment == types.ShopFulfillmentShip && shippingQuote != nil {
		page.ShippingCents = shippingQuote.AmountCents
		page.TotalCents = page.SubtotalCents + page.ShippingCents + page.TaxCents
	}
	paymentMethod := firstNonEmpty(strings.TrimSpace(page.Checkout.PaymentMethod), "btc")
	taxAddress, err := shopTaxAddress(ctx, r, fulfillment, page.PickupConf)
	if err != nil {
		page.Error = err.Error()
		renderShopTemplate(w, r, ctx, "shop/checkout.tmpl", page)
		return
	}
	taxQuote, err := shopStripeTaxQuote(cart, taxAddress, page.ShippingCents)
	if err != nil {
		ctx.Err.Printf("/shop/checkout tax quote: %s", err)
		page.Error = "Tax could not be calculated for this order. Please verify the delivery details."
		renderShopTemplate(w, r, ctx, "shop/checkout.tmpl", page)
		return
	}
	page.TaxCents = taxQuote.SalesTaxAmountCents
	page.TotalCents = page.SubtotalCents + page.ShippingCents + page.TaxCents

	items := make([]getters.ShopOrderItemInput, 0, len(cart))
	for _, item := range cart {
		items = append(items, getters.ShopOrderItemInput{
			ProductID:            item.Product.ID,
			VariantID:            item.Variant.ID,
			Quantity:             item.Qty,
			UnitPriceCents:       item.UnitPriceCents,
			LineTotalCents:       item.LineTotalCents,
			ProductTagSnapshot:   item.Product.Tag,
			ProductNameSnapshot:  item.Product.Name,
			VariantLabelSnapshot: item.Variant.Label,
			SKUSnapshot:          item.Variant.SKU,
			FulfillmentMethod:    fulfillment,
			PickupConferenceID:   pickupConfID,
			Status:               types.ShopItemStatusPending,
		})
	}
	order, err := getters.CreateShopOrder(ctx, getters.ShopOrderInput{
		BuyerEmail:          email,
		BuyerName:           name,
		Source:              types.ShopOrderSourceOnline,
		CheckoutKind:        types.ShopCheckoutKindMerch,
		PaymentProvider:     paymentMethod,
		Currency:            "USD",
		SubtotalCents:       page.SubtotalCents,
		ShippingAmountCents: page.ShippingCents,
		SalesTaxAmountCents: page.TaxCents,
		TotalCents:          page.TotalCents,
		ShippingAddress:     shippingAddress,
	}, items)
	if err != nil {
		ctx.Err.Printf("/shop/checkout create: %s", err)
		page.Error = "Unable to create order."
		renderShopTemplate(w, r, ctx, "shop/checkout.tmpl", page)
		return
	}
	if shippingQuote != nil {
		shippingQuote.OrderID = order.ID
		if err := getters.CreateShippingRateQuote(ctx, *shippingQuote); err != nil {
			ctx.Err.Printf("/shop/checkout shipping quote persist: %s", err)
			if cancelErr := getters.CancelShopOrder(ctx, order.ID, "", "shipping quote could not be saved"); cancelErr != nil {
				ctx.Err.Printf("/shop/checkout release order after shipping quote failure %s: %s", order.ID, cancelErr)
			}
			page.Error = "The selected shipping service could not be saved. Please choose it again."
			renderShopTemplate(w, r, ctx, "shop/checkout.tmpl", page)
			return
		}
	}
	taxQuote.OrderID = order.ID
	if err := getters.CreateTaxQuote(ctx, *taxQuote); err != nil {
		ctx.Err.Printf("/shop/checkout tax quote persist: %s", err)
		if cancelErr := getters.CancelShopOrder(ctx, order.ID, "", "tax quote could not be saved"); cancelErr != nil {
			ctx.Err.Printf("/shop/checkout release order after tax quote failure %s: %s", order.ID, cancelErr)
		}
		page.Error = "The tax calculation could not be saved. Please review the order and try again."
		renderShopTemplate(w, r, ctx, "shop/checkout.tmpl", page)
		return
	}
	ctx.Session.Put(r.Context(), shopActiveCheckoutSessionKey, order.PublicID)
	if paymentMethod == "card" {
		ShopStripeInit(w, r, ctx, order, cart)
		return
	}
	ShopOpenNodeInit(w, r, ctx, order)
}

func ShopOpenNodeInit(w http.ResponseWriter, r *http.Request, ctx *config.AppContext, order *types.ShopOrder) {
	payment, err := getters.InitOpenNodeShopCheckout(ctx, order)
	if err != nil {
		ctx.Err.Printf("/shop/checkout opennode session: %s", err)
		if cancelErr := getters.CancelShopOrder(ctx, order.ID, "", "OpenNode checkout could not start"); cancelErr != nil {
			ctx.Err.Printf("/shop/checkout release failed OpenNode order %s: %s", order.ID, cancelErr)
		}
		ctx.Session.Remove(r.Context(), shopActiveCheckoutSessionKey)
		http.Redirect(w, r, "/shop/cart?err="+url.QueryEscape("Bitcoin checkout could not start. Your cart was not charged."), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, payment.HostedCheckoutURL, http.StatusSeeOther)
}

func ShopStripeInit(w http.ResponseWriter, r *http.Request, ctx *config.AppContext, order *types.ShopOrder, cart []*shopCartItem) {
	if order == nil {
		http.Error(w, "missing order", http.StatusInternalServerError)
		return
	}
	params := shopStripeCheckoutParams(order, cart, ctx.Env.GetURI())
	s, err := session.New(params)
	if err != nil {
		ctx.Err.Printf("/shop/checkout stripe session: %s", err)
		if cancelErr := getters.CancelShopOrder(ctx, order.ID, "", "Stripe checkout could not start"); cancelErr != nil {
			ctx.Err.Printf("/shop/checkout release failed Stripe order %s: %s", order.ID, cancelErr)
		}
		ctx.Session.Remove(r.Context(), shopActiveCheckoutSessionKey)
		http.Redirect(w, r, "/shop/cart?err="+url.QueryEscape("Card checkout could not start. Your cart was not charged."), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, s.URL, http.StatusSeeOther)
}

func shopStripeCheckoutParams(order *types.ShopOrder, cart []*shopCartItem, domain string) *stripe.CheckoutSessionParams {
	metadata := map[string]string{
		"checkout-kind": "merch",
		"shop-order-id": order.ID,
	}
	lineItems := make([]*stripe.CheckoutSessionLineItemParams, 0, len(cart)+2)
	for _, item := range cart {
		if item == nil || item.Product == nil || item.Variant == nil {
			continue
		}
		name := item.Product.Name
		if item.Variant.Label != "" && item.Variant.Label != "Default" {
			name += " · " + item.Variant.Label
		}
		productData := &stripe.CheckoutSessionLineItemPriceDataProductDataParams{
			Name:        stripe.String(name),
			Description: stripe.String(item.Variant.SKU),
			Metadata:    metadata,
		}
		if code := strings.TrimSpace(item.Product.StripeTaxCode); code != "" {
			productData.TaxCode = stripe.String(code)
		}
		if productData.TaxCode == nil {
			productData.TaxCode = stripe.String(types.StripeTaxCodeTangibleGood)
		}
		lineItems = append(lineItems, &stripe.CheckoutSessionLineItemParams{
			PriceData: &stripe.CheckoutSessionLineItemPriceDataParams{
				ProductData: productData,
				TaxBehavior: stripe.String("exclusive"),
				UnitAmount:  stripe.Int64(int64(item.UnitPriceCents)),
				Currency:    stripe.String(strings.ToLower(order.Currency)),
			},
			Quantity: stripe.Int64(int64(item.Qty)),
		})
	}
	if order.ShippingAmountCents > 0 {
		lineItems = append(lineItems, &stripe.CheckoutSessionLineItemParams{
			PriceData: &stripe.CheckoutSessionLineItemPriceDataParams{
				ProductData: &stripe.CheckoutSessionLineItemPriceDataProductDataParams{
					Name:        stripe.String("Shipping"),
					Description: stripe.String("Selected during order review"),
				},
				UnitAmount: stripe.Int64(int64(order.ShippingAmountCents)),
				Currency:   stripe.String(strings.ToLower(order.Currency)),
			},
			Quantity: stripe.Int64(1),
		})
	}
	if order.SalesTaxAmountCents > 0 {
		lineItems = append(lineItems, &stripe.CheckoutSessionLineItemParams{
			PriceData: &stripe.CheckoutSessionLineItemPriceDataParams{
				ProductData: &stripe.CheckoutSessionLineItemPriceDataProductDataParams{
					Name:        stripe.String("Sales tax"),
					Description: stripe.String("Calculated from the delivery location"),
				},
				UnitAmount: stripe.Int64(int64(order.SalesTaxAmountCents)),
				Currency:   stripe.String(strings.ToLower(order.Currency)),
			},
			Quantity: stripe.Int64(1),
		})
	}
	params := &stripe.CheckoutSessionParams{
		AutomaticTax:  &stripe.CheckoutSessionAutomaticTaxParams{Enabled: stripe.Bool(false)},
		CustomerEmail: stripe.String(order.BuyerEmail),
		LineItems:     lineItems,
		Metadata:      metadata,
		Mode:          stripe.String(string(stripe.CheckoutSessionModePayment)),
		SuccessURL:    stripe.String(domain + "/shop/success/" + order.PublicID),
		CancelURL:     stripe.String(domain + "/shop/checkout/cancel/" + order.PublicID),
		ExpiresAt:     stripe.Int64(time.Now().Add(types.ShopCheckoutSessionTTL).Unix()),
	}
	if address := order.ShippingAddress; address != nil {
		shipping := &stripe.ShippingDetailsParams{
			Name: stripe.String(firstNonEmpty(strings.TrimSpace(address.Name), order.BuyerName)),
			Address: &stripe.AddressParams{
				Line1:      stripe.String(strings.TrimSpace(address.Line1)),
				Line2:      stripe.String(strings.TrimSpace(address.Line2)),
				City:       stripe.String(strings.TrimSpace(address.City)),
				State:      stripe.String(strings.TrimSpace(address.Region)),
				PostalCode: stripe.String(strings.TrimSpace(address.PostalCode)),
				Country:    stripe.String(strings.ToUpper(strings.TrimSpace(address.Country))),
			},
		}
		if phone := strings.TrimSpace(address.Phone); phone != "" {
			shipping.Phone = stripe.String(phone)
		}
		params.PaymentIntentData = &stripe.CheckoutSessionPaymentIntentDataParams{
			Shipping: shipping,
		}
	}
	return params
}

func ShopCheckoutCancel(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	publicID := strings.TrimSpace(mux.Vars(r)["order"])
	order, err := getters.GetShopOrderByPublicID(ctx, publicID)
	if err != nil {
		http.Redirect(w, r, "/shop/cart?err="+url.QueryEscape("Payment canceled."), http.StatusSeeOther)
		return
	}
	switch order.Status {
	case types.ShopOrderStatusPending:
		if err := getters.CancelShopOrder(ctx, order.ID, "", "buyer cancelled payment checkout"); err != nil {
			ctx.Err.Printf("/shop/checkout/cancel/%s: %s", publicID, err)
			http.Redirect(w, r, "/shop/checkout?err="+url.QueryEscape("Payment could not be canceled. Please try again."), http.StatusSeeOther)
			return
		}
	case types.ShopOrderStatusCancelled:
		// A repeated cancellation is safe and should still restore the cart.
	default:
		ctx.Session.Remove(r.Context(), shopActiveCheckoutSessionKey)
		http.Redirect(w, r, "/shop/cart?err="+url.QueryEscape("This payment can no longer be canceled."), http.StatusSeeOther)
		return
	}
	lines := restoreShopOrderCart(readShopCart(ctx, r), order)
	if len(lines) > 0 {
		saveShopCart(ctx, r, lines)
	}
	ctx.Session.Remove(r.Context(), shopActiveCheckoutSessionKey)
	http.Redirect(w, r, "/shop/cart?err="+url.QueryEscape("Payment canceled. Your cart has been restored."), http.StatusSeeOther)
}

func ShopSuccess(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	orderID := strings.TrimSpace(mux.Vars(r)["order"])
	order, err := getters.GetShopOrderByPublicID(ctx, orderID)
	if err != nil {
		ctx.Err.Printf("/shop/success/%s: %s", orderID, err)
		http.NotFound(w, r)
		return
	}
	// The order snapshot is authoritative after checkout. Remove only the
	// quantities purchased so items added in another tab are preserved.
	saveShopCart(ctx, r, removeShopOrderFromCart(readShopCart(ctx, r), order))
	ctx.Session.Remove(r.Context(), shopActiveCheckoutSessionKey)
	page := baseShopPage(ctx, r, "order received")
	page.Order = order
	for _, shipment := range order.Shipments {
		if shipment != nil && shipment.Provider == types.ShippingProviderEasyship && shipment.Status != "cancelled" {
			page.EasyshipShipment = shipment
			break
		}
	}
	page.EasyshipRateQuote, _ = getters.GetLatestEasyshipRateQuote(ctx, order.ID)
	page.CanCreateEasyshipShipment = canManageEasyshipShipment(order, page.EasyshipRateQuote)
	renderShopTemplate(w, r, ctx, "shop/success.tmpl", page)
}

func sendShopReceiptEmail(ctx *config.AppContext, order *types.ShopOrder) error {
	if order == nil {
		return fmt.Errorf("order is required")
	}
	if strings.TrimSpace(order.BuyerEmail) == "" {
		return fmt.Errorf("order has no buyer email")
	}
	var html bytes.Buffer
	if err := ctx.TemplateCache.ExecuteTemplate(&html, "shop/receipt.tmpl", &DashboardPage{
		Name:      order.BuyerEmail,
		ShopOrder: order,
		BaseURI:   ctx.Env.GetURI(),
		Year:      helpers.CurrentYear(),
	}); err != nil {
		return fmt.Errorf("render receipt email: %w", err)
	}
	title := "Your bitcoin++ receipt"
	if order.CheckoutKind == types.ShopCheckoutKindMixed {
		title = "Your bitcoin++ ticket and merch receipt"
	}
	if order.CheckoutKind == types.ShopCheckoutKindMerch {
		title = "Your bitcoin++ merch receipt"
	}
	return emails.ComposeAndSendMail(ctx, &emails.Mail{
		JobKey:   "shop-receipt:" + order.PublicID,
		Email:    order.BuyerEmail,
		Title:    title,
		SendAt:   time.Now(),
		HTMLBody: html.Bytes(),
		TextBody: []byte(fmt.Sprintf("Receipt %s total %s", order.PublicID, merchMoney(order.TotalCents, nil))),
	})
}

func finalizeShopTaxTransaction(ctx *config.AppContext, orderID string) error {
	recorded, err := getters.HasRecordedShopTaxTransaction(ctx, orderID)
	if err != nil || recorded {
		return err
	}
	calculationID, err := getters.GetShopTaxCalculationID(ctx, orderID)
	if err != nil || calculationID == "" {
		return err
	}
	params := &stripe.TaxTransactionCreateFromCalculationParams{
		Calculation: stripe.String(calculationID),
		Reference:   stripe.String("shop-order-" + orderID),
	}
	params.SetIdempotencyKey("shop-tax-transaction:" + orderID)
	transaction, err := stripeTaxTransaction.CreateFromCalculation(params)
	if err != nil {
		return fmt.Errorf("finalize Stripe Tax calculation: %w", err)
	}
	raw, err := json.Marshal(transaction)
	if err != nil {
		return fmt.Errorf("encode Stripe Tax transaction: %w", err)
	}
	order, err := getters.GetShopOrderByID(ctx, orderID)
	if err != nil {
		return err
	}
	return getters.RecordShopTaxTransaction(ctx, orderID, transaction.ID, order.SalesTaxAmountCents, string(raw))
}

func AdminMerch(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	if id := requireGlobalAdmin(w, r, ctx); id == nil {
		return
	}
	products, err := getters.ListMerchProducts(ctx, true)
	if err != nil {
		ctx.Err.Printf("/admin/merch products: %s", err)
		http.Error(w, "Unable to load merch admin", http.StatusInternalServerError)
		return
	}
	page := baseShopPage(ctx, r, "merch admin")
	page.Products = products
	page.Admin = true
	stats, err := getters.GetShopOperationalStats(ctx)
	if err != nil {
		ctx.Err.Printf("/admin/merch operational stats: %s", err)
	} else {
		page.ShopStats = stats
	}
	renderShopTemplate(w, r, ctx, "admin/merch.tmpl", page)
}

func AdminMerchNew(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	if id := requireGlobalAdmin(w, r, ctx); id == nil {
		return
	}
	page := baseShopPage(ctx, r, "new merch item")
	page.Admin = true
	page.Product = &types.MerchProduct{
		Status:           types.MerchProductStatusDraft,
		ProductType:      "apparel",
		Currency:         "USD",
		Symbol:           "$",
		RequiresShipping: true,
		AllowEventPickup: true,
	}
	renderShopTemplate(w, r, ctx, "admin/merch_new.tmpl", page)
}

func AdminMerchProduct(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	if id := requireGlobalAdmin(w, r, ctx); id == nil {
		return
	}
	productID := strings.TrimSpace(mux.Vars(r)["id"])
	product, err := getters.GetMerchProductByID(ctx, productID)
	if err != nil {
		ctx.Err.Printf("/admin/merch/%s: %s", productID, err)
		http.NotFound(w, r)
		return
	}
	page := baseShopPage(ctx, r, "edit merch item")
	page.Admin = true
	page.Product = product
	page.SpacesReady = spaces.IsConfigured()
	renderShopTemplate(w, r, ctx, "admin/merch_edit.tmpl", page)
}

func AdminMerchCreate(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	id := requireGlobalAdmin(w, r, ctx)
	if id == nil {
		return
	}
	limitRequestBody(w, r, maxFormBodyBytes)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	input := merchProductInputFromForm(r)
	productID, err := getters.CreateMerchProduct(ctx, input)
	if err != nil {
		http.Redirect(w, r, "/admin/merch/new?err="+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	}
	variantID, err := getters.CreateMerchVariant(ctx, merchVariantInputFromForm(r, productID))
	if err != nil {
		http.Redirect(w, r, "/admin/merch/new?err="+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	}
	if stock := parseIntForm(r.FormValue("stock"), 0); stock > 0 {
		_ = getters.AdjustMerchInventory(ctx, variantID, "initial", stock, id.Email, "initial admin stock")
	}
	http.Redirect(w, r, "/admin/merch?flash="+url.QueryEscape("Product created."), http.StatusSeeOther)
}

func AdminMerchUpdate(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	if requireGlobalAdmin(w, r, ctx) == nil {
		return
	}
	productID := strings.TrimSpace(mux.Vars(r)["id"])
	limitRequestBody(w, r, maxFormBodyBytes)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	if err := getters.UpdateMerchProduct(ctx, productID, merchProductInputFromForm(r)); err != nil {
		http.Redirect(w, r, adminMerchProductURL(productID, "err", err.Error()), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, adminMerchProductURL(productID, "flash", "Product updated."), http.StatusSeeOther)
}

func AdminMerchUploadImage(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	if id := requireGlobalAdmin(w, r, ctx); id == nil {
		return
	}
	productID := strings.TrimSpace(mux.Vars(r)["id"])
	limitRequestBody(w, r, maxMultipartBodyBytes)
	raw, contentType, ext, err := readMultipartImageFile(r, "file", false)
	if err != nil {
		http.Error(w, "missing or unreadable file", http.StatusBadRequest)
		return
	}
	if !spaces.IsConfigured() {
		http.Error(w, "spaces not configured", http.StatusInternalServerError)
		return
	}
	shortID := imgproc.ShortID(raw)
	key := "merch/" + shortID + ext
	if !spaces.Exists(key) {
		if _, err := spaces.Upload(key, raw, contentType, ""); err != nil {
			ctx.Err.Printf("/admin/merch/%s/upload-image: %s", productID, err)
			http.Error(w, "upload failed", http.StatusInternalServerError)
			return
		}
	}
	altText := strings.TrimSpace(r.FormValue("alt_text"))
	displayOrder := parseIntForm(r.FormValue("display_order"), 0)
	primary := r.FormValue("primary") == "on"
	if _, err := getters.AddMerchProductImage(ctx, productID, key, altText, displayOrder, primary); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"url": spaces.PublicURL(key), "key": key})
}

func AdminMerchVariantCreate(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	id := requireGlobalAdmin(w, r, ctx)
	if id == nil {
		return
	}
	productID := strings.TrimSpace(mux.Vars(r)["id"])
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	variantID, err := getters.CreateMerchVariant(ctx, merchVariantInputFromForm(r, productID))
	if err == nil {
		if stock := parseIntForm(r.FormValue("stock"), 0); stock > 0 {
			err = getters.AdjustMerchInventory(ctx, variantID, "initial", stock, id.Email, "initial admin stock")
		}
	}
	adminMerchRedirect(w, r, err, "Variant created.")
}

func AdminMerchVariantUpdate(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	id := requireGlobalAdmin(w, r, ctx)
	if id == nil {
		return
	}
	productID := strings.TrimSpace(mux.Vars(r)["id"])
	variantID := strings.TrimSpace(mux.Vars(r)["variant"])
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	variant, product, err := getters.GetMerchVariant(ctx, variantID)
	if err == nil && (variant == nil || product == nil || product.ID != productID) {
		err = fmt.Errorf("variant does not belong to product")
	}
	if err == nil {
		err = getters.UpdateMerchVariant(ctx, variantID, merchVariantInputFromForm(r, productID))
	}
	if err == nil {
		if delta := parseIntForm(r.FormValue("stock_delta"), 0); delta != 0 {
			err = getters.AdjustMerchInventory(ctx, variantID, "adjustment", delta, id.Email, "admin adjustment")
		}
	}
	adminMerchRedirect(w, r, err, "Variant updated.")
}

func AdminMerchImageUpdate(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	if requireGlobalAdmin(w, r, ctx) == nil {
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	productID := strings.TrimSpace(mux.Vars(r)["id"])
	imageID := strings.TrimSpace(mux.Vars(r)["image"])
	var err error
	if r.FormValue("delete") == "1" {
		err = getters.DeleteMerchProductImage(ctx, imageID)
	} else {
		err = getters.UpdateMerchProductImage(ctx, productID, imageID, r.FormValue("alt_text"), parseIntForm(r.FormValue("display_order"), 0), r.FormValue("primary") == "on")
	}
	adminMerchRedirect(w, r, err, "Image updated.")
}

func AdminMerchOptionSave(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	if requireGlobalAdmin(w, r, ctx) == nil {
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	productID := strings.TrimSpace(mux.Vars(r)["id"])
	optionID := strings.TrimSpace(mux.Vars(r)["option"])
	var err error
	if r.FormValue("delete") == "1" {
		err = getters.DeleteMerchProductOption(ctx, productID, optionID)
	} else {
		values := strings.FieldsFunc(r.FormValue("values"), func(r rune) bool { return r == ',' || r == '\n' })
		err = getters.SaveMerchProductOption(ctx, productID, optionID, r.FormValue("name"), parseIntForm(r.FormValue("display_order"), 0), r.FormValue("required") == "on", values)
	}
	adminMerchRedirect(w, r, err, "Product option updated.")
}

func adminMerchRedirect(w http.ResponseWriter, r *http.Request, err error, success string) {
	productID := strings.TrimSpace(mux.Vars(r)["id"])
	if err != nil {
		http.Redirect(w, r, adminMerchProductURL(productID, "err", err.Error()), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, adminMerchProductURL(productID, "flash", success), http.StatusSeeOther)
}

func adminMerchProductURL(productID, key, message string) string {
	path := "/admin/merch"
	if productID = strings.TrimSpace(productID); productID != "" {
		path += "/" + url.PathEscape(productID)
	}
	if key != "" && message != "" {
		path += "?" + url.QueryEscape(key) + "=" + url.QueryEscape(message)
	}
	return path
}

func AdminMerchOrders(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	if id := requireGlobalAdmin(w, r, ctx); id == nil {
		return
	}
	orders, err := getters.ListShopOrders(ctx, 200)
	if err != nil {
		ctx.Err.Printf("/admin/merch/orders: %s", err)
		http.Error(w, "Unable to load merch orders", http.StatusInternalServerError)
		return
	}
	page := baseShopPage(ctx, r, "merch orders")
	page.Admin = true
	page.OrderView = strings.TrimSpace(r.URL.Query().Get("view"))
	if page.OrderView != "needs_shipping" && page.OrderView != "event_pickup" {
		page.OrderView = "all"
	}
	page.Orders, page.OrderCount, page.NeedsShippingCount, page.EventPickupOrderCount = filterShopOrdersForAdmin(orders, page.OrderView)
	renderShopTemplate(w, r, ctx, "admin/merch_orders.tmpl", page)
}

func filterShopOrdersForAdmin(orders []*types.ShopOrder, view string) ([]*types.ShopOrder, uint, uint, uint) {
	filtered := make([]*types.ShopOrder, 0, len(orders))
	var total, needsShipping, eventPickup uint
	for _, order := range orders {
		if order == nil {
			continue
		}
		total++
		if order.UnfulfilledShippingQuantity > 0 {
			needsShipping++
		}
		if order.EventPickupQuantity > 0 {
			eventPickup++
		}
		if (view != "needs_shipping" || order.UnfulfilledShippingQuantity > 0) &&
			(view != "event_pickup" || order.EventPickupQuantity > 0) {
			filtered = append(filtered, order)
		}
	}
	return filtered, total, needsShipping, eventPickup
}

func AdminMerchOrder(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	if id := requireGlobalAdmin(w, r, ctx); id == nil {
		return
	}
	publicID := strings.TrimSpace(mux.Vars(r)["order"])
	order, err := getters.GetShopOrderByPublicID(ctx, publicID)
	if err != nil {
		ctx.Err.Printf("/admin/merch/orders/%s: %s", publicID, err)
		http.NotFound(w, r)
		return
	}
	page := baseShopPage(ctx, r, "merch order")
	page.Admin = true
	page.Order = order
	for _, shipment := range order.Shipments {
		if shipment != nil && shipment.Provider == types.ShippingProviderEasyship && shipment.Status != "cancelled" {
			page.EasyshipShipment = shipment
			break
		}
	}
	page.EasyshipRateQuote, _ = getters.GetLatestEasyshipRateQuote(ctx, order.ID)
	page.CanCreateEasyshipShipment = canPrepareEasyshipShipmentOrder(order)
	page.ManualRefund = !shopOrderUsesAutomatedStripeRefund(order)
	if page.ManualRefund {
		page.RefundContact, err = getters.GetShopRefundContactByEmail(ctx, order.BuyerEmail)
		if err != nil {
			ctx.Err.Printf("/admin/merch/orders/%s refund contact: %s", publicID, err)
			page.RefundContact = &types.ShopRefundContact{}
		}
	}
	renderShopTemplate(w, r, ctx, "admin/merch_order.tmpl", page)
}

func canManageEasyshipShipment(order *types.ShopOrder, quote *types.ShippingRateQuote) bool {
	if order == nil || quote == nil || order.UnfulfilledShippingQuantity == 0 {
		return false
	}
	return order.Status == types.ShopOrderStatusPaid || order.Status == types.ShopOrderStatusPartiallyRefunded
}

func canPrepareEasyshipShipmentOrder(order *types.ShopOrder) bool {
	if order == nil || order.ShippingAddress == nil || order.UnfulfilledShippingQuantity == 0 {
		return false
	}
	return order.Status == types.ShopOrderStatusPaid || order.Status == types.ShopOrderStatusPartiallyRefunded
}

func shopOrderUsesAutomatedStripeRefund(order *types.ShopOrder) bool {
	return order != nil && strings.EqualFold(strings.TrimSpace(order.PaymentProvider), "stripe") && strings.TrimSpace(order.PaymentProviderID) != ""
}

func AdminMerchOrderAction(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	id := requireGlobalAdmin(w, r, ctx)
	if id == nil {
		return
	}
	publicID := strings.TrimSpace(mux.Vars(r)["order"])
	action := strings.TrimSpace(mux.Vars(r)["action"])
	order, err := getters.GetShopOrderByPublicID(ctx, publicID)
	if err != nil {
		ctx.Err.Printf("/admin/merch/orders/%s/%s: %s", publicID, action, err)
		http.NotFound(w, r)
		return
	}
	limitRequestBody(w, r, maxFormBodyBytes)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	var actionErr error
	switch action {
	case "notes":
		actionErr = getters.UpdateShopOrderAdminNotes(ctx, order.ID, id.Email, r.FormValue("admin_notes"))
	case "cancel":
		actionErr = getters.CancelShopOrder(ctx, order.ID, id.Email, r.FormValue("notes"))
	case "ship":
		actionErr = getters.MarkShopOrderShipped(ctx, order.ID, id.Email, r.FormValue("courier_name"), r.FormValue("tracking_number"), r.FormValue("tracking_url"), r.FormValue("notes"))
	case "easyship-create":
		actionErr = updateEasyshipDestinationPhone(ctx, order, r.FormValue("contact_phone"))
		if actionErr == nil {
			actionErr = adminCreateEasyshipShipment(r.Context(), ctx, order, id.Email)
		}
	case "easyship-rate":
		actionErr = adminRefreshEasyshipRate(r.Context(), ctx, order)
	case "easyship-label":
		actionErr = adminCreateEasyshipLabel(r.Context(), ctx, order, id.Email)
	case "pickup":
		actionErr = getters.MarkShopOrderItemPickedUp(ctx, r.FormValue("item_id"), id.Email, "admin order detail pickup")
	case "receipt":
		actionErr = sendShopReceiptEmail(ctx, order)
	case "refund":
		actionErr = adminRefundShopOrder(ctx, order, id.Email, r)
	default:
		http.NotFound(w, r)
		return
	}
	if actionErr != nil {
		ctx.Err.Printf("/admin/merch/orders/%s/%s: %s", publicID, action, actionErr)
		http.Redirect(w, r, "/admin/merch/orders/"+url.PathEscape(publicID)+"?err="+url.QueryEscape(actionErr.Error()), http.StatusSeeOther)
		return
	}
	if action == "ship" || action == "refund" {
		updated, loadErr := getters.GetShopOrderByID(ctx, order.ID)
		if loadErr != nil {
			ctx.Err.Printf("/admin/merch/orders/%s/%s notification reload: %s", publicID, action, loadErr)
		} else if mailErr := sendShopStatusEmail(ctx, updated, action); mailErr != nil {
			// The operational action has already committed. Log notification
			// failure instead of encouraging an admin to repeat it.
			ctx.Err.Printf("/admin/merch/orders/%s/%s notification: %s", publicID, action, mailErr)
		}
	}
	http.Redirect(w, r, "/admin/merch/orders/"+url.PathEscape(publicID)+"?flash="+url.QueryEscape("Order updated."), http.StatusSeeOther)
}

func updateEasyshipDestinationPhone(ctx *config.AppContext, order *types.ShopOrder, submitted string) error {
	if order == nil || order.ShippingAddress == nil {
		return fmt.Errorf("order has no shipping address")
	}
	phone := strings.TrimSpace(submitted)
	if phone == "" {
		phone = strings.TrimSpace(order.ShippingAddress.Phone)
	}
	if phone == "" {
		return fmt.Errorf("Easyship requires a destination phone number")
	}
	if phone == order.ShippingAddress.Phone {
		return nil
	}
	order.ShippingAddress.Phone = phone
	return getters.UpsertShopOrderShippingAddress(ctx, order.ID, order.ShippingAddress)
}

func adminRefreshEasyshipRate(requestContext context.Context, ctx *config.AppContext, order *types.ShopOrder) error {
	if order == nil || order.ShippingAddress == nil {
		return fmt.Errorf("order has no shipping address")
	}
	settings, err := getters.GetEasyshipSettings(ctx)
	if err != nil {
		return err
	}
	if !settings.IsConfigured() {
		return fmt.Errorf("Easyship origin is not configured")
	}
	parcelItems, err := getters.ListEasyshipOrderParcelItems(ctx, order.ID)
	if err != nil {
		return err
	}
	items := make([]easyship.Item, 0, len(parcelItems))
	for _, item := range parcelItems {
		items = append(items, easyship.Item{
			SKU: item.SKU, Name: item.Description, Quantity: item.Quantity,
			ValueCents: item.ValueCents, WeightGrams: item.WeightGrams,
			LengthMM: item.LengthMM, WidthMM: item.WidthMM, HeightMM: item.HeightMM,
			HSCode: item.HSCode, Category: item.Category, OriginCountry: item.OriginCountry,
		})
	}
	origin := easyship.Address{
		ContactName: settings.ContactName, CompanyName: settings.CompanyName,
		Email: settings.Email, Phone: settings.Phone, Country: settings.CountryAlpha2,
		Region: settings.Region, PostalCode: settings.PostalCode, City: settings.City,
		Line1: settings.Line1, Line2: settings.Line2,
	}
	dest := easyship.Address{
		ContactName: order.ShippingAddress.Name, Email: order.BuyerEmail,
		Phone: order.ShippingAddress.Phone, Country: order.ShippingAddress.Country,
		Region: order.ShippingAddress.Region, PostalCode: order.ShippingAddress.PostalCode,
		City: order.ShippingAddress.City, Line1: order.ShippingAddress.Line1,
		Line2: order.ShippingAddress.Line2,
	}
	rates, err := easyship.Rates(requestContext, ctx.Env.Easyship, origin, dest, items)
	if err != nil {
		return err
	}
	// Easyship rates are sorted cheapest first. Save that service as the
	// operational replacement for a development fallback checkout quote.
	rate := rates[0]
	expiresAt := time.Now().Add(shopShippingRatesTTL)
	return getters.CreateShippingRateQuote(ctx, getters.ShippingRateQuoteInput{
		OrderID: order.ID, Provider: types.ShippingProviderEasyship,
		ProviderQuoteID:    rate.ProviderQuoteID,
		DestinationCountry: dest.Country, DestinationRegion: dest.Region,
		DestinationPostalCode: dest.PostalCode, CourierName: rate.CourierName,
		ServiceName: rate.ServiceName, AmountCents: rate.AmountCents,
		Currency: rate.Currency, EstimatedMinDays: rate.MinDays,
		EstimatedMaxDays: rate.MaxDays, RawResponse: string(rate.Raw), ExpiresAt: &expiresAt,
	})
}

func adminCreateEasyshipShipment(requestContext context.Context, ctx *config.AppContext, order *types.ShopOrder, actorEmail string) error {
	if order == nil || order.ShippingAddress == nil {
		return fmt.Errorf("order has no shipping address")
	}
	quote, err := getters.GetLatestEasyshipRateQuote(ctx, order.ID)
	if err != nil {
		return err
	}
	settings, err := getters.GetEasyshipSettings(ctx)
	if err != nil {
		return err
	}
	if !settings.IsConfigured() {
		return fmt.Errorf("Easyship origin is not configured")
	}
	shipment, err := getters.PrepareEasyshipShipment(ctx, order.ID, actorEmail, quote)
	if err != nil {
		return err
	}
	if shipment.ProviderShipmentID != "" {
		return nil
	}
	parcelItems, err := getters.ListEasyshipShipmentParcelItems(ctx, shipment.ID)
	if err != nil {
		return err
	}
	items := make([]easyship.Item, 0, len(parcelItems))
	for _, item := range parcelItems {
		items = append(items, easyship.Item{
			SKU: item.SKU, Name: item.Description, Quantity: item.Quantity,
			ValueCents: item.ValueCents, WeightGrams: item.WeightGrams,
			LengthMM: item.LengthMM, WidthMM: item.WidthMM, HeightMM: item.HeightMM,
			HSCode: item.HSCode, Category: item.Category, OriginCountry: item.OriginCountry,
		})
	}
	origin := easyship.Address{
		ContactName: settings.ContactName, CompanyName: settings.CompanyName,
		Email: settings.Email, Phone: settings.Phone, Country: settings.CountryAlpha2,
		Region: settings.Region, PostalCode: settings.PostalCode, City: settings.City,
		Line1: settings.Line1, Line2: settings.Line2,
	}
	dest := easyship.Address{
		ContactName: order.ShippingAddress.Name, Email: order.BuyerEmail,
		Phone: order.ShippingAddress.Phone, Country: order.ShippingAddress.Country,
		Region: order.ShippingAddress.Region, PostalCode: order.ShippingAddress.PostalCode,
		City: order.ShippingAddress.City, Line1: order.ShippingAddress.Line1,
		Line2: order.ShippingAddress.Line2,
	}
	result, err := easyship.CreateShipment(requestContext, ctx.Env.Easyship, origin, dest, items,
		quote.CourierServiceID, order.PublicID, shipment.CreateIdempotencyKey)
	if err != nil {
		_ = getters.RecordEasyshipShipmentError(ctx, shipment.ID, err.Error())
		return err
	}
	update := &types.Shipment{
		ProviderShipmentID: result.EasyshipShipmentID, CourierServiceID: result.CourierServiceID,
		CourierName: result.CourierName, ServiceName: result.ServiceName,
		ProviderLabelID: result.LabelID, LabelURL: result.LabelURL, LabelState: result.LabelState,
		TrackingNumber: result.TrackingNumber, TrackingURL: result.TrackingURL,
	}
	if err := getters.CompleteEasyshipShipmentCreation(ctx, shipment.ID, update, result.Raw, actorEmail); err != nil {
		return fmt.Errorf("save Easyship shipment: %w", err)
	}
	return nil
}

func adminCreateEasyshipLabel(requestContext context.Context, ctx *config.AppContext, order *types.ShopOrder, actorEmail string) error {
	if order == nil {
		return fmt.Errorf("order is required")
	}
	var shipment *types.Shipment
	for _, candidate := range order.Shipments {
		if candidate != nil && candidate.Provider == types.ShippingProviderEasyship && candidate.Status != "cancelled" {
			shipment = candidate
			break
		}
	}
	if shipment == nil || shipment.ProviderShipmentID == "" {
		return fmt.Errorf("create the Easyship shipment before purchasing its label")
	}
	if shipment.LabelState == "generated" && shipment.LabelURL != "" {
		return nil
	}
	result, err := easyship.CreateLabel(requestContext, ctx.Env.Easyship, shipment.ProviderShipmentID,
		shipment.CourierServiceID, shipment.LabelIdempotencyKey)
	if err != nil {
		_ = getters.RecordEasyshipShipmentError(ctx, shipment.ID, err.Error())
		return err
	}
	update := &types.Shipment{
		ProviderLabelID: result.LabelID, LabelURL: result.LabelURL, LabelState: result.LabelState,
		TrackingNumber: result.TrackingNumber, TrackingURL: result.TrackingURL,
	}
	if err := getters.CompleteEasyshipLabelCreation(ctx, shipment.ID, update, result.Raw, actorEmail); err != nil {
		return fmt.Errorf("save Easyship label: %w", err)
	}
	return nil
}

func adminRefundShopOrder(ctx *config.AppContext, order *types.ShopOrder, actorEmail string, r *http.Request) error {
	if order == nil {
		return fmt.Errorf("order is required")
	}
	itemID := strings.TrimSpace(r.FormValue("item_id"))
	quantity := uint(parseIntForm(r.FormValue("quantity"), 0))
	amountCents := uint(parseIntForm(r.FormValue("amount_cents"), 0))
	reason := strings.TrimSpace(r.FormValue("reason"))
	restock := r.FormValue("restock") == "on"
	if itemID == "" || quantity == 0 || amountCents == 0 {
		return fmt.Errorf("item, quantity, and refund amount are required")
	}
	provider := strings.TrimSpace(order.PaymentProvider)
	providerRefundID := strings.TrimSpace(r.FormValue("provider_refund_id"))
	if shopOrderUsesAutomatedStripeRefund(order) {
		checkout, err := session.Get(order.PaymentProviderID, nil)
		if err != nil {
			return fmt.Errorf("load stripe checkout session: %w", err)
		}
		if checkout.PaymentIntent == nil || strings.TrimSpace(checkout.PaymentIntent.ID) == "" {
			return fmt.Errorf("stripe checkout has no payment intent")
		}
		params := &stripe.RefundParams{
			Amount:        stripe.Int64(int64(amountCents)),
			PaymentIntent: stripe.String(checkout.PaymentIntent.ID),
			Reason:        stripe.String(string(stripe.RefundReasonRequestedByCustomer)),
		}
		refundedBefore := uint(0)
		for _, item := range order.Items {
			if item != nil && item.ID == itemID {
				refundedBefore = item.RefundedQuantity
				break
			}
		}
		params.SetIdempotencyKey(fmt.Sprintf("shop-refund:%s:%s:%d:%d:%d:%t", order.ID, itemID, refundedBefore, quantity, amountCents, restock))
		refund, err := stripeRefund.New(params)
		if err != nil {
			return fmt.Errorf("stripe refund: %w", err)
		}
		providerRefundID = refund.ID
	} else if provider == "" {
		provider = "manual"
	}
	return getters.RecordShopRefund(ctx, order.ID, itemID, actorEmail, provider, providerRefundID, reason, quantity, amountCents, restock)
}

func sendShopStatusEmail(ctx *config.AppContext, order *types.ShopOrder, action string) error {
	if order == nil || strings.TrimSpace(order.BuyerEmail) == "" {
		return fmt.Errorf("order with buyer email is required")
	}
	var title, textBody, htmlBody, jobSuffix string
	switch action {
	case "ship":
		if len(order.Shipments) == 0 {
			return fmt.Errorf("order has no shipment")
		}
		shipment := order.Shipments[len(order.Shipments)-1]
		title = "Your bitcoin++ order has shipped"
		textBody = fmt.Sprintf("Order %s has shipped via %s. Tracking: %s %s", order.PublicID, shipment.CourierName, shipment.TrackingNumber, shipment.TrackingURL)
		htmlBody = fmt.Sprintf("<p>Your bitcoin++ order <strong>%s</strong> has shipped.</p><p>%s<br><a href=\"%s\">%s</a></p>",
			template.HTMLEscapeString(order.PublicID), template.HTMLEscapeString(shipment.CourierName),
			template.HTMLEscapeString(shipment.TrackingURL), template.HTMLEscapeString(shipment.TrackingNumber))
		jobSuffix = shipment.ID
	case "refund":
		title = "Your bitcoin++ order refund"
		textBody = fmt.Sprintf("A refund was recorded for order %s. Current order status: %s.", order.PublicID, order.Status)
		htmlBody = fmt.Sprintf("<p>A refund was recorded for bitcoin++ order <strong>%s</strong>.</p><p>Current status: %s</p>",
			template.HTMLEscapeString(order.PublicID), template.HTMLEscapeString(order.Status))
		for _, item := range order.Items {
			if item != nil {
				jobSuffix += fmt.Sprintf(":%s-%d", item.ID, item.RefundedQuantity)
			}
		}
	default:
		return fmt.Errorf("unsupported shop notification %q", action)
	}
	return emails.ComposeAndSendMail(ctx, &emails.Mail{
		JobKey:   "shop-" + action + ":" + order.PublicID + ":" + jobSuffix,
		Email:    order.BuyerEmail,
		Title:    title,
		SendAt:   time.Now(),
		HTMLBody: []byte(htmlBody),
		TextBody: []byte(textBody),
	})
}

func renderShopTemplate(w http.ResponseWriter, r *http.Request, ctx *config.AppContext, tmpl string, page *shopPage) {
	if err := ctx.TemplateCache.ExecuteTemplate(w, tmpl, page); err != nil {
		ctx.Err.Printf("%s render %s: %s", r.URL.Path, tmpl, err)
		http.Error(w, "Unable to load page", http.StatusInternalServerError)
	}
}

func baseShopPage(ctx *config.AppContext, r *http.Request, title string) *shopPage {
	page := &shopPage{
		Title:      title,
		Year:       int(helpers.CurrentYear()),
		Flash:      strings.TrimSpace(r.URL.Query().Get("flash")),
		Error:      strings.TrimSpace(r.URL.Query().Get("err")),
		PickupConf: nextShopPickupConf(ctx),
		Email:      strings.TrimSpace(ctx.Session.GetString(r.Context(), auth.SessionEmailKey)),
	}
	cart, _ := loadShopCart(ctx, r)
	page.CartCount = uint(len(cart))
	for _, item := range cart {
		page.CartCount += item.Qty - 1
	}
	return page
}

func defaultShopCheckoutDetails(email string) *shopCheckoutDetails {
	return &shopCheckoutDetails{
		Email:         strings.ToLower(strings.TrimSpace(email)),
		Fulfillment:   types.ShopFulfillmentShip,
		Country:       "US",
		PaymentMethod: "btc",
	}
}

func shopCheckoutDetailsFromRequest(r *http.Request, fallbackEmail string) *shopCheckoutDetails {
	details := defaultShopCheckoutDetails(fallbackEmail)
	details.Name = strings.TrimSpace(r.FormValue("name"))
	details.Email = strings.ToLower(strings.TrimSpace(r.FormValue("email")))
	details.Fulfillment = strings.TrimSpace(r.FormValue("fulfillment"))
	details.Address1 = strings.TrimSpace(r.FormValue("address1"))
	details.Address2 = strings.TrimSpace(r.FormValue("address2"))
	details.City = strings.TrimSpace(r.FormValue("city"))
	details.Region = strings.TrimSpace(r.FormValue("region"))
	details.PostalCode = strings.TrimSpace(r.FormValue("postal_code"))
	details.Country = strings.ToUpper(strings.TrimSpace(r.FormValue("country")))
	details.Phone = strings.TrimSpace(r.FormValue("phone"))
	details.PaymentMethod = strings.TrimSpace(r.FormValue("payment_method"))
	details.ShippingRateID = strings.TrimSpace(r.FormValue("shipping_rate_id"))
	details.ShippingRateAmountCents = parseUintForm(r.FormValue("shipping_rate_amount_cents"), 0)
	details.PickupConferenceID = strings.TrimSpace(r.FormValue("pickup_conf_id"))
	if details.Fulfillment == "" {
		details.Fulfillment = types.ShopFulfillmentShip
	}
	if details.Country == "" {
		details.Country = "US"
	}
	if details.PaymentMethod == "" {
		details.PaymentMethod = "btc"
	}
	return details
}

func loadShopCart(ctx *config.AppContext, r *http.Request) ([]*shopCartItem, error) {
	lines := readShopCart(ctx, r)
	var out []*shopCartItem
	for _, line := range lines {
		if line.Qty == 0 {
			continue
		}
		variant, product, err := getters.GetMerchVariant(ctx, line.VariantID)
		if err != nil {
			return out, err
		}
		unit := merchVariantPrice(product, variant)
		out = append(out, &shopCartItem{
			Product:        product,
			Variant:        variant,
			Qty:            line.Qty,
			UnitPriceCents: unit,
			LineTotalCents: unit * line.Qty,
		})
	}
	return out, nil
}

func readShopCart(ctx *config.AppContext, r *http.Request) []shopCartLine {
	raw := ctx.Session.GetString(r.Context(), shopCartSessionKey)
	if raw == "" {
		return nil
	}
	var lines []shopCartLine
	if err := json.Unmarshal([]byte(raw), &lines); err != nil {
		return nil
	}
	return lines
}

func saveShopCart(ctx *config.AppContext, r *http.Request, lines []shopCartLine) {
	// Shipping quotes are tied to the exact parcel set. Any cart mutation,
	// including restoring a cancelled checkout, invalidates the previous set.
	ctx.Session.Remove(r.Context(), shopShippingRatesSessionKey)
	if len(lines) == 0 {
		ctx.Session.Remove(r.Context(), shopCartSessionKey)
		return
	}
	raw, _ := json.Marshal(lines)
	ctx.Session.Put(r.Context(), shopCartSessionKey, string(raw))
}

func restoreShopOrderCart(lines []shopCartLine, order *types.ShopOrder) []shopCartLine {
	required := shopOrderVariantQuantities(order)
	out := append([]shopCartLine(nil), lines...)
	for i := range out {
		if quantity := required[out[i].VariantID]; quantity > 0 {
			if out[i].Qty < quantity {
				out[i].Qty = quantity
			}
			delete(required, out[i].VariantID)
		}
	}
	for variantID, quantity := range required {
		out = append(out, shopCartLine{VariantID: variantID, Qty: quantity})
	}
	return out
}

func removeShopOrderFromCart(lines []shopCartLine, order *types.ShopOrder) []shopCartLine {
	purchased := shopOrderVariantQuantities(order)
	out := make([]shopCartLine, 0, len(lines))
	for _, line := range lines {
		quantity := purchased[line.VariantID]
		if quantity >= line.Qty {
			continue
		}
		line.Qty -= quantity
		out = append(out, line)
	}
	return out
}

func shopOrderVariantQuantities(order *types.ShopOrder) map[string]uint {
	quantities := make(map[string]uint)
	if order == nil {
		return quantities
	}
	for _, item := range order.Items {
		if item != nil && strings.TrimSpace(item.VariantID) != "" && item.Quantity > 0 {
			quantities[item.VariantID] += item.Quantity
		}
	}
	return quantities
}

func normalizedShopFulfillment(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "pickup" || raw == types.ShopFulfillmentEventPickup {
		return types.ShopFulfillmentEventPickup
	}
	return types.ShopFulfillmentShip
}

func shopTaxAddress(ctx *config.AppContext, r *http.Request, fulfillment string, pickupConf *types.Conf) (*types.ShopAddress, error) {
	if fulfillment == types.ShopFulfillmentEventPickup {
		if pickupConf == nil {
			return nil, fmt.Errorf("Event pickup is not currently available.")
		}
		address := &types.ShopAddress{
			Name:       pickupConf.Venue,
			Line1:      strings.TrimSpace(pickupConf.PickupAddressLine1),
			Line2:      strings.TrimSpace(pickupConf.PickupAddressLine2),
			City:       strings.TrimSpace(pickupConf.PickupAddressCity),
			Region:     strings.TrimSpace(pickupConf.PickupAddressRegion),
			PostalCode: strings.TrimSpace(pickupConf.PickupAddressPostalCode),
			Country:    strings.ToUpper(strings.TrimSpace(pickupConf.PickupAddressCountry)),
		}
		if address.Line1 == "" || address.City == "" || address.PostalCode == "" || address.Country == "" {
			ctx.Err.Printf("event pickup tax address is incomplete for %s", pickupConf.Tag)
			return nil, fmt.Errorf("Tax configuration for event pickup is incomplete. Please choose shipping or contact support.")
		}
		return address, nil
	}
	address := &types.ShopAddress{
		Name:       strings.TrimSpace(r.FormValue("name")),
		Line1:      strings.TrimSpace(r.FormValue("address1")),
		Line2:      strings.TrimSpace(r.FormValue("address2")),
		City:       strings.TrimSpace(r.FormValue("city")),
		Region:     strings.TrimSpace(r.FormValue("region")),
		PostalCode: strings.TrimSpace(r.FormValue("postal_code")),
		Country:    strings.ToUpper(strings.TrimSpace(r.FormValue("country"))),
		Phone:      strings.TrimSpace(r.FormValue("phone")),
	}
	if address.Line1 == "" || address.City == "" || address.PostalCode == "" || address.Country == "" {
		return nil, fmt.Errorf("A complete shipping address is required to calculate tax.")
	}
	return address, nil
}

func shopCartSubtotal(cart []*shopCartItem) uint {
	var subtotal uint
	for _, item := range cart {
		if item != nil {
			subtotal += item.LineTotalCents
		}
	}
	return subtotal
}

func pendingShopCheckout(ctx *config.AppContext, r *http.Request, email string) *types.ShopOrder {
	publicID := strings.TrimSpace(ctx.Session.GetString(r.Context(), shopActiveCheckoutSessionKey))
	var order *types.ShopOrder
	var err error
	if publicID != "" {
		order, err = getters.GetShopOrderByPublicID(ctx, publicID)
	}
	if (order == nil || err != nil) && strings.TrimSpace(email) != "" {
		order, err = getters.GetLatestPendingShopOrderByEmail(ctx, email)
		if err == nil && order != nil {
			ctx.Session.Put(r.Context(), shopActiveCheckoutSessionKey, order.PublicID)
		}
	}
	if err != nil || order == nil || order.Status != types.ShopOrderStatusPending {
		if publicID != "" {
			ctx.Session.Remove(r.Context(), shopActiveCheckoutSessionKey)
		}
		return nil
	}
	return order
}

func fillCartTotals(page *shopPage) {
	subtotalCents := shopCartSubtotal(page.Cart)
	page.SubtotalCents = subtotalCents
	if subtotalCents == 0 {
		page.TotalCents = 0
		return
	}
	if subtotalCents >= 7500 {
		page.ShippingCents = 0
	} else {
		page.ShippingCents = shopFlatRateShippingCents
	}
	// Tax is calculated after the customer supplies a destination, before either
	// payment provider is initialized.
	page.TaxCents = 0
	page.TotalCents = subtotalCents + page.ShippingCents + page.TaxCents
}

func shopFallbackShippingCents(cart []*shopCartItem) uint {
	var subtotal uint
	for _, item := range cart {
		if item != nil {
			subtotal += item.LineTotalCents
		}
	}
	if subtotal == 0 || subtotal >= 7500 {
		return 0
	}
	return shopFlatRateShippingCents
}

func merchVariantAvailable(variant *types.MerchVariant, qty uint) bool {
	if variant == nil || strings.TrimSpace(variant.Status) != "active" {
		return false
	}
	switch variant.InventoryPolicy {
	case types.MerchInventoryPolicyUnlimited, types.MerchInventoryPolicyAllowBackorder:
		return true
	default:
		if qty == 0 {
			qty = 1
		}
		return variant.Stock >= int(qty)
	}
}

func merchProductSoldOut(product *types.MerchProduct) bool {
	if product == nil || len(product.Variants) == 0 {
		return true
	}
	for _, variant := range product.Variants {
		if merchVariantAvailable(variant, 1) {
			return false
		}
	}
	return true
}

func validateShopCartInventory(cart []*shopCartItem) error {
	for _, item := range cart {
		if item == nil || item.Product == nil || item.Variant == nil {
			return fmt.Errorf("One of those items is no longer available.")
		}
		if !merchVariantAvailable(item.Variant, item.Qty) {
			if item.Variant.Stock <= 0 {
				return fmt.Errorf("%s is sold out.", item.Product.Name)
			}
			return fmt.Errorf("Only %d of %s are left in stock.", item.Variant.Stock, item.Product.Name)
		}
	}
	return nil
}

func shopShippingQuote(ctx *config.AppContext, r *http.Request, cart []*shopCartItem, fulfillment string, selectedRateID string, selectedAmountCents uint) (*getters.ShippingRateQuoteInput, error) {
	if fulfillment != types.ShopFulfillmentShip {
		return nil, nil
	}
	rateSet, ok := loadShopShippingRateSet(ctx, r, cart)
	if !ok {
		return nil, errShopShippingRatesExpired
	}
	return shopShippingQuoteFromRateSet(rateSet, selectedRateID, selectedAmountCents)
}

func shopShippingQuoteFromRateSet(rateSet *shopShippingRateSet, selectedRateID string, selectedAmountCents uint) (*getters.ShippingRateQuoteInput, error) {
	if rateSet == nil || len(rateSet.Rates) == 0 {
		return nil, errShopShippingRatesExpired
	}
	selectedRateID = strings.TrimSpace(selectedRateID)
	if selectedRateID == "" {
		return nil, fmt.Errorf("Select a shipping service before creating the order.")
	}
	var selected *easyship.Rate
	for i := range rateSet.Rates {
		if rateSet.Rates[i].ProviderQuoteID == selectedRateID {
			selected = &rateSet.Rates[i]
			break
		}
	}
	if selected == nil {
		return nil, errShopShippingRatesExpired
	}
	if selected.AmountCents != selectedAmountCents {
		return nil, fmt.Errorf("The price for that shipping service changed. Please review the current options.")
	}
	provider := types.ShippingProviderEasyship
	if selected.ProviderQuoteID == "flat-rate" {
		provider = "fallback"
	}
	return &getters.ShippingRateQuoteInput{
		Provider:              provider,
		ProviderQuoteID:       selected.ProviderQuoteID,
		DestinationCountry:    rateSet.Destination.Country,
		DestinationRegion:     rateSet.Destination.Region,
		DestinationPostalCode: rateSet.Destination.PostalCode,
		CourierName:           selected.CourierName,
		ServiceName:           selected.ServiceName,
		AmountCents:           selected.AmountCents,
		Currency:              firstNonEmpty(selected.Currency, "USD"),
		EstimatedMinDays:      selected.MinDays,
		EstimatedMaxDays:      selected.MaxDays,
		RawResponse:           string(selected.Raw),
	}, nil
}

func saveShopShippingRateSet(ctx *config.AppContext, r *http.Request, cart []*shopCartItem, dest easyship.Address, rates []easyship.Rate) error {
	set := &shopShippingRateSet{
		AddressKey:  shopShippingAddressKey(r),
		CartKey:     shopShippingCartKey(cart),
		Destination: dest,
		Rates:       rates,
		ExpiresAt:   time.Now().Add(shopShippingRatesTTL),
	}
	raw, err := json.Marshal(set)
	if err != nil {
		return err
	}
	ctx.Session.Put(r.Context(), shopShippingRatesSessionKey, string(raw))
	return nil
}

func loadShopShippingRateSet(ctx *config.AppContext, r *http.Request, cart []*shopCartItem) (*shopShippingRateSet, bool) {
	raw := ctx.Session.GetString(r.Context(), shopShippingRatesSessionKey)
	if raw == "" {
		return nil, false
	}
	var set shopShippingRateSet
	if err := json.Unmarshal([]byte(raw), &set); err != nil || time.Now().After(set.ExpiresAt) ||
		set.AddressKey != shopShippingAddressKey(r) || set.CartKey != shopShippingCartKey(cart) {
		ctx.Session.Remove(r.Context(), shopShippingRatesSessionKey)
		return nil, false
	}
	return &set, true
}

func shopShippingAddressKey(r *http.Request) string {
	return strings.Join([]string{
		strings.ToUpper(strings.TrimSpace(r.FormValue("country"))),
		strings.ToUpper(strings.TrimSpace(r.FormValue("region"))),
		strings.ToUpper(strings.TrimSpace(r.FormValue("postal_code"))),
		strings.ToLower(strings.TrimSpace(r.FormValue("city"))),
		strings.ToLower(strings.TrimSpace(r.FormValue("address1"))),
		strings.ToLower(strings.TrimSpace(r.FormValue("address2"))),
	}, "|")
}

func shopShippingCartKey(cart []*shopCartItem) string {
	lines := make([]string, 0, len(cart))
	for _, item := range cart {
		if item == nil || item.Variant == nil {
			continue
		}
		lines = append(lines, fmt.Sprintf("%s:%d:%d:%d:%d:%d:%d", item.Variant.ID, item.Qty,
			item.UnitPriceCents, item.Variant.WeightGrams, item.Variant.LengthMM,
			item.Variant.WidthMM, item.Variant.HeightMM))
	}
	sort.Strings(lines)
	return strings.Join(lines, "|")
}

func shopAvailableShippingRates(ctx *config.AppContext, r *http.Request, cart []*shopCartItem, fulfillment string, fallbackCents uint) ([]easyship.Rate, easyship.Address, error) {
	if fulfillment != types.ShopFulfillmentShip {
		return nil, easyship.Address{}, nil
	}
	dest := easyship.Address{
		Country:    firstNonEmpty(strings.TrimSpace(r.FormValue("country")), "US"),
		Region:     strings.TrimSpace(r.FormValue("region")),
		PostalCode: strings.TrimSpace(r.FormValue("postal_code")),
		City:       strings.TrimSpace(r.FormValue("city")),
		Line1:      strings.TrimSpace(r.FormValue("address1")),
		Line2:      strings.TrimSpace(r.FormValue("address2")),
	}
	if dest.Line1 == "" || dest.City == "" || dest.PostalCode == "" || dest.Country == "" {
		return nil, dest, fmt.Errorf("A complete shipping address is required to calculate shipping.")
	}
	items := make([]easyship.Item, 0, len(cart))
	for _, item := range cart {
		if item == nil || item.Product == nil || item.Variant == nil || !item.Product.RequiresShipping {
			continue
		}
		if err := validateShopVariantParcel(item.Product, item.Variant); err != nil {
			ctx.Err.Printf("/shop/checkout invalid parcel configuration: %s", err)
			return nil, dest, err
		}
		items = append(items, easyship.Item{
			SKU:      item.Variant.SKU,
			Name:     item.Product.Name,
			Quantity: item.Qty,
			// Easyship's declared customs value is the value of one unit. Quantity
			// is sent separately, so using the line total would multiply it twice.
			ValueCents:    item.UnitPriceCents,
			WeightGrams:   item.Variant.WeightGrams,
			LengthMM:      item.Variant.LengthMM,
			WidthMM:       item.Variant.WidthMM,
			HeightMM:      item.Variant.HeightMM,
			HSCode:        item.Product.HSCode,
			Category:      item.Product.EasyshipCategory,
			OriginCountry: item.Product.CountryOfOrigin,
		})
	}
	if len(items) == 0 {
		return nil, dest, nil
	}
	settings, err := getters.GetEasyshipSettings(ctx)
	if err != nil {
		ctx.Err.Printf("/shop/checkout easyship origin load failed: %s", err)
		return nil, dest, fmt.Errorf("Shipping configuration could not be loaded. Please try again.")
	}
	if !settings.IsConfigured() {
		err = fmt.Errorf("easyship fulfillment origin is not configured in global admin")
		if ctx.InProduction {
			ctx.Err.Printf("/shop/checkout: %s", err)
			return nil, dest, fmt.Errorf("Shipping is temporarily unavailable. Please try again later.")
		}
		ctx.Err.Printf("/shop/checkout easyship development fallback: %s", err)
		return []easyship.Rate{fallbackShippingRate(fallbackCents, err.Error())}, dest, nil
	}
	origin := easyship.Address{
		ContactName: settings.ContactName,
		CompanyName: settings.CompanyName,
		Email:       settings.Email,
		Phone:       settings.Phone,
		Country:     settings.CountryAlpha2,
		Region:      settings.Region,
		PostalCode:  settings.PostalCode,
		City:        settings.City,
		Line1:       settings.Line1,
		Line2:       settings.Line2,
	}
	rates, err := easyship.Rates(r.Context(), ctx.Env.Easyship, origin, dest, items)
	if err != nil {
		if ctx.InProduction {
			ctx.Err.Printf("/shop/checkout easyship quote failed: %s", err)
			return nil, dest, fmt.Errorf("Shipping could not be calculated for that address. Please verify it and try again.")
		}
		ctx.Err.Printf("/shop/checkout easyship development fallback: %s", err)
		return []easyship.Rate{fallbackShippingRate(fallbackCents, err.Error())}, dest, nil
	}
	usdRates := make([]easyship.Rate, 0, len(rates))
	for _, rate := range rates {
		if strings.EqualFold(strings.TrimSpace(rate.Currency), "USD") {
			usdRates = append(usdRates, rate)
		}
	}
	if len(usdRates) == 0 {
		return nil, dest, fmt.Errorf("Shipping services were returned in a currency this checkout cannot charge.")
	}
	return usdRates, dest, nil
}

func validateShopVariantParcel(product *types.MerchProduct, variant *types.MerchVariant) error {
	if product == nil || variant == nil {
		return fmt.Errorf("One of the cart items has incomplete shipping configuration.")
	}
	if variant.WeightGrams <= 0 || variant.LengthMM <= 0 || variant.WidthMM <= 0 || variant.HeightMM <= 0 {
		return fmt.Errorf("%s does not have complete shipping weight and dimensions. Please update its variant in merch admin.", product.Name)
	}
	return nil
}

func shopStripeTaxQuote(cart []*shopCartItem, address *types.ShopAddress, shippingCents uint) (*getters.TaxQuoteInput, error) {
	params, err := shopStripeTaxParams(cart, address, shippingCents)
	if err != nil {
		return nil, err
	}
	calculation, err := stripeTaxCalculation.New(params)
	if err != nil {
		return nil, fmt.Errorf("create Stripe Tax calculation: %w", err)
	}
	raw, err := json.Marshal(calculation)
	if err != nil {
		return nil, fmt.Errorf("encode Stripe Tax calculation: %w", err)
	}
	if calculation.TaxAmountExclusive < 0 {
		return nil, fmt.Errorf("Stripe Tax returned a negative tax amount")
	}
	var expiresAt *time.Time
	if calculation.ExpiresAt > 0 {
		expires := time.Unix(calculation.ExpiresAt, 0)
		expiresAt = &expires
	}
	return &getters.TaxQuoteInput{
		Provider:              types.TaxProviderStripe,
		ProviderQuoteID:       calculation.ID,
		SalesTaxAmountCents:   uint(calculation.TaxAmountExclusive),
		DestinationCountry:    address.Country,
		DestinationRegion:     address.Region,
		DestinationPostalCode: address.PostalCode,
		RawResponse:           string(raw),
		ExpiresAt:             expiresAt,
	}, nil
}

func shopStripeTaxParams(cart []*shopCartItem, address *types.ShopAddress, shippingCents uint) (*stripe.TaxCalculationParams, error) {
	if address == nil || strings.TrimSpace(address.Line1) == "" || strings.TrimSpace(address.City) == "" || strings.TrimSpace(address.PostalCode) == "" || strings.TrimSpace(address.Country) == "" {
		return nil, fmt.Errorf("a complete shipping address is required to calculate tax")
	}
	params := &stripe.TaxCalculationParams{
		Currency: stripe.String("usd"),
		CustomerDetails: &stripe.TaxCalculationCustomerDetailsParams{
			Address: &stripe.AddressParams{
				Line1:      stripe.String(strings.TrimSpace(address.Line1)),
				Line2:      stripe.String(strings.TrimSpace(address.Line2)),
				City:       stripe.String(strings.TrimSpace(address.City)),
				State:      stripe.String(strings.TrimSpace(address.Region)),
				PostalCode: stripe.String(strings.TrimSpace(address.PostalCode)),
				Country:    stripe.String(strings.ToUpper(strings.TrimSpace(address.Country))),
			},
			AddressSource: stripe.String(string(stripe.TaxCalculationCustomerDetailsAddressSourceShipping)),
		},
	}
	for _, item := range cart {
		if item == nil || item.Product == nil || item.Variant == nil || item.Qty == 0 {
			continue
		}
		taxCode := firstNonEmpty(strings.TrimSpace(item.Product.StripeTaxCode), types.StripeTaxCodeTangibleGood)
		params.LineItems = append(params.LineItems, &stripe.TaxCalculationLineItemParams{
			Amount:      stripe.Int64(int64(item.LineTotalCents)),
			Quantity:    stripe.Int64(int64(item.Qty)),
			Reference:   stripe.String(item.Variant.ID),
			TaxBehavior: stripe.String("exclusive"),
			TaxCode:     stripe.String(taxCode),
		})
	}
	if len(params.LineItems) == 0 {
		return nil, fmt.Errorf("at least one taxable line item is required")
	}
	if shippingCents > 0 {
		params.ShippingCost = &stripe.TaxCalculationShippingCostParams{
			Amount:      stripe.Int64(int64(shippingCents)),
			TaxBehavior: stripe.String("exclusive"),
			TaxCode:     stripe.String(types.StripeTaxCodeShipping),
		}
	}
	return params, nil
}

func fallbackShippingRate(amountCents uint, reason string) easyship.Rate {
	raw, _ := json.Marshal(map[string]any{
		"fallback": true,
		"reason":   reason,
	})
	return easyship.Rate{
		ProviderQuoteID: "flat-rate",
		CourierName:     "Standard shipping",
		ServiceName:     "Development fallback",
		AmountCents:     amountCents,
		Currency:        "USD",
		Raw:             raw,
	}
}

func merchProductInputFromForm(r *http.Request) getters.MerchProductInput {
	status := strings.TrimSpace(r.FormValue("status"))
	if status == "" {
		status = types.MerchProductStatusDraft
	}
	return getters.MerchProductInput{
		Tag:              r.FormValue("tag"),
		Slug:             r.FormValue("slug"),
		Name:             r.FormValue("name"),
		Subtitle:         r.FormValue("subtitle"),
		Description:      r.FormValue("description"),
		Status:           status,
		ProductType:      r.FormValue("product_type"),
		BasePriceCents:   parseUintForm(r.FormValue("base_price_cents"), 0),
		Currency:         firstNonEmpty(strings.TrimSpace(r.FormValue("currency")), "USD"),
		Symbol:           firstNonEmpty(strings.TrimSpace(r.FormValue("symbol")), "$"),
		StripeTaxCode:    r.FormValue("stripe_tax_code"),
		EasyshipCategory: r.FormValue("easyship_category"),
		HSCode:           r.FormValue("hs_code"),
		CountryOfOrigin:  r.FormValue("country_of_origin"),
		RequiresShipping: r.FormValue("requires_shipping") != "",
		AllowEventPickup: r.FormValue("allow_event_pickup") != "",
	}
}

func merchVariantInputFromForm(r *http.Request, productID string) getters.MerchVariantInput {
	sku := strings.TrimSpace(r.FormValue("sku"))
	if sku == "" {
		sku = strings.ToUpper(strings.ReplaceAll(strings.TrimSpace(r.FormValue("tag")), "-", "_"))
	}
	label := strings.TrimSpace(r.FormValue("variant_label"))
	if label == "" {
		label = "Default"
	}
	return getters.MerchVariantInput{
		ProductID:       productID,
		SKU:             sku,
		Label:           label,
		PriceDeltaCents: parseIntForm(r.FormValue("price_delta_cents"), 0),
		WeightGrams:     parseIntForm(r.FormValue("weight_grams"), 0),
		LengthMM:        merchDimensionMMFromForm(r, "length"),
		WidthMM:         merchDimensionMMFromForm(r, "width"),
		HeightMM:        merchDimensionMMFromForm(r, "height"),
		InventoryPolicy: firstNonEmpty(strings.TrimSpace(r.FormValue("inventory_policy")), types.MerchInventoryPolicyDeny),
		Status:          firstNonEmpty(strings.TrimSpace(r.FormValue("variant_status")), "active"),
	}
}

func nextShopPickupConf(ctx *config.AppContext) *types.Conf {
	return nextShopPickupConfAt(ctx, time.Now())
}

func nextShopPickupConfAt(ctx *config.AppContext, now time.Time) *types.Conf {
	confs, err := getters.ListConfs(ctx)
	if err != nil {
		return nil
	}
	var upcoming []*types.Conf
	for _, conf := range confs {
		if shopEventPickupOpenAt(conf, now) {
			upcoming = append(upcoming, conf)
		}
	}
	sort.Slice(upcoming, func(i, j int) bool {
		return upcoming[i].StartDate.Before(upcoming[j].StartDate)
	})
	if len(upcoming) == 0 {
		return nil
	}
	return upcoming[0]
}

func shopEventPickupOpenAt(conf *types.Conf, now time.Time) bool {
	if conf == nil || !conf.IsPublished() || conf.StartDate.IsZero() {
		return false
	}
	loc := conf.Loc()
	cutoff := conf.StartDate.In(loc).AddDate(0, 0, -shopEventPickupCloseDays)
	return now.In(loc).Before(cutoff)
}

func validateShopPickupSelection(conf *types.Conf, selectedConfID string) error {
	if conf == nil {
		return fmt.Errorf("Event pickup closes seven days before the event. Please choose shipping.")
	}
	if strings.TrimSpace(selectedConfID) == "" || strings.TrimSpace(selectedConfID) != conf.Ref {
		return fmt.Errorf("That event pickup option is no longer available. Please review the current delivery options.")
	}
	return nil
}

func shopCategories(products []*types.MerchProduct) []shopCategory {
	labels := map[string]string{
		"apparel":     "Apparel",
		"accessories": "Accessories",
		"stickers":    "Stickers & patches",
		"pins":        "Pins",
		"exclusive":   "Attendee exclusives",
		"standard":    "Merch",
	}
	tones := map[string]int{"apparel": 28, "accessories": 190, "stickers": 145, "pins": 275, "exclusive": 45, "standard": 28}
	counts := map[string]int{}
	for _, p := range products {
		counts[shopCategorySlug(p.ProductType)]++
	}
	var out []shopCategory
	for slug, count := range counts {
		out = append(out, shopCategory{Slug: slug, Label: firstNonEmpty(labels[slug], slug), Count: count, Tone: tones[slug]})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Slug < out[j].Slug })
	return out
}

func ticketCheckoutAddOnProducts(ctx *config.AppContext, conf *types.Conf) []*types.MerchProduct {
	if !shopEventPickupOpenAt(conf, time.Now()) {
		return nil
	}
	products, err := getters.ListConferenceMerchUpsells(ctx, conf.Ref)
	if err != nil {
		return nil
	}
	out := make([]*types.MerchProduct, 0, 3)
	for _, product := range products {
		if product.AllowEventPickup && len(product.Variants) > 0 && !merchProductSoldOut(product) {
			out = append(out, product)
		}
		if len(out) == 3 {
			break
		}
	}
	return out
}

func selectedTicketAddOns(ctx *config.AppContext, conf *types.Conf, r *http.Request) ([]*shopCartItem, uint) {
	products := ticketCheckoutAddOnProducts(ctx, conf)
	var out []*shopCartItem
	var totalCents uint
	for _, product := range products {
		if product == nil || len(product.Variants) == 0 {
			continue
		}
		variant := product.Variants[0]
		qty := parseUintForm(r.FormValue("addon_"+variant.ID), 0)
		if qty == 0 {
			continue
		}
		if qty > 4 {
			qty = 4
		}
		if !merchVariantAvailable(variant, qty) {
			continue
		}
		unit := merchVariantPrice(product, variant)
		lineTotal := unit * qty
		out = append(out, &shopCartItem{
			Product:        product,
			Variant:        variant,
			Qty:            qty,
			UnitPriceCents: unit,
			LineTotalCents: lineTotal,
		})
		totalCents += lineTotal
	}
	return out, totalCents
}

func createTicketAddOnOrder(ctx *config.AppContext, conf *types.Conf, tix *types.ConfTicket, form *types.TixForm, ticketKind string, paymentMethod string, addOns []*shopCartItem, addOnTotalCents, salesTaxCents uint) (*types.ShopOrder, error) {
	if len(addOns) == 0 {
		return nil, nil
	}
	if !shopEventPickupOpenAt(conf, time.Now()) {
		return nil, fmt.Errorf("event pickup closes seven days before the event")
	}
	ticketUnitCents := form.DiscountPrice * 100
	if paymentMethod == "card" {
		ticketUnitCents = cardSurchargePrice(form.DiscountPrice, tix.CardSurchargeBPS) * 100
	}
	items := []getters.ShopOrderItemInput{{
		Quantity:             form.Count,
		UnitPriceCents:       ticketUnitCents,
		LineTotalCents:       ticketUnitCents * form.Count,
		ProductTagSnapshot:   "ticket",
		ProductNameSnapshot:  conf.Desc,
		VariantLabelSnapshot: ticketKind,
		SKUSnapshot:          tix.ID,
		FulfillmentMethod:    types.ShopFulfillmentPOSTakeaway,
		SaleConferenceID:     conf.Ref,
		Status:               types.ShopItemStatusPending,
	}}
	for _, item := range addOns {
		items = append(items, getters.ShopOrderItemInput{
			ProductID:            item.Product.ID,
			VariantID:            item.Variant.ID,
			Quantity:             item.Qty,
			UnitPriceCents:       item.UnitPriceCents,
			LineTotalCents:       item.LineTotalCents,
			ProductTagSnapshot:   item.Product.Tag,
			ProductNameSnapshot:  item.Product.Name,
			VariantLabelSnapshot: item.Variant.Label,
			SKUSnapshot:          item.Variant.SKU,
			FulfillmentMethod:    types.ShopFulfillmentEventPickup,
			SaleConferenceID:     conf.Ref,
			PickupConferenceID:   conf.Ref,
			Status:               types.ShopItemStatusPending,
		})
	}
	return getters.CreateShopOrder(ctx, getters.ShopOrderInput{
		BuyerEmail:          form.Email,
		BuyerName:           form.Email,
		Source:              types.ShopOrderSourceOnline,
		CheckoutKind:        types.ShopCheckoutKindMixed,
		PaymentProvider:     paymentMethod,
		Currency:            firstNonEmpty(tix.Currency, "USD"),
		SubtotalCents:       ticketUnitCents*form.Count + addOnTotalCents,
		SalesTaxAmountCents: salesTaxCents,
		TotalCents:          ticketUnitCents*form.Count + addOnTotalCents + salesTaxCents,
	}, items)
}

func shopCategorySlug(productType string) string {
	s := strings.ToLower(strings.TrimSpace(productType))
	if s == "" {
		return "standard"
	}
	return strings.ReplaceAll(s, " ", "-")
}

func merchVariantPrice(product *types.MerchProduct, variant *types.MerchVariant) uint {
	if product == nil {
		return 0
	}
	base := int(product.BasePriceCents)
	if variant != nil {
		base += variant.PriceDeltaCents
	}
	if base < 0 {
		return 0
	}
	return uint(base)
}

func merchPrice(product *types.MerchProduct) uint {
	if product == nil {
		return 0
	}
	if len(product.Variants) == 0 {
		return product.BasePriceCents
	}
	return merchVariantPrice(product, product.Variants[0])
}

func merchPrimaryImage(product *types.MerchProduct) string {
	if product == nil || len(product.Images) == 0 {
		return ""
	}
	key := strings.TrimSpace(product.Images[0].ObjectKey)
	if strings.HasPrefix(key, "/") || strings.HasPrefix(key, "http://") || strings.HasPrefix(key, "https://") {
		return key
	}
	return spaces.PublicURL(key)
}

func merchStaticImage(product *types.MerchProduct) string {
	if product == nil {
		return ""
	}
	switch product.Tag {
	case "core-hat":
		return "/static/img/merch/core-hat.avif"
	case "libbit-hat":
		return "/static/img/merch/libbit-hat.avif"
	case "bpp-hat", "librerelay-hat":
		return "/static/img/merch/librerelay-hat.avif"
	default:
		return ""
	}
}

func merchImage(product *types.MerchProduct) string {
	if img := merchPrimaryImage(product); img != "" {
		return img
	}
	return merchStaticImage(product)
}

func merchProductStock(product *types.MerchProduct) int {
	if product == nil {
		return 0
	}
	stock := 0
	for _, variant := range product.Variants {
		if variant != nil {
			stock += variant.Stock
		}
	}
	return stock
}

func shopOrderItemImage(item *types.ShopOrderItem) string {
	if item == nil {
		return ""
	}
	key := strings.TrimSpace(item.ImageObjectKey)
	if key == "" {
		if confTag := strings.TrimSpace(item.SaleConferenceTag); confTag != "" {
			return confImagePath(confTag, "leading")
		}
		return ""
	}
	if strings.HasPrefix(key, "/") || strings.HasPrefix(key, "http://") || strings.HasPrefix(key, "https://") {
		return key
	}
	return spaces.PublicURL(key)
}

func shopFulfillmentLabel(method string) string {
	switch method {
	case types.ShopFulfillmentShip:
		return "Ship to buyer"
	case types.ShopFulfillmentEventPickup:
		return "Event pickup"
	case types.ShopFulfillmentPOSTakeaway:
		return "Point of sale"
	default:
		return firstNonEmpty(method, "Unknown")
	}
}

func shopOrderHasFulfillment(order *types.ShopOrder, method string) bool {
	if order == nil {
		return false
	}
	for _, item := range order.Items {
		if item != nil && item.FulfillmentMethod == method {
			return true
		}
	}
	return false
}

func shopOrderFulfillmentSummary(order *types.ShopOrder) string {
	hasShip := shopOrderHasFulfillment(order, types.ShopFulfillmentShip)
	hasPickup := shopOrderHasFulfillment(order, types.ShopFulfillmentEventPickup)
	hasPOS := shopOrderHasFulfillment(order, types.ShopFulfillmentPOSTakeaway)
	switch {
	case hasShip && hasPickup:
		return "Mixed: ship some items, event pickup for others"
	case hasShip:
		return "Ship to buyer"
	case hasPickup:
		return "Event pickup"
	case hasPOS:
		return "Point of sale takeaway"
	default:
		return "No fulfillment items"
	}
}

func merchMoney(amount uint, product *types.MerchProduct) string {
	symbol := "$"
	post := ""
	if product != nil {
		symbol = firstNonEmpty(product.Symbol, "$")
		post = product.PostSymbol
	}
	whole := amount / 100
	cents := amount % 100
	if cents == 0 {
		return fmt.Sprintf("%s%d%s", symbol, whole, post)
	}
	return fmt.Sprintf("%s%d.%02d%s", symbol, whole, cents, post)
}

func merchInches(mm int) string {
	if mm <= 0 {
		return "0"
	}
	inches := math.Round(float64(mm)/25.4*100) / 100
	return strconv.FormatFloat(inches, 'f', -1, 64)
}

func merchDimensionMMFromForm(r *http.Request, dimension string) int {
	rawInches := strings.TrimSpace(r.FormValue(dimension + "_inches"))
	if rawInches == "" {
		// Accept the old field name for compatibility with existing clients.
		return parseIntForm(r.FormValue(dimension+"_mm"), 0)
	}
	inches, err := strconv.ParseFloat(rawInches, 64)
	if err != nil || inches < 0 {
		return 0
	}
	return int(math.Round(inches * 25.4))
}

func merchSats(amount uint) string {
	if amount == 0 {
		return "0"
	}
	sats := int64(math.Round((float64(amount) / 100 / 107500) * 100000000))
	return groupSatsCommas(sats)
}

func merchJSON(v any) template.JS {
	raw, _ := json.Marshal(v)
	return template.JS(raw)
}

func parseUintForm(raw string, fallback uint) uint {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return fallback
	}
	n, err := strconv.ParseUint(raw, 10, 32)
	if err != nil {
		return fallback
	}
	return uint(n)
}

func parseIntForm(raw string, fallback int) int {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return fallback
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}
	return n
}
