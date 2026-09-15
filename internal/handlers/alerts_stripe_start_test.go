package handlers

import (
	"bytes"
	"html/template"
	"net/url"
	"os"
	"strings"
	"testing"
)

func TestAlertsStripeSelection(t *testing.T) {
	const account = "8f5ea224-65de-43c6-9f48-1fd89a21a2bb"
	valid := url.Values{"account": {account}, "slots": {"2"}}
	got, slots, err := alertsStripeSelection(valid)
	if err != nil || got != account || slots != 2 {
		t.Fatal(got, slots, err)
	}
	for _, v := range []url.Values{
		{"account": {account}, "slots": {"0"}},
		{"account": {account}, "slots": {"-1"}},
		{"account": {account}, "slots": {"1", "2"}},
		{"account": {account, account}, "slots": {"1"}},
		{"account": {"wrong"}, "slots": {"1"}},
		{"account": {account}, "slots": {"999999999999999"}},
	} {
		if _, _, err := alertsStripeSelection(v); err == nil {
			t.Fatal("accepted invalid selection", v)
		}
	}
}

func TestAlertsStripeStartTemplate(t *testing.T) {
	raw, err := os.ReadFile("../../templates/alerts_stripe_start.tmpl")
	if err != nil {
		t.Fatal(err)
	}
	tmpl, err := template.New("start").Parse(string(raw))
	if err != nil {
		t.Fatal(err)
	}
	for _, enabled := range []bool{false, true} {
		var out bytes.Buffer
		if err = tmpl.Execute(&out, alertsStripeStartPage{Account: "account", Slots: 2, Total: "$4.00", CSRF: `"><script>bad()</script>`, Key: "key", Enabled: enabled}); err != nil {
			t.Fatal(err)
		}
		html := out.String()
		if strings.Contains(html, "<script>bad()") || strings.Contains(html, `method="post"`) != enabled || !strings.Contains(html, "$4.00") {
			t.Fatal("incorrect checkout rendering")
		}
	}
}
