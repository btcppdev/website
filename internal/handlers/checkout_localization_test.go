package handlers

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"regexp"
	"strings"
	"testing"

	"btcpp-web/internal/config"
	"btcpp-web/internal/i18n"
	"btcpp-web/internal/types"
	"github.com/stripe/stripe-go/v86"
)

func TestCheckoutLocaleRouting(t *testing.T) {
	for _, tc := range []struct{ event, query, want string }{{"seoul", "?lang=ko", "ko"}, {"seoul", "?lang=en", "en"}, {"seoul", "?lang=fr", "en"}, {"berlin26", "?lang=ko", "en"}, {"seoul", "", "en"}} {
		req := httptest.NewRequest("GET", "/tix/test/checkout"+tc.query, nil)
		if got := checkoutLocale(req, &types.Conf{Tag: tc.event}); got != tc.want {
			t.Errorf("%+v: got %s", tc, got)
		}
	}
	for input, want := range map[string]string{"/tix/123+local/checkout?payment=card": "/tix/123+local/checkout?lang=ko&payment=card", "/seoul/success?session_id=cs_test": "/seoul/success?lang=ko&session_id=cs_test"} {
		if got := i18n.CheckoutURL("ko", input); got != want {
			t.Errorf("%s -> %s", input, got)
		}
	}
}

func TestLocalizedCheckoutRendering(t *testing.T) {
	t.Chdir("../..")
	ctx := &config.AppContext{Env: &types.EnvConfig{}}
	if err := loadTemplates(ctx); err != nil {
		t.Fatal(err)
	}
	conf := &types.Conf{Tag: "seoul", Desc: "bitcoin++ Seoul, privacy edition", Location: "Seoul, South Korea", DateDesc: "Nov 5 - 6, 2026"}
	page := &TixFormPage{Conf: conf, Tix: &types.ConfTicket{ID: "pass", Tier: "General", Currency: "USD", Symbol: "$"}, TixSlug: "pass", Count: 2, TixPrice: 389, DiscountPrice: 300, CardPrice: 330, Discount: "<bad>", HMAC: "signed-price"}
	page.AddOnProducts = []*ticketCheckoutAddOnProduct{{Product: &types.MerchProduct{Name: "Test shirt", Subtitle: "Event pickup"}, Variant: &types.MerchVariant{ID: "shirt"}, UnitPriceCents: 2000}}
	for _, locale := range []string{"en", "ko"} {
		req := httptest.NewRequest("GET", "/tix/pass/checkout?lang="+locale, nil)
		w := httptest.NewRecorder()
		if err := renderCheckout(w, req, ctx, "collect-email.tmpl", page, conf); err != nil {
			t.Fatal(err)
		}
		body := w.Body.String()
		for _, expected := range []string{`lang="` + locale + `"`, `value="signed-price"`, `value="300"`, `&lt;bad&gt;`, `data-ticket-card-price-cents="33000"`} {
			if !strings.Contains(body, expected) {
				t.Errorf("%s missing %s", locale, expected)
			}
		}
		if locale == "ko" {
			for _, expected := range []string{"티켓 2장", "비트코인", "대한민국 서울", `action="/tix/pass/checkout?lang=ko"`, `hx-post="/tix/pass/apply-discount?lang=ko"`, `data-tax-url="/tix/pass/tax-quote?lang=ko"`} {
				if !strings.Contains(body, expected) {
					t.Errorf("missing %s", expected)
				}
			}
		}
		match := regexp.MustCompile(`(?s)<script type="application/json" id="checkout-messages">(.*?)</script>`).FindStringSubmatch(body)
		if len(match) != 2 {
			t.Fatal("missing client catalog")
		}
		var messages map[string]string
		if err := json.Unmarshal([]byte(match[1]), &messages); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(messages["continue"], "{Tickets}") || !strings.Contains(messages["continue"], "{Total}") {
			t.Fatal("client placeholders lost")
		}
		if os.Getenv("BTCPP_TRANSLATION_PREVIEW") == "1" {
			if err := os.WriteFile("/tmp/btcpp-checkout-"+locale+".html", w.Body.Bytes(), 0600); err != nil {
				t.Fatal(err)
			}
		}
		for _, sponsored := range []bool{false, true} {
			w = httptest.NewRecorder()
			if err := renderCheckout(w, req, ctx, "success.tmpl", &SuccessPage{Conf: conf, Sponsored: sponsored}, conf); err != nil {
				t.Fatal(err)
			}
			if locale == "ko" && !strings.Contains(w.Body.String(), `href="/ko/seoul"`) {
				t.Fatal("confirmation loses locale")
			}
		}
		w = httptest.NewRecorder()
		if err := renderCheckout(w, req, ctx, "tix_details.tmpl", page, conf); err != nil {
			t.Fatal(err)
		}
		if locale == "ko" && !strings.Contains(w.Body.String(), "티켓 2장") {
			t.Fatal("discount fragment lost locale")
		}
	}
}

