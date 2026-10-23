package handlers

import (
	"bytes"
	"html/template"
	"strings"
	"testing"
)

func TestAlertsStripeManageIncreaseReview(t *testing.T) {
	tmpl, err := template.ParseFiles("../../templates/alerts_stripe_manage.tmpl")
	if err != nil {
		t.Fatal(err)
	}
	page := alertsStripeManagePage{CanIncrease: true, Account: "account-fixture", PaidSlots: 2, MinIncrease: 3, MaxIncrease: 894784}
	var out bytes.Buffer
	if err = tmpl.Execute(&out, page); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`method="get" action="/services/alerts/subscribe"`, `name="account" value="account-fixture"`, `name="slots" type="number" min="3"`, "Review increase", "prorated charge"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("missing %s", want)
		}
	}
	page.CanIncrease = false
	out.Reset()
	if err = tmpl.Execute(&out, page); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "Review increase") {
		t.Fatal("unavailable upgrade shown")
	}
}
