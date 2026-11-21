package handlers

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"btcpp-web/external/getters"
	"btcpp-web/internal/config"
	"btcpp-web/internal/types"
)

type checkoutTicketOption struct {
	Ticket      *types.ConfTicket
	Remaining   uint
	Maximum     uint
	Count       uint
	BTC         uint
	Card        uint
	Original    uint
	DiscountRef string
}

func (o checkoutTicketOption) BTCTotal() uint  { return o.BTC * o.Count }
func (o checkoutTicketOption) CardTotal() uint { return o.Card * o.Count }

type ticketCartPrice struct {
	Currency    string
	ID          string
	BTC         uint
	Card        uint
	Original    uint
	DiscountRef string
}
type ticketCartQuote struct {
	Conference string
	Discount   string
	Affiliate  string
	Offer      string
	Expires    int64
	Prices     []ticketCartPrice
}

func ticketCartHMAC(app *config.AppContext, raw string) string {
	mac := hmac.New(sha256.New, app.Env.HMACKey[:])
	mac.Write([]byte("ticket-cart-v1:" + raw))
	return hex.EncodeToString(mac.Sum(nil))
}

func ticketCartQuantities(values url.Values) (map[string]uint, error) {
	counts := map[string]uint{}
	var total uint
	for key, values := range values {
		if !strings.HasPrefix(key, "ticket_") {
			continue
		}
		if len(values) != 1 {
			return nil, fmt.Errorf("invalid ticket quantity")
		}
		n, err := strconv.ParseUint(values[0], 10, 8)
		if err != nil || n > 8 {
			return nil, fmt.Errorf("choose 0–8 tickets of each type")
		}
		if n > 0 {
			counts[strings.TrimPrefix(key, "ticket_")] = uint(n)
			total += uint(n)
		}
	}
	if total < 1 || total > 8 {
		return nil, fmt.Errorf("choose 1–8 tickets in total")
	}
	return counts, nil
}

// Every option carries its own price and discount policy. The signed quote
// covers unit prices; quantities remain editable and are validated on submit.
func populateTicketCart(app *config.AppContext, page *TixFormPage, inventory *getters.TicketAvailability, counts map[string]uint) error {
	if page.SponsorCheckout || strings.Contains(page.TixSlug, "+") {
		return nil
	}
	page.Cart = true
	page.TicketOptions = nil
	page.CartBTC, page.CartCard = 0, 0
	quote := ticketCartQuote{Conference: page.Conf.Ref, Discount: strings.TrimSpace(page.Discount), Affiliate: strings.TrimSpace(page.AffiliateCode), Offer: strings.TrimSpace(page.OfferCode), Expires: time.Now().Add(30 * time.Minute).Unix()}
	if counts == nil {
		counts = map[string]uint{page.Tix.ID: page.Count}
	}
	var count uint
	for _, ticket := range page.Conf.Tickets {
		remaining := inventory.Remaining(ticket, page.Conf.Tickets, time.Now())
		if ticket.Disabled || (remaining == 0 && counts[ticket.ID] == 0) {
			continue
		}
		if !strings.EqualFold(ticket.Currency, page.Tix.Currency) {
			continue
		}
		option := checkoutTicketOption{Ticket: ticket, Remaining: remaining, Maximum: min(remaining, 8), Count: counts[ticket.ID], Original: ticket.StandardPrice() * 100}
		option.BTC = option.Original
		if ticket.DiscountsAllowed() {
			page.CartHasDiscounts = true
			code, label, err := resolveTicketDiscount(app, page.Conf, ticket, page.Discount, page.AffiliateCode, page.OfferCode)
			if err != nil {
				page.Err = err.Error()
			} else if code != "" {
				price, discount, err := getters.CalcDiscount(app, page.Conf.Ref, code, ticket.StandardPrice(), 1)
				if err != nil {
					if page.Discount != "" || page.OfferCode != "" {
						page.Err = "This offer or discount code is not available."
					}
				} else {
					option.BTC = price * 100
					if discount != nil {
						option.DiscountRef = discount.Ref
					}
					page.DiscountLabel = label
				}
			}
		}
		option.Card = cardSurchargePrice(option.BTC/100, ticket.CardSurchargeBPS) * 100
		page.CartBTC += option.BTC * option.Count
		page.CartCard += option.Card * option.Count
		count += option.Count
		page.TicketOptions = append(page.TicketOptions, option)
		quote.Prices = append(quote.Prices, ticketCartPrice{Currency: ticket.Currency, ID: ticket.ID, BTC: option.BTC, Card: option.Card, Original: option.Original, DiscountRef: option.DiscountRef})
	}
	page.Count = count
	raw, err := json.Marshal(quote)
	if err != nil {
		return err
	}
	page.CartQuote = base64.RawURLEncoding.EncodeToString(raw)
	page.CartHMAC = ticketCartHMAC(app, page.CartQuote)
	return nil
}

