package handlers

import (
	"fmt"
	"net/http"

	"btcpp-web/external/getters"
	"btcpp-web/internal/config"
	"btcpp-web/internal/types"
)

type antarcticaBerth struct {
	ID, Name, Description, Price, Deposit, BitcoinContribution string
	PriceCents                                                 int64
	ReservationCost, HeldBitcoinValue                          string
}

type antarcticaPage struct {
	Conf     *types.Conf
	Preview  bool
	Berths   []antarcticaBerth
	Speakers []*types.Speaker
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
	page := &antarcticaPage{Conf: conf, Preview: preview}
	for _, b := range []struct {
		id, name, description string
		price                 int64
	}{
		{"shared", "Shared berth", "The shared-berth expedition fare.", 1642900},
		{"triple", "Three-person bunk", "Share three ways. Save $830 per person.", 1559900},
		{"quad", "Four-person bunk", "A little closer together. Save $2,000 per person.", 1442900},
	} {
		page.Berths = append(page.Berths, antarcticaBerth{
			ID: b.id, Name: b.name, Description: b.description, PriceCents: b.price,
			Price: antarcticaMoney(b.price), Deposit: antarcticaMoney(b.price / 5),
			BitcoinContribution: antarcticaMoney(b.price * 3 / 4),
			ReservationCost:     antarcticaMoney(b.price / 4), HeldBitcoinValue: antarcticaMoney(b.price / 2),
		})
	}
	return page
}

func AdminAntarcticaPreview(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	if requireGlobalAdmin(w, r, ctx) == nil {
		return
	}
	renderAntarcticaPage(w, ctx, newAntarcticaPage(nil, true))
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
