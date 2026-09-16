package handlers

import (
	"net/url"
	"testing"
)

func TestAlertsBitcoinSelectionAndAddress(t *testing.T) {
	account := "10000000-0000-0000-0000-000000000001"
	for _, months := range []string{"12", "24", "120"} {
		v := url.Values{"account": {account}, "slots": {"2"}, "months": {months}}
		if a, n, _, err := alertsBitcoinSelection(v); err != nil || a != account || n != 2 {
			t.Fatal(a, n, err)
		}
	}
	for _, query := range []string{"months=1&slots=2", "months=0&slots=2", "months=120&slots=100000", "months=12&slots=0", "months=12&months=24&slots=2", "months=12&slots=2&slots=3"} {
		v, _ := url.ParseQuery(query)
		v.Set("account", account)
		if _, _, _, err := alertsBitcoinSelection(v); err == nil {
			t.Fatal("accepted invalid selection", query)
		}
	}
	v := url.Values{"line1": {" 1 Main St "}, "line2": {""}, "city": {"Austin"}, "region": {"TX"}, "postal": {"78701"}, "country": {"us"}}
	if a, err := alertsBitcoinAddress(v); err != nil || a.Country != "US" || a.Line1 != "1 Main St" {
		t.Fatal(a, err)
	}
	for _, country := range []string{"", "USA", "1A", "US\n"} {
		v.Set("country", country)
		if _, err := alertsBitcoinAddress(v); err == nil {
			t.Fatal("accepted country", country)
		}
	}
	v.Set("country", "US")
	v.Add("line1", "different")
	if _, err := alertsBitcoinAddress(v); err == nil {
		t.Fatal("accepted duplicate address")
	}
}
