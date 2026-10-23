package handlers

import (
	"context"
	"fmt"
	"net/http"

	"btcpp-web/external/getters"
	"btcpp-web/internal/config"
	"btcpp-web/internal/expedition"
	"btcpp-web/internal/types"
)

type antarcticaBerth struct {
	ID, Name, Description, Price, Deposit string
	PriceCents                            int64
	PayInFull, PayInFullSavings           string
}

type antarcticaPage struct {
	StaffBerths, RemainingBerths int
	Merch                        []antarcticaMerch
	MerchSnapshot                bool
	CheckoutURL                  string
	Conf                         *types.Conf
	Preview                      bool
	Berths                       []antarcticaBerth
	Speakers                     []*types.Speaker
}

func antarcticaMoney(cents int64) string {
	dollars := cents / 100
	whole := fmt.Sprint(dollars)
	for i := len(whole) - 3; i > 0; i -= 3 {
		whole = whole[:i] + "," + whole[i:]
	}
	if cents%100 == 0 {
		return "$" + whole
	}
	return fmt.Sprintf("$%s.%02d", whole, cents%100)
}

func newAntarcticaPage(conf *types.Conf, preview bool) *antarcticaPage {
	page := &antarcticaPage{Conf: conf, Preview: preview, CheckoutURL: "?checkout=1", StaffBerths: expedition.DefaultStaffBerths, RemainingBerths: expedition.TotalBerths - expedition.DefaultStaffBerths}
	for _, b := range expedition.Fares() {
		page.Berths = append(page.Berths, antarcticaBerth{
			ID: b.ID, Name: b.Name, Description: b.Description, PriceCents: b.Cents,
			PayInFull:        antarcticaMoney(expedition.FullBitcoinCents(b.Cents)),
			PayInFullSavings: antarcticaMoney(b.Cents - expedition.FullBitcoinCents(b.Cents)),
			Price:            antarcticaMoney(b.Cents), Deposit: antarcticaMoney(b.Cents / 5),
		})
	}
	return page
}

func AdminAntarcticaPreview(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	if requireGlobalAdmin(w, r, ctx) == nil {
		return
	}
	if r.URL.Query().Get("checkout") == "1" {
		renderAntarcticaCheckout(w, ctx, newAntarcticaPage(nil, true))
		return
	}
	renderAntarcticaPage(w, ctx, newAntarcticaPage(nil, true))
}

func renderAntarcticaCheckout(w http.ResponseWriter, ctx *config.AppContext, page *antarcticaPage) {
	if ctx.DB != nil {
		a, err := (&expedition.Store{DB: ctx.DB}).Availability(context.Background())
		if err != nil {
			page.RemainingBerths = 0
			ctx.Err.Printf("Antarctica inventory: %s", err)
		} else {
			page.StaffBerths = a.Staff
			page.RemainingBerths = a.Remaining
		}
	}
	page.Merch, page.MerchSnapshot = antarcticaCheckoutMerch(ctx)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("X-Robots-Tag", "noindex, nofollow")
	if err := ctx.TemplateCache.ExecuteTemplate(w, "conf/antarctica-checkout.tmpl", page); err != nil {
		ctx.Err.Printf("Antarctica checkout preview: %s", err)
		http.Error(w, "Unable to load checkout preview", http.StatusInternalServerError)
	}
}

func renderAntarcticaConference(w http.ResponseWriter, ctx *config.AppContext, conf *types.Conf) {
	page := newAntarcticaPage(conf, false)
	// Profiles and conference participation remain backed by the existing site.
	if talks, err := getters.GetTalksFor(ctx, conf.Tag); err == nil {
		page.Speakers = acceptedSpeakersForConf(ctx, conf, talks)
	}
	renderAntarcticaPage(w, ctx, page)
}

func renderAntarcticaPage(w http.ResponseWriter, ctx *config.AppContext, page *antarcticaPage) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("X-Robots-Tag", "noindex, nofollow")
	if err := ctx.TemplateCache.ExecuteTemplate(w, "conf/antarctica.tmpl", page); err != nil {
		ctx.Err.Printf("Antarctica page: %s", err)
		http.Error(w, "Unable to load expedition page", http.StatusInternalServerError)
	}
}
