package handlers

import (
	"net/http"
	"net/url"
	"strings"
	"time"

	"btcpp-web/external/getters"
	"btcpp-web/internal/config"
	"btcpp-web/internal/i18n"
	"btcpp-web/internal/types"
	"github.com/gorilla/mux"
)

// Resolve availability when clicked so dashboard links survive tier changes.
func ConferenceCheckout(w http.ResponseWriter, r *http.Request, app *config.AppContext) {
	w.Header().Set("Cache-Control", "no-store")
	conf, err := getters.GetConfByTag(app, mux.Vars(r)["conf"])
	if err != nil {
		http.Error(w, "Unable to load event", http.StatusServiceUnavailable)
		return
	}
	if conf == nil || !conf.IsPublished() {
		http.NotFound(w, r)
		return
	}
	eventURL := "/" + url.PathEscape(conf.Tag) + "#tickets"
	if !conf.Active || conf.HasEnded() {
		http.Redirect(w, r, eventURL, http.StatusSeeOther)
		return
	}
	inventory, err := getters.TicketInventory(app, conf.Ref)
	if err != nil {
		http.Error(w, "Ticket availability is temporarily unavailable", http.StatusServiceUnavailable)
		return
	}
	now := time.Now()
	ticket := types.CurrentConfTicketAt(conf.Tickets, inventory.Sold+inventory.Held, now)
	if ticket == nil {
		for _, option := range conf.Tickets {
			if inventory.Remaining(option, conf.Tickets, now) > 0 {
				ticket = option
				break
			}
		}
	}
	if ticket == nil {
		http.Redirect(w, r, eventURL, http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, i18n.CheckoutURL(checkoutLocale(r, conf), "/tix/"+url.PathEscape(ticket.ID)+"/checkout"), http.StatusSeeOther)
}

// NextPriceIncreaseTicket uses the same deadline and capacity rules as checkout.
// A closing sale or a transition to the same price is not a price increase.
func (p *ConfPage) NextPriceIncreaseTicket() *types.ConfTicket {
	if p == nil || p.Conf == nil || p.Tix == nil || p.Tix.Evergreen || p.Tix.Disabled || !p.Tix.SalesEndAt.After(time.Now()) {
		return nil
	}
	next := types.CurrentConfTicketAt(p.Conf.Tickets, p.Sold, p.Tix.SalesEndAt)
	if next == nil || !strings.EqualFold(next.Currency, p.Tix.Currency) || next.StandardPrice() <= p.Tix.StandardPrice() {
		return nil
	}
	return next
}