func validateTicketCart(app *config.AppContext, conf *types.Conf, tix *types.ConfTicket, slug string, values url.Values, form *types.TixForm) ([]types.TicketCartLine, error) {
	counts, err := ticketCartQuantities(values)
	if err != nil {
		return nil, err
	}
	raw := values.Get("CartQuote")
	if !hmac.Equal([]byte(ticketCartHMAC(app, raw)), []byte(values.Get("CartHMAC"))) {
		return nil, fmt.Errorf("ticket prices changed; refresh checkout")
	}
	decoded, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return nil, fmt.Errorf("invalid ticket quote")
	}
	var quoted ticketCartQuote
	if err = json.Unmarshal(decoded, &quoted); err != nil {
		return nil, err
	}
	if quoted.Conference != conf.Ref || quoted.Expires < time.Now().Unix() || quoted.Discount != strings.TrimSpace(form.Discount) || quoted.Affiliate != strings.TrimSpace(form.AffiliateCode) || quoted.Offer != strings.TrimSpace(form.OfferCode) {
		return nil, fmt.Errorf("ticket prices changed; refresh checkout")
	}
	inventory, err := getters.TicketInventory(app, conf.Ref)
	if err != nil {
		return nil, err
	}
	page := &TixFormPage{Conf: conf, Tix: tix, TixSlug: slug, Discount: form.Discount, AffiliateCode: form.AffiliateCode, OfferCode: form.OfferCode}
	if err = populateTicketCart(app, page, inventory, counts); err != nil {
		return nil, err
	}
	if !page.Cart || page.Err != "" {
		return nil, fmt.Errorf("%s", firstNonEmpty(page.Err, "invalid ticket selection"))
	}
	var lines []types.TicketCartLine
	var matched uint
	for _, option := range page.TicketOptions {
		if option.Count == 0 {
			continue
		}
		price := ticketCartPrice{Currency: option.Ticket.Currency, ID: option.Ticket.ID, BTC: option.BTC, Card: option.Card, Original: option.Original, DiscountRef: option.DiscountRef}
		found := false
		for _, old := range quoted.Prices {
			if old == price {
				found = true
				break
			}
		}
		if !found {
			return nil, fmt.Errorf("ticket prices changed; refresh checkout")
		}
		if option.Count > option.Remaining {
			return nil, fmt.Errorf("only %d %s tickets remain", option.Remaining, option.Ticket.Tier)
		}
		unit := option.BTC
		if form.PaymentMethod == "card" {
			unit = option.Card
		}
		kind := option.Ticket.TicketKind
		if kind == "" {
			kind = types.TicketTypeGeneral
		}
		lines = append(lines, types.TicketCartLine{Ticket: option.Ticket, TicketID: option.Ticket.ID, Quantity: option.Count, UnitCents: unit, BaseCents: option.BTC, OriginalCents: option.Original, DiscountRef: option.DiscountRef, Title: option.Ticket.Tier, Kind: kind, Currency: option.Ticket.Currency})
		matched++
	}
	if int(matched) != len(counts) {
		return nil, fmt.Errorf("one of the selected tickets is unavailable for this event")
	}
	form.Count = page.Count
	return lines, nil
}

func handleTicketCartDiscount(w http.ResponseWriter, r *http.Request, app *config.AppContext, conf *types.Conf, tix *types.ConfTicket, slug string) {
	inventory, err := getters.TicketInventory(app, conf.Ref)
	if err != nil {
		http.Error(w, "Unable to load tickets", http.StatusServiceUnavailable)
		return
	}
	counts, _ := ticketCartQuantities(r.PostForm)
	if counts == nil {
		counts = map[string]uint{}
	}
	page := &TixFormPage{Conf: conf, Tix: tix, TixSlug: slug, Discount: r.Form.Get("Discount"), AffiliateCode: r.Form.Get("AffiliateCode"), OfferCode: r.Form.Get("OfferCode")}
	if err = populateTicketCart(app, page, inventory, counts); err != nil {
		http.Error(w, "Unable to price tickets", http.StatusServiceUnavailable)
		return
	}
	if err = renderCheckout(w, r, app, "tix_details.tmpl", page, conf); err != nil {
		app.Err.Printf("ticket cart quote: %s", err)
	}
}

// Keep discount usage and affiliate calculations scoped to the actual eligible
// lines, excluding student/local tickets and card processing surcharges.
func recordTicketCartDiscountUsage(app *config.AppContext, conf *types.Conf, entry *types.Entry) {
	if !entry.Cart || entry.AlreadyFulfilled {
		return
	}
	type group struct {
		items    []types.Item
		base     int64
		original uint
	}
	groups := map[string]*group{}
	for _, item := range entry.Items {
		if item.DiscountRef == "" {
			continue
		}
		key := fmt.Sprintf("%s:%d", item.DiscountRef, item.OriginalCents)
		g := groups[key]
		if g == nil {
			g = &group{original: item.OriginalCents}
			groups[key] = g
		}
		g.items = append(g.items, item)
		g.base += int64(item.BaseCents)
	}
	for _, g := range groups {
		ref := g.items[0].DiscountRef
		if err := getters.IncrementDiscountUses(app, ref, uint(len(g.items))); err != nil {
			app.Err.Printf("ticket cart discount usage: %s", err)
		}
		eligible := *entry
		eligible.Items = g.items
		eligible.Total = g.base
		eligible.DiscountRef = ref
		recordAffiliateUsageFromCheckout(app, conf, &eligible, strconv.FormatUint(uint64(g.original), 10))
	}
}
