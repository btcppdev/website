package handlers

import (
	"bytes"
	"html/template"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"btcpp-web/external/getters"
)

func TestServiceQuotePresentation(t *testing.T) {
	raw, err := os.ReadFile("../../templates/service_quote.tmpl")
	if err != nil {
		t.Fatal(err)
	}
	tmpl, err := template.New("service_quote.tmpl").Parse(string(raw))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	end := now.AddDate(1, 0, 0)
	quote := &getters.ServiceCheckoutQuote{ID: "local-quote", Quote: getters.ServiceQuote{Provider: "opennode", Quantity: 2, TotalCents: 2400, TaxCents: 200, PeriodStart: &now, PeriodEnd: &end, ExpiresAt: now.Add(time.Minute)}}
	for _, expired := range []bool{false, true} {
		at := now
		if expired {
			at = quote.Quote.ExpiresAt
		}
		page := serviceQuotePresentation(quote, at)
		page.CSRF = "<script>unsafe</script>"
		var out bytes.Buffer
		if err := tmpl.Execute(&out, page); err != nil {
			t.Fatal(err)
		}
		html := out.String()
		if page.Expired != expired || !strings.Contains(html, "$24.00") || !strings.Contains(html, "$2.00") || !strings.Contains(html, "no automatic renewal") {
			t.Fatal("incomplete review", html)
		}
		if strings.Contains(html, "<script>unsafe") || strings.Contains(html, `name="price"`) || strings.Contains(html, `name="quantity"`) {
			t.Fatal("unsafe review form")
		}
		if strings.Contains(html, `action="/services/quotes/local-quote/confirm"`) == expired {
			t.Fatal("confirmation availability disagrees with expiry")
		}
	}
	quote.Quote.BaseOrderID = "base"
	page := serviceQuotePresentation(quote, now)
	if !strings.Contains(page.Message, "prorated") || !strings.Contains(page.Message, "not charged again") {
		t.Fatal("upgrade terms missing")
	}
}

func TestServiceQuoteConfirmationRequiresPost(t *testing.T) {
	recorder := httptest.NewRecorder()
	ServiceQuoteConfirm(recorder, httptest.NewRequest("GET", "/services/quotes/any/confirm", nil), nil)
	if recorder.Code != 405 || recorder.Header().Get("Allow") != "POST" {
		t.Fatal("GET can confirm an order")
	}
}
