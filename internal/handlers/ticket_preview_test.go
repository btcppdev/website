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
	"btcpp-web/internal/types"
	"github.com/gorilla/mux"
)

// Local visual review only. Check-in payments use an in-process mock provider;
// customer checkout payment creation is deliberately not registered.
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
			{Tier: "Supporter", Price: 500, Capacity: 25, Currency: "EUR", TaxCode: "txcd_20030000", CardSurchargeBPS: 1000, Evergreen: true, TicketKind: "supporter", Description: "This ticket tier helps make bitcoin education accessible to more developers by providing subsidized tickets to students. Supporters are all acknowledged in our conference booklets.", AllowDiscounts: true},
			{Tier: "Local", Price: 100, Capacity: 40, Currency: "EUR", TaxCode: "txcd_20030000", CardSurchargeBPS: 1000, Evergreen: true, TicketKind: "local", Description: "For people living in the Berlin area."},
			{Tier: "Student", Price: 50, Capacity: 20, Currency: "EUR", TaxCode: "txcd_20030000", CardSurchargeBPS: 1000, Evergreen: true, TicketKind: "student", Description: "For currently enrolled students. Bring your student ID."},
		}
		raw, _ := json.Marshal(eventplan.TicketSettings{Tickets: tickets, Offers: []eventplan.CheckoutOffer{{Label: "Eurocrypt ticket holder", Restrictions: "Proof of Eurocrypt ticket will be requested at the door.", Percent: 15, Enabled: true}}})
		if _, err = getters.SaveEventPlanning(app, conf, 0, "", "tickets", raw); err != nil {
			t.Fatal(err)
		}
		if _, err = app.DB.Exec(c, `UPDATE conferences SET publication_status='published',active=true,leading_image_url='/static/img/berlin/leading.png' WHERE id=$1`, conf.Ref); err != nil {
			t.Fatal(err)
		}
	}
	person, err := getters.CreateSpeaker(app, getters.SpeakerInput{Name: "Ticket preview admin", Email: fmt.Sprintf("tickets-%d@example.test", os.Getpid()), Signal: "ticket-preview", AttendeePreferences: &types.AttendeePreferences{TShirt: "MM", Diet: "vegetarian", Allergies: "Peanuts"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = app.DB.Exec(c, `INSERT INTO people_roles(person_id,scope,position) VALUES($1,'global','admin')`, person); err != nil {
		t.Fatal(err)
	}
	root := mux.NewRouter()
	installCheckInUpgradePreview(t, app, root, conf)
	for i := 1; i <= 3; i++ {
		kind := "genpop"
		if i == 1 {
			kind = "supporter"
		}
		if i == 3 {
			kind = "student"
		}
		_, err = app.DB.Exec(c, `INSERT INTO registrations(ref_id,checkout_id,conference_id,type,email,person_id,platform)
 VALUES($1,'dev-attendee-preview',$2,$3,$4,$5,'dev-checkin-preview')
 ON CONFLICT(ref_id) DO UPDATE SET email=EXCLUDED.email,person_id=EXCLUDED.person_id,type=EXCLUDED.type`, fmt.Sprintf("dev-attendee-preview-%d", i), conf.Ref, kind, fmt.Sprintf("tickets-%d@example.test", os.Getpid()), person)
		if err != nil {
			t.Fatal(err)
		}
		// A restarted demo represents a new purchase for the new preview owner.
		if _, err = app.DB.Exec(c, `UPDATE registrations SET attendee_version=0 WHERE ref_id=$1`, fmt.Sprintf("dev-attendee-preview-%d", i)); err != nil {
			t.Fatal(err)
		}
	}
	var attendeeReservation string
	if err = app.DB.QueryRow(c, `INSERT INTO ticket_reservations(conference_id,ticket_id,quantity,email,provider,provider_id,status)
 VALUES($1,(SELECT id FROM conference_tickets WHERE conference_id=$1 AND NOT evergreen ORDER BY sales_end_at LIMIT 1),3,$2,'stripe','dev-attendee-preview','fulfilled') RETURNING id::text`, conf.Ref, fmt.Sprintf("tickets-%d@example.test", os.Getpid())).Scan(&attendeeReservation); err != nil {
		t.Fatal(err)
	}
	root.HandleFunc("/preview/attendees", func(w http.ResponseWriter, r *http.Request) {
		app.Session.Put(r.Context(), ticketDetailsReservationKey(conf.Tag), attendeeReservation)
		app.Session.Put(r.Context(), checkoutAddOnsSessionKey(conf.Tag), r.URL.Query().Get("addons") == "1")
		http.Redirect(w, r, "/"+conf.Tag+"/success", http.StatusSeeOther)
	})
	root.HandleFunc("/ticket/{ticket}/details", func(w http.ResponseWriter, r *http.Request) { TicketAttendeeDetails(w, r, app) }).Methods("GET", "POST")
	root.HandleFunc("/dashboard/profile", func(w http.ResponseWriter, r *http.Request) { DashboardEditSpeaker(w, r, app) }).Methods("GET", "POST")
	root.HandleFunc("/dashboard", func(w http.ResponseWriter, r *http.Request) { Dashboard(w, r, app) }).Methods("GET")
	root.HandleFunc("/ticket/{ticket}/pdf", func(w http.ResponseWriter, r *http.Request) { TicketPDF(w, r, app) }).Methods("GET")
	root.HandleFunc("/ticket/{ticket}", func(w http.ResponseWriter, r *http.Request) { Ticket(w, r, app) }).Methods("GET")
	root.HandleFunc("/dev/check-in/{ticket}", func(w http.ResponseWriter, r *http.Request) { DevCheckInPreview(w, r, app) }).Methods("GET")
	root.HandleFunc("/{conf}/admin/registrations", func(w http.ResponseWriter, r *http.Request) { RegistrationsAdmin(w, r, app) }).Methods("GET")
	root.HandleFunc("/{conf}/success", func(w http.ResponseWriter, r *http.Request) { RenderConfSuccess(w, r, app) }).Methods("GET", "POST")
	registerEventWizard(root, app)
	registerConferenceHackathonAdminRoutes(root, app)
	root.HandleFunc("/{conf}/hackathon", func(w http.ResponseWriter, r *http.Request) { HackathonShow(w, r, app) }).Methods("GET")
	root.HandleFunc("/tix/{tix}/checkout", func(w http.ResponseWriter, r *http.Request) { HandleCheckout(w, r, app) }).Methods("GET")
	root.HandleFunc("/{conf}/checkout", func(w http.ResponseWriter, r *http.Request) { ConferenceCheckout(w, r, app) }).Methods("GET")
	root.HandleFunc("/tix/{tix}/apply-discount", func(w http.ResponseWriter, r *http.Request) { HandleDiscount(w, r, app) }).Methods("POST")
	root.HandleFunc("/preview/login", func(w http.ResponseWriter, r *http.Request) {
		if e := auth.LoginPerson(app, r, person, auth.MethodEmailLink); e != nil {
			http.Error(w, e.Error(), 500)
			return
		}
		next := "/crypto-demo/admin/setup?step=tickets"
		if r.URL.Query().Get("next") != "" {
			next = safeReturnTo(r.URL.Query().Get("next"))
		}
		http.Redirect(w, r, next, 303)
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
