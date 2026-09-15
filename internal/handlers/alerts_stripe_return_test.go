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
	"btcpp-web/internal/alertsstripe"
)

func TestAlertsStripeReturnStates(t *testing.T) {
	now := time.Now()
	for _, tc := range []struct {
		name                                   string
		age                                    time.Duration
		bound, closed, enabled, reference, pay bool
		heading                                string
	}{
		{"new", time.Minute, false, false, true, false, true, "Awaiting payment confirmation"},
		{"resume", 100 * time.Minute, false, false, true, true, true, "Awaiting payment confirmation"},
		{"unbound creation too old", 100 * time.Minute, false, false, true, false, false, "Awaiting payment confirmation"},
		{"completed awaiting invoice", time.Minute, true, false, true, true, false, "Awaiting payment confirmation"},
		{"expired", 2 * time.Hour, false, false, true, true, false, "Checkout needs review"},
		{"closed", time.Minute, false, true, true, true, false, "Checkout closed"},
		{"disabled", time.Minute, false, false, false, false, false, "Awaiting payment confirmation"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stored := &getters.AlertsStripeCheckout{Intent: alertsstripe.Intent{ID: "checkout", PaidSlots: 2, CreatedAt: now.Add(-tc.age)}, Closed: tc.closed}
			if tc.reference {
				stored.Reference = "cs_test"
			}
			page := alertsStripeReturnPresentation(stored, tc.bound, tc.enabled, now)
			if page.CanPay != tc.pay || page.Heading != tc.heading || page.Total != "$4.00" {
				t.Fatal(page)
			}
			if strings.Contains(page.Heading, "Payment confirmed") {
				t.Fatal("checkout alone represented as paid")
			}
		})
	}
}

func TestAlertsStripeReturnTemplate(t *testing.T) {
	raw, err := os.ReadFile("../../templates/alerts_stripe_return.tmpl")
	if err != nil {
		t.Fatal(err)
	}
	tmpl, err := template.New("return").Parse(string(raw))
	if err != nil {
		t.Fatal(err)
	}
	for _, pay := range []bool{true, false} {
		var out bytes.Buffer
		if err = tmpl.Execute(&out, alertsStripeReturnPage{ID: "checkout", Heading: "Awaiting payment confirmation", Message: "<script>bad()</script>", CSRF: "secret", Total: "$4.00", CanPay: pay}); err != nil {
			t.Fatal(err)
		}
		html := out.String()
		if strings.Contains(html, "<script>bad()") || strings.Contains(html, `method="post"`) != pay || !strings.Contains(html, "https://alerts.btcpp.dev/alerts") {
			t.Fatal("unsafe or incomplete checkout page")
		}
	}
	r := httptest.NewRecorder()
	AlertsStripeReturn(r, httptest.NewRequest("DELETE", "/services/alerts/checkout/id", nil), nil)
	if r.Code != 405 {
		t.Fatal(r.Code)
	}
}
