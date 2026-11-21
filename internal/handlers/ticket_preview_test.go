package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"testing"

	"btcpp-web/external/getters"
	"btcpp-web/internal/auth"
	"btcpp-web/internal/eventplan"
	"github.com/gorilla/mux"
)

// Local visual review only. Payment creation is deliberately not registered.
func TestEvergreenPreview(t *testing.T) {
	if os.Getenv("BTCPP_EVERGREEN_PREVIEW") != "1" {
		t.Skip("local preview only")
	}
	app := wizardTestApp(t)
	app.Env.Port = "8127"
	c := context.Background()
	conf, err := getters.GetConfByTag(app, "crypto-demo")
	if err != nil {
		t.Fatal(err)
	}
	if conf == nil {
		_, err = getters.CreatePlannedEvent(app, eventplan.Basics{Tag: "crypto-demo", Name: "Cryptography edition", Edition: "global", Location: "Berlin, Germany", Timezone: "Europe/Berlin", Country: "DE", Start: "2027-05-01T09:00", End: "2027-05-03T18:00"}, "")
		if err != nil {
			t.Fatal(err)
		}
		conf, _ = getters.GetConfByTag(app, "crypto-demo")
		tickets := []eventplan.Ticket{
			{Tier: "Early bird", Price: 200, Capacity: 50, Currency: "EUR", SalesEnd: "2027-03-01T09:00", TaxCode: "txcd_20030000", CardSurchargeBPS: 1000},
			{Tier: "Standard", Price: 300, Capacity: 200, Currency: "EUR", SalesEnd: "2027-05-01T09:00", TaxCode: "txcd_20030000", CardSurchargeBPS: 1000},
			{Tier: "Supporter", Price: 500, Capacity: 25, Currency: "EUR", TaxCode: "txcd_20030000", CardSurchargeBPS: 1000, Evergreen: true, TicketKind: "supporter", Description: "Help make bitcoin education accessible to more developers.", AllowDiscounts: true},
			{Tier: "Local", Price: 100, Capacity: 40, Currency: "EUR", TaxCode: "txcd_20030000", CardSurchargeBPS: 1000, Evergreen: true, TicketKind: "local", Description: "For people living in the Berlin area."},
			{Tier: "Student", Price: 50, Capacity: 20, Currency: "EUR", TaxCode: "txcd_20030000", CardSurchargeBPS: 1000, Evergreen: true, TicketKind: "student", Description: "For currently enrolled students. Bring your student ID."},
		}
		raw, _ := json.Marshal(eventplan.TicketSettings{Tickets: tickets, Offers: []eventplan.CheckoutOffer{{Label: "Eurocrypt ticket holders", Percent: 15, Enabled: true}}})
		if _, err = getters.SaveEventPlanning(app, conf, 0, "", "tickets", raw); err != nil {
			t.Fatal(err)
		}
		if _, err = app.DB.Exec(c, `UPDATE conferences SET publication_status='published',active=true,leading_image_url='/static/img/berlin/leading.png' WHERE id=$1`, conf.Ref); err != nil {
			t.Fatal(err)
		}
	}
	person, err := getters.CreateSpeaker(app, getters.SpeakerInput{Name: "Ticket preview admin", Email: fmt.Sprintf("tickets-%d@example.test", os.Getpid())})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = app.DB.Exec(c, `INSERT INTO people_roles(person_id,scope,position) VALUES($1,'global','admin')`, person); err != nil {
		t.Fatal(err)
	}
	root := mux.NewRouter()
	registerEventWizard(root, app)
	root.HandleFunc("/tix/{tix}/checkout", func(w http.ResponseWriter, r *http.Request) { HandleCheckout(w, r, app) }).Methods("GET")
	root.HandleFunc("/tix/{tix}/apply-discount", func(w http.ResponseWriter, r *http.Request) { HandleDiscount(w, r, app) }).Methods("POST")
	root.HandleFunc("/preview/login", func(w http.ResponseWriter, r *http.Request) {
		if e := auth.LoginPerson(app, r, person, auth.MethodEmailLink); e != nil {
			http.Error(w, e.Error(), 500)
			return
		}
		http.Redirect(w, r, "/crypto-demo/admin/setup?step=tickets", 303)
	})
	root.HandleFunc("/navigation/account", func(w http.ResponseWriter, r *http.Request) { SiteAccountNavigation(w, r, app) })
	root.HandleFunc("/live/status", func(w http.ResponseWriter, r *http.Request) { LiveStatus(w, r, app) })
	root.PathPrefix("/static/").Handler(http.StripPrefix("/static/", http.FileServer(http.Dir("static"))))
	root.HandleFunc("/{conf}", func(w http.ResponseWriter, r *http.Request) { RenderConf(w, r, app) }).Methods("GET")
	t.Log("Preview: http://127.0.0.1:8127/preview/login")
	if err = http.ListenAndServe("127.0.0.1:8127", app.Session.LoadAndSave(root)); err != nil {
		t.Fatal(err)
	}
}
