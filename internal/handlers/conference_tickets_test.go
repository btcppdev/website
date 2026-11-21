package handlers

import (
	"bytes"
	"regexp"
	"strings"
	"testing"
	"time"

	"btcpp-web/internal/config"
	"btcpp-web/internal/eventplan"
	"btcpp-web/internal/types"
)

func TestConferenceTicketCountdown(t *testing.T) {
	t.Chdir(findRepoRoot(t))
	ctx := &config.AppContext{Env: &types.EnvConfig{}}
	if err := loadTemplates(ctx); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(48 * time.Hour)
	for _, test := range []struct {
		name   string
		change func(*ConfPage, *types.ConfTicket)
		want   bool
	}{
		{"scheduled increase", func(*ConfPage, *types.ConfTicket) {}, true},
		{"final tier", func(p *ConfPage, _ *types.ConfTicket) { p.Conf.Tickets = p.Conf.Tickets[1:] }, false},
		{"same price", func(_ *ConfPage, next *types.ConfTicket) { next.BasePrice = 200 }, false},
		{"lower price", func(_ *ConfPage, next *types.ConfTicket) { next.BasePrice = 150 }, false},
		{"disabled tier", func(_ *ConfPage, next *types.ConfTicket) { next.Disabled = true }, false},
		{"supporter is not next tier", func(_ *ConfPage, next *types.ConfTicket) { next.Evergreen = true }, false},
		{"sold out next tier", func(p *ConfPage, next *types.ConfTicket) { next.Max = p.Sold }, false},
		{"same deadline", func(p *ConfPage, next *types.ConfTicket) { next.SalesEndAt = p.Tix.SalesEndAt }, false},
		{"expired deadline", func(p *ConfPage, _ *types.ConfTicket) { p.Tix.SalesEndAt = time.Now().Add(-time.Hour) }, false},
		{"missing deadline", func(p *ConfPage, _ *types.ConfTicket) { p.Tix.SalesEndAt = time.Time{} }, false},
		{"different currency", func(_ *ConfPage, next *types.ConfTicket) { next.Currency = "USD" }, false},
		{"closed sales", func(p *ConfPage, _ *types.ConfTicket) { p.Tix = nil }, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			current := &types.ConfTicket{ID: "early", Max: 50, BasePrice: 200, Currency: "EUR", SalesEndAt: deadline}
			next := &types.ConfTicket{ID: "general", Max: 200, BasePrice: 300, Currency: "EUR", SalesEndAt: deadline.Add(24 * time.Hour)}
			p := &ConfPage{Conf: &types.Conf{Tag: "seoul", Tickets: []*types.ConfTicket{next, current}}, Tix: current, Sold: 10}
			test.change(p, next)
			if got := p.NextPriceIncreaseTicket(); (got != nil) != test.want || (test.want && got != next) {
				t.Fatalf("next price increase = %v; want visible: %v", got, test.want)
			}
			for _, language := range []string{"en", "ko"} {
				templates := ctx.TemplateCache
				if language != "en" {
					templates = ctx.LocalizedTemplates[language]
				}
				var out bytes.Buffer
				if err := templates.ExecuteTemplate(&out, "generic_conf_tickets", map[string]any{"Page": p}); err != nil {
					t.Fatal(err)
				}
				if got := strings.Contains(out.String(), "data-ticket-countdown="); got != test.want {
					t.Fatalf("countdown rendered: %v, want %v", got, test.want)
				}
			}
		})
	}
}

func TestConferenceTicketTypeCards(t *testing.T) {
	t.Chdir(findRepoRoot(t))
	ctx := &config.AppContext{Env: &types.EnvConfig{}}
	if err := loadTemplates(ctx); err != nil {
		t.Fatal(err)
	}
	supporterCard := regexp.MustCompile(`(?s)<article class="event-redesign-ticket event-redesign-ticket--supporter">(.*?)</article>`)
	for _, test := range []struct {
		name      string
		standard  bool
		supporter bool
		remaining uint
		discounts bool
	}{
		{"both types", true, true, 25, true},
		{"supporter excludes offers", true, true, 25, false},
		{"supporter unavailable", true, true, 0, false},
		{"without supporter", true, false, 0, false},
		{"only supporter available", false, true, 25, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			page := &ConfPage{
				Conf:           &types.Conf{Tag: "seoul", Desc: "Cryptography edition"},
				CheckoutOffers: []eventplan.CheckoutOffer{{Label: "Eurocrypt ticket holder", Percent: 15}},
				Evergreen: []EvergreenTicketOption{{Ticket: &types.ConfTicket{
					ID: "student-pass", Tier: "Student", TicketKind: "student", Evergreen: true,
					BasePrice: 50, Symbol: "€", Currency: "EUR", CardSurchargeBPS: 1000,
				}, Remaining: 20}},
			}
			if test.standard {
				page.Tix = &types.ConfTicket{ID: "standard-pass", Tier: "Early bird", BasePrice: 200, Symbol: "€", Currency: "EUR", CardSurchargeBPS: 1000}
			}
			if test.supporter {
				page.Evergreen = append(page.Evergreen, EvergreenTicketOption{Ticket: &types.ConfTicket{
					ID: "supporter-pass", Tier: "Supporter", TicketKind: "supporter", Evergreen: true,
					BasePrice: 500, Symbol: "€", Currency: "EUR", CardSurchargeBPS: 1000,
					Description: "Support <bitcoin++>", AllowDiscounts: test.discounts,
				}, Remaining: test.remaining})
			}
			for _, language := range []string{"en", "ko"} {
				templates := ctx.TemplateCache
				if language != "en" {
					templates = ctx.LocalizedTemplates[language]
				}
				var out bytes.Buffer
				if err := templates.ExecuteTemplate(&out, "generic_conf_tickets", map[string]any{"Page": page}); err != nil {
					t.Fatal(err)
				}
				body := out.String()
				if strings.Contains(body, "event-redesign-ticket--card") || strings.Contains(body, "payment=card") {
					t.Fatal("payment method still presented as a ticket type")
				}
				if test.standard && (!strings.Contains(body, "<strong>€200</strong>") || strings.Count(body, "/tix/standard-pass/checkout") != 1) {
					t.Fatal("standard card lost the current tier's Bitcoin price or checkout link")
				}
				if strings.Count(body, "/tix/student-pass/checkout") != 1 {
					t.Fatal("student ticket missing or duplicated")
				}
				cards := supporterCard.FindAllStringSubmatch(body, -1)
				if !test.supporter {
					if len(cards) != 0 {
						t.Fatal("unconfigured supporter ticket shown")
					}
					continue
				}
				if len(cards) != 1 || !strings.Contains(cards[0][1], "<strong>€500</strong>") || strings.Contains(cards[0][1], "€550") {
					t.Fatal("supporter must appear once at its Bitcoin price")
				}
				if !strings.Contains(cards[0][1], "Support &lt;bitcoin&#43;&#43;&gt;") {
					t.Fatal("supporter description missing or not escaped")
				}
				if strings.Contains(cards[0][1], "Eurocrypt ticket holder") != test.discounts {
					t.Fatal("supporter offers do not match its discount policy")
				}
				wantLinks := 0
				if test.remaining > 0 {
					wantLinks = 1
				}
				if strings.Count(body, "/tix/supporter-pass/checkout") != wantLinks {
					t.Fatal("supporter checkout duplicated, missing, or available without inventory")
				}
				if !test.standard && strings.Contains(body, "This event is sold out.") {
					t.Fatal("available supporter tickets incorrectly advertised as event sold out")
				}
			}
		})
	}
}
