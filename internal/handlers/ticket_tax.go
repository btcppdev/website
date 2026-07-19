package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"btcpp-web/external/getters"
	"btcpp-web/internal/config"
	"btcpp-web/internal/types"
	"github.com/gorilla/mux"
)

func TicketTaxQuote(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	limitRequestBody(w, r, maxFormBodyBytes)
	w.Header().Set("Content-Type", "application/json")
	if err := parseShopRequestForm(r); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Unable to read the selected add-ons."})
		return
	}
	tixSlug := strings.TrimSpace(mux.Vars(r)["tix"])
	conf, _, _, _, err := determineTixPrice(ctx, tixSlug)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	addOns, addOnTotalCents := selectedTicketAddOns(ctx, conf, r)
	if len(addOns) == 0 {
		_ = json.NewEncoder(w).Encode(map[string]any{"tax_cents": 0, "add_on_total_cents": 0})
		return
	}
	quote, err := ticketCheckoutTaxQuote(ctx, conf, addOns)
	if err != nil {
		ctx.Err.Printf("/tix/%s/tax-quote: %s", tixSlug, err)
		w.WriteHeader(http.StatusUnprocessableEntity)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Sales tax could not be calculated for event pickup."})
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]any{
		"tax_cents":          quote.SalesTaxAmountCents,
		"add_on_total_cents": addOnTotalCents,
	})
}

func ticketCheckoutTaxQuote(ctx *config.AppContext, conf *types.Conf, addOns []*shopCartItem) (*getters.TaxQuoteInput, error) {
	if conf == nil {
		return nil, fmt.Errorf("event is required to calculate pickup tax")
	}
	address, err := shopTaxAddress(ctx, nil, types.ShopFulfillmentEventPickup, conf)
	if err != nil {
		return nil, err
	}
	return shopStripeTaxQuote(addOns, address, 0)
}
