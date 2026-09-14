package handlers

import (
	"bytes"
	"html/template"
	"os"
	"strings"
	"testing"
	"time"

	"btcpp-web/external/getters"
	"btcpp-web/internal/config"
	"btcpp-web/internal/types"
)

func TestServiceOrderPresentation(t *testing.T) {
	for _, tc := range []struct {
		status, access string
		verified       bool
		heading        string
	}{
		{"pending", "pending", false, "Awaiting payment"},
		{"paid", "pending", false, "Awaiting payment"},
		{"paid", "active", true, "Payment confirmed"},
		{"paid", "pending", true, "Payment confirmed — access pending"},
		{"cancelled", "pending", true, "Payment received — review needed"},
		{"cancelled", "pending", false, "Order canceled"},
		{"paid", "expired", true, "Access period ended"},
		{"paid", "inactive", true, "Access inactive"},
		{"partially_refunded", "inactive", true, "Refund recorded"},
	} {
		view := &getters.ServiceOrderView{Status: tc.status, AccessState: tc.access, PaymentVerified: tc.verified, TotalCents: 1234, TaxCents: 34}
		page := serviceOrderPresentation(view)
		if page.Heading != tc.heading || page.Total != "$12.34" || page.Tax != "$0.34" {
			t.Fatal(tc, page)
		}
	}
	data, err := os.ReadFile("../../templates/service_order.tmpl")
	if err != nil {
		t.Fatal(err)
	}
	tmpl, err := template.New("service_order.tmpl").Parse(string(data))
	if err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, 9, 14, 12, 30, 0, 0, time.UTC)
	end := start.AddDate(1, 0, 0)
	page := serviceOrderPresentation(&getters.ServiceOrderView{PublicID: "order-1", Status: "paid", AccessState: "active", PaymentVerified: true, Currency: "USD", TotalCents: 1200, Quote: getters.ServiceQuote{Name: "<script>bad()</script>", ProductCode: "alerts.repository-slots", Quantity: 1, PeriodStart: &start, PeriodEnd: &end}})
	var out bytes.Buffer
	if err := tmpl.Execute(&out, page); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "<script>bad()") || !strings.Contains(out.String(), "September 14, 2027") || !strings.Contains(out.String(), "https://alerts.btcpp.dev/alerts") {
		t.Fatal("unsafe or incomplete order rendering")
	}
}

func TestServiceOrderPaymentAvailability(t *testing.T) {
	now := time.Now()
	app := &config.AppContext{Env: &types.EnvConfig{OpenNode: types.OpenNodeConfig{Key: "local-test-key"}}}
	base := getters.ServiceOrderView{Status: "pending", Currency: "USD", TotalCents: 1200, Quote: getters.ServiceQuote{Provider: "opennode", TotalCents: 1200, ExpiresAt: now.Add(time.Hour)}}
	if !serviceOrderCanPay(&base, app, now) {
		t.Fatal("valid payment hidden")
	}
	for _, mutate := range []func(*getters.ServiceOrderView){
		func(o *getters.ServiceOrderView) { o.Status = "paid" },
		func(o *getters.ServiceOrderView) { o.PaymentVerified = true },
		func(o *getters.ServiceOrderView) { o.Quote.ExpiresAt = now },
		func(o *getters.ServiceOrderView) { o.Quote.Provider = "stripe" },
		func(o *getters.ServiceOrderView) { o.TotalCents++ },
		func(o *getters.ServiceOrderView) { o.TaxCents++ },
		func(o *getters.ServiceOrderView) { o.Currency = "EUR" },
	} {
		order := base
		mutate(&order)
		if serviceOrderCanPay(&order, app, now) {
			t.Fatal("ineligible payment offered", order)
		}
	}
	app.Env.OpenNode.Key = ""
	if serviceOrderCanPay(&base, app, now) {
		t.Fatal("unconfigured provider offered")
	}
}
