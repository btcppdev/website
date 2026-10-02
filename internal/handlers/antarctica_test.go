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
	want := [][3]string{
		{"$16,429", "$3,285.80", "$12,321.75"},
		{"$15,599", "$3,119.80", "$11,699.25"},
		{"$14,429", "$2,885.80", "$10,821.75"},
	}
	wantSplit := [][2]string{{"$4,107.25", "$8,214.50"}, {"$3,899.75", "$7,799.50"}, {"$3,607.25", "$7,214.50"}}
	for i, berth := range page.Berths {
		got := [3]string{berth.Price, berth.Deposit, berth.BitcoinContribution}
		if got != want[i] {
			t.Errorf("%s: got %v, want %v", berth.ID, got, want[i])
		}
		if split := [2]string{berth.ReservationCost, berth.HeldBitcoinValue}; split != wantSplit[i] {
			t.Errorf("%s split: got %v, want %v", berth.ID, split, wantSplit[i])
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
	for _, required := range []string{"Private design preview", "Reservations are not open", "$16,429", "$15,599", "$14,429", "No deposit or payment is collected", "January 1, 2027", "April 1, 2027", "July 1, 2027", "October 1, 2027", "December 1, 2026"} {
		if !strings.Contains(out.String(), required) {
			t.Errorf("missing %q", required)
		}
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
		renderAntarcticaPage(w, app, newAntarcticaPage(nil, true))
	})
	t.Log("Antarctica preview: http://127.0.0.1:8110")
	t.Fatal(http.ListenAndServe("127.0.0.1:8110", mux))
}
