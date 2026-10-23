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
	ID, Name, Description, Price, Deposit, FinalInstallment string
	PriceCents                                              int64
	PayInFull, PayInFullSavings                             string
}

const antarcticaBookingInformationUpdated = "October 24, 2026"

type antarcticaPage struct {
	DefaultTerms, DefaultPrivacy string
	TermsUpdated                 string
	BetaTrial                    bool
	CheckoutEnabled              bool
	StripeEnabled                bool
	CSRF, RequestKey             string
	Policy                       *getters.ServiceLegalPolicy
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
	page := &antarcticaPage{DefaultTerms: expedition.BookingTerms, DefaultPrivacy: expedition.BookingPrivacy, TermsUpdated: antarcticaBookingInformationUpdated, Conf: conf, Preview: preview, StripeEnabled: preview, CheckoutURL: "?checkout=1", StaffBerths: expedition.DefaultStaffBerths, RemainingBerths: expedition.TotalBerths - expedition.DefaultStaffBerths}
	if !preview {
		page.CheckoutURL = "/expeditions/antarctica/checkout"
	}
	for _, b := range expedition.Fares() {
		page.Berths = append(page.Berths, antarcticaBerth{
			ID: b.ID, Name: b.Name, Description: b.Description, PriceCents: b.Cents,
			PayInFull:        antarcticaMoney(expedition.FullBitcoinCents(b.Cents)),
			PayInFullSavings: antarcticaMoney(b.Cents - expedition.FullBitcoinCents(b.Cents)),
			Price:            antarcticaMoney(b.Cents), Deposit: antarcticaMoney(b.Cents / 5),
			FinalInstallment: antarcticaMoney(b.Cents/5 + b.Cents%5),
		})
	}
	return page
}

func AdminAntarcticaPreview(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	if requireGlobalAdmin(w, r, ctx) == nil {
		return
	}
	if r.URL.Query().Get("booking") == "1" {
		renderExpeditionBooking(w, ctx, sampleExpeditionBooking())
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
		policy, err := getters.CurrentServiceLegalPolicy(ctx, expedition.ProductCode)
		if err != nil {
			http.Error(w, "Unable to load booking terms", http.StatusServiceUnavailable)
			return
		}
		page.Policy = policy
	}
	if ctx.DB != nil {
		a, err := (&expedition.Store{DB: ctx.DB}).Availability(context.Background())
		if err != nil {
			ctx.Err.Printf("Antarctica inventory: %s", err)
			http.Error(w, "Unable to check berth availability. Please try again.", 503)
			return
		} else {
			page.StaffBerths = a.Staff
			page.RemainingBerths = a.Remaining
		}
	}
	if page.BetaTrial && page.RemainingBerths > 1 {
		page.RemainingBerths = 1
	}
	if !page.CheckoutEnabled {
		page.Merch, page.MerchSnapshot = antarcticaCheckoutMerch(ctx)
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("X-Robots-Tag", "noindex, nofollow")
	if err := ctx.TemplateCache.ExecuteTemplate(w, "conf/antarctica-checkout.tmpl", page); err != nil {
		ctx.Err.Printf("Antarctica checkout preview: %s", err)
		http.Error(w, "Unable to load checkout", http.StatusInternalServerError)
	}
}

// AntarcticaLanding is public by direct link without publishing a conference listing.
func AntarcticaLanding(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	if r.URL.Query().Get("checkout") == "1" {
		AntarcticaCheckout(w, r, ctx)
		return
	}
	page := newAntarcticaPage(nil, false)
	if ctx.DB != nil {
		a, err := (&expedition.Store{DB: ctx.DB}).Availability(r.Context())
		if err != nil {
			ctx.Err.Printf("Antarctica landing inventory: %v", err)
			http.Error(w, "Unable to check expedition availability. Please try again.", http.StatusServiceUnavailable)
			return
		}
		page.RemainingBerths, page.StaffBerths = a.Remaining, a.Staff
		if a.Remaining == 0 {
			page.CheckoutURL = "/expeditions/antarctica/waitlist"
		}
	}
	renderAntarcticaPage(w, ctx, page)
}

func renderAntarcticaConference(w http.ResponseWriter, ctx *config.AppContext, conf *types.Conf) {
	page := newAntarcticaPage(conf, false)
	if a, err := (&expedition.Store{DB: ctx.DB}).Availability(context.Background()); err == nil {
		page.RemainingBerths = a.Remaining
		page.StaffBerths = a.Staff
		if a.Remaining == 0 {
			page.CheckoutURL = "/expeditions/antarctica/waitlist"
		}
	} else {
		ctx.Err.Printf("Antarctica landing inventory: %v", err)
	}
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

func AntarcticaTerms(w http.ResponseWriter, r *http.Request, app *config.AppContext) {
	w.Header().Set("Cache-Control", "no-store")
	page := newAntarcticaPage(nil, false)
	if app.DB != nil {
		policy, err := getters.CurrentServiceLegalPolicy(app, expedition.ProductCode)
		if err != nil {
			http.Error(w, "Unable to load booking terms", 503)
			return
		}
		page.Policy = policy
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := app.TemplateCache.ExecuteTemplate(w, "conf/antarctica-terms.tmpl", page); err != nil {
		app.Err.Printf("expedition terms: %v", err)
	}
}
