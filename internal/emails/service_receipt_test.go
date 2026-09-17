package emails

import (
	"strings"
	"testing"
	"time"

	"btcpp-web/external/getters"
)

func TestServiceReceiptUsesVerifiedOrderAndStableJob(t *testing.T) {
	start := time.Date(2026, 9, 17, 12, 30, 0, 0, time.UTC)
	end := start.AddDate(2, 0, 0)
	order := getters.ServiceOrderView{PublicID: "receipt-123", Status: "paid", PaymentVerified: true, Currency: "USD", TotalCents: 9600, TaxCents: 640, Quote: getters.ServiceQuote{Name: "Alerts <script>bad()</script>", ProductCode: "alerts.repository-slots", Quantity: 2, PeriodStart: &start, PeriodEnd: &end}}
	m, err := ServiceReceiptMail(&order, "owner@example.test", "https://btcpp.dev")
	if err != nil {
		t.Fatal(err)
	}
	again, err := ServiceReceiptMail(&order, "owner@example.test", "https://btcpp.dev")
	if err != nil {
		t.Fatal(err)
	}
	if m.JobKey != "service-receipt:receipt-123" || again.JobKey != m.JobKey || m.Email != "owner@example.test" || m.Sub != "" || m.Missive != "" {
		t.Fatal("receipt identity or transactional semantics changed", m.JobKey)
	}
	for _, body := range []string{string(m.HTMLBody), string(m.TextBody)} {
		for _, want := range []string{"$96.00", "$6.40", "September 17, 2028", "https://btcpp.dev/services/orders/receipt-123", "https://alerts.btcpp.dev/alerts"} {
			if !strings.Contains(body, want) {
				t.Fatal("missing receipt detail", want)
			}
		}
	}
	if strings.Contains(string(m.HTMLBody), "<script>bad()") {
		t.Fatal("unescaped product name")
	}
	for _, mutate := range []func(*getters.ServiceOrderView){
		func(o *getters.ServiceOrderView) { o.PaymentVerified = false },
		func(o *getters.ServiceOrderView) { o.Status = "refunded" },
		func(o *getters.ServiceOrderView) { o.Status = "pending" },
		func(o *getters.ServiceOrderView) { o.Currency = "EUR" },
		func(o *getters.ServiceOrderView) { o.TaxCents = o.TotalCents + 1 },
		func(o *getters.ServiceOrderView) { o.Quote.Quantity = 0 },
	} {
		copy := order
		mutate(&copy)
		if _, err := ServiceReceiptMail(&copy, "owner@example.test", "https://btcpp.dev"); err == nil {
			t.Fatal("invalid payment accepted")
		}
	}
	for _, recipient := range []string{"", "not-an-email", "Someone <owner@example.test>", "owner@example.test\r\nBcc: other@example.test"} {
		if _, err := ServiceReceiptMail(&order, recipient, "https://btcpp.dev"); err == nil {
			t.Fatal("invalid recipient accepted")
		}
	}
	for _, base := range []string{"javascript:alert(1)", "https://user:secret@btcpp.dev", "https://btcpp.dev?token=secret", "http://btcpp.dev"} {
		if _, err := ServiceReceiptMail(&order, "owner@example.test", base); err == nil {
			t.Fatal("invalid base accepted")
		}
	}
}
