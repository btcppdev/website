package handlers

import (
	"bytes"
	"log"
	"net/http"
	"os"
	"strings"
	"testing"

	"btcpp-web/internal/config"
	"btcpp-web/internal/types"
)

func TestAntarcticaFares(t *testing.T) {
	page := newAntarcticaPage(nil, true)
	want := [][2]string{
		{"$16,429", "$3,285.80"},
		{"$15,599", "$3,119.80"},
		{"$14,429", "$2,885.80"},
	}
	wantFull := [][2]string{{"$15,607.55", "$821.45"}, {"$14,819.05", "$779.95"}, {"$13,707.55", "$721.45"}}
	for i, berth := range page.Berths {
		if full := [2]string{berth.PayInFull, berth.PayInFullSavings}; full != wantFull[i] {
			t.Errorf("%s pay in full: got %v, want %v", berth.ID, full, wantFull[i])
		}
		got := [2]string{berth.Price, berth.Deposit}
		if got != want[i] {
			t.Errorf("%s: got %v, want %v", berth.ID, got, want[i])
		}
	}
}

func antarcticaTestTemplates(t *testing.T) *config.AppContext {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir("../.."); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(wd) })
	app := &config.AppContext{Env: &types.EnvConfig{}, Err: log.New(os.Stderr, "", 0)}
	if err := loadTemplates(app); err != nil {
		t.Fatal(err)
	}
	return app
}

func TestAntarcticaTemplate(t *testing.T) {
	app := antarcticaTestTemplates(t)
	var out bytes.Buffer
	if err := app.TemplateCache.ExecuteTemplate(&out, "conf/antarctica.tmpl", newAntarcticaPage(nil, true)); err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{"Private design preview", "Reservations are not open", "$16,429", "$15,599", "$14,429", "No deposit or payment is collected", "January 1, 2027", "April 1, 2027", "July 1, 2027", "October 1, 2027", "Pay in full with bitcoin", "Pay in full via Stripe"} {
		if !strings.Contains(out.String(), required) {
			t.Errorf("missing %q", required)
		}
	}
	for _, removed := range []string{"bitcoin-explorer", "Stay in bitcoin", "December 1, 2026", "data-held", "Settle before sailing"} {
		if strings.Contains(out.String(), removed) {
			t.Errorf("removed payment plan still present: %q", removed)
		}
	}
}

// Explicit opt-in, loopback-only design preview. No database or payment provider.
func TestAntarcticaCheckout(t *testing.T) {
	app := antarcticaTestTemplates(t)
	var out bytes.Buffer
	if err := app.TemplateCache.ExecuteTemplate(&out, "conf/antarctica-checkout.tmpl", newAntarcticaPage(nil, true)); err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{"Number of spots to reserve", "cabin-allocations", "Roommate", "newsletter-opt-in", "Payments opening soon"} {
		if !strings.Contains(strings.ToLower(out.String()), strings.ToLower(required)) {
			t.Errorf("missing checkout content %q", required)
		}
	}
	if strings.Contains(out.String(), `id="newsletter-opt-in" checked`) {
		t.Fatal("newsletter preselected")
	}
}

// Explicit opt-in, loopback-only design preview. No database or payment provider.
func TestAntarcticaDemo(t *testing.T) {
	if os.Getenv("ANTARCTICA_DEMO") != "1" {
		t.Skip("local expedition design preview")
	}
	app := antarcticaTestTemplates(t)
	mux := http.NewServeMux()
	mux.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir("static"))))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("checkout") == "1" {
			renderAntarcticaCheckout(w, app, newAntarcticaPage(nil, true))
			return
		}
		renderAntarcticaPage(w, app, newAntarcticaPage(nil, true))
	})
	t.Log("Antarctica preview: http://127.0.0.1:8110")
	t.Fatal(http.ListenAndServe("127.0.0.1:8110", mux))
}