func TestCheckoutStepsFollowAvailableAddOns(t *testing.T) {
	t.Chdir("../..")
	app := &config.AppContext{Env: &types.EnvConfig{}}
	if err := loadTemplates(app); err != nil {
		t.Fatal(err)
	}
	conf := &types.Conf{Tag: "seoul"}
	for _, tc := range []struct {
		name                   string
		available, unavailable bool
	}{
		{"none", false, false}, {"temporarily unavailable", false, true}, {"available", true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			page := &TixFormPage{Conf: conf, Tix: &types.ConfTicket{ID: "pass", Tier: "General", Currency: "USD"}, TixSlug: "pass", Count: 1}
			if tc.unavailable {
				page.AddOnUnavailable = "Pricing unavailable"
			}
			if tc.available {
				page.AddOnProducts = []*ticketCheckoutAddOnProduct{{Product: &types.MerchProduct{Name: "Shirt"}, Variant: &types.MerchVariant{ID: "shirt"}, UnitPriceCents: 2000}}
			}
			for _, locale := range []string{"en", "ko"} {
				req := httptest.NewRequest("GET", "/tix/pass/checkout?lang="+locale, nil)
				w := httptest.NewRecorder()
				if err := renderCheckout(w, req, app, "collect-email.tmpl", page, conf); err != nil {
					t.Fatal(err)
				}
				body := w.Body.String()
				for _, marker := range []string{`data-checkout-nav="addons"`, `data-checkout-step="addons"`, `data-checkout-next`, `data-checkout-back`} {
					if strings.Contains(body, marker) != tc.available {
						t.Errorf("%s: incorrect add-on control %s", locale, marker)
					}
				}
				steps := "3"
				if tc.available {
					steps = "4"
				}
				if !strings.Contains(body, `data-step="01/0`+steps+`"`) || strings.Count(body, `data-checkout-submit`) != 1 {
					t.Fatal("incorrect progress or missing payment submit button")
				}
				if !tc.available {
					registration := regexp.MustCompile(`(?s)data-checkout-step="register".*?data-checkout-submit`).MatchString(body)
					if !registration {
						t.Fatal("registration does not submit directly to payment")
					}
				}
				w = httptest.NewRecorder()
				if err := renderCheckout(w, req, app, "success.tmpl", &SuccessPage{Conf: conf, HasAddOns: tc.available}, conf); err != nil {
					t.Fatal(err)
				}
				if !strings.Contains(w.Body.String(), `data-step="0`+steps+`/0`+steps+`"`) {
					t.Fatal("confirmation has the wrong progress count")
				}
			}
		})
	}
}

type localizedCheckoutTransport func(*http.Request) (*http.Response, error)

func (f localizedCheckoutTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestLocalizedStripeCheckout(t *testing.T) {
	original := stripe.GetBackend(stripe.APIBackend)
	t.Cleanup(func() { stripe.SetBackend(stripe.APIBackend, original) })
	client := &http.Client{Transport: localizedCheckoutTransport(func(r *http.Request) (*http.Response, error) {
		b, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatal(err)
		}
		form, err := url.ParseQuery(string(b))
		if err != nil {
			t.Fatal(err)
		}
		if form.Get("metadata[locale]") != "ko" {
			t.Error("Stripe registration locale missing")
		}
		if form.Get("locale") != "ko" {
			t.Errorf("Stripe locale %q", form.Get("locale"))
		}
		if !strings.Contains(form.Get("success_url"), "/seoul/success?lang=ko&session_id={CHECKOUT_SESSION_ID}") {
			t.Errorf("success URL: %s", form.Get("success_url"))
		}
		if !strings.HasSuffix(form.Get("cancel_url"), "/ko/seoul") {
			t.Errorf("cancel URL: %s", form.Get("cancel_url"))
		}
		if form.Get("line_items[0][price_data][unit_amount]") != "33000" || form.Get("line_items[0][quantity]") != "2" {
			t.Error("payment amounts changed")
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"id":"cs_test","url":"https://checkout.example.test/pay"}`))}, nil
	})}
	stripe.SetBackend(stripe.APIBackend, stripe.GetBackendWithConfig(stripe.APIBackend, &stripe.BackendConfig{HTTPClient: client}))
	ctx := &config.AppContext{Env: &types.EnvConfig{Host: "localhost", LocalExternal: "https://checkout.example.test"}, Err: log.New(io.Discard, "", 0)}
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/tix/pass/checkout?lang=ko", nil)
	StripeInitWithDiscount(w, r, ctx, &types.Conf{Tag: "seoul", Desc: "Seoul"}, &types.ConfTicket{Currency: "USD"}, 330, 389, 300, &types.TixForm{Email: "buyer@example.test", Count: 2}, types.TicketTypeGeneral, nil, nil, 0, "")
	if w.Code != http.StatusSeeOther {
		t.Fatalf("payment redirect failed: %d %s", w.Code, w.Body.String())
	}
}

func TestCheckoutAccountDeliveryRendering(t *testing.T) {
	t.Chdir("../..")
	app := &config.AppContext{Env: &types.EnvConfig{}}
	if err := loadTemplates(app); err != nil {
		t.Fatal(err)
	}
	conf := &types.Conf{Tag: "seoul", Desc: "Seoul"}
	for _, language := range []string{"en", "ko"} {
		page := &TixFormPage{Conf: conf, Tix: &types.ConfTicket{Currency: "USD", Symbol: "$"}, Count: 1, AccountEmail: "buyer@example.test"}
		request := httptest.NewRequest("GET", "/tix/pass/checkout?lang="+language, nil)
		w := httptest.NewRecorder()
		if err := renderCheckout(w, request, app, "collect-email.tmpl", page, conf); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(w.Body.String(), `type="email"`) || !strings.Contains(w.Body.String(), `<strong>buyer@example.test</strong>`) {
			t.Fatal("account delivery is not read-only")
		}
		page.AccountEmail = ""
		w = httptest.NewRecorder()
		if err := renderCheckout(w, request, app, "collect-email.tmpl", page, conf); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(w.Body.String(), `type="email" id="Email"`) {
			t.Fatal("guest/account without email cannot enter delivery email")
		}
	}
}
