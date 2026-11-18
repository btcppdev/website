package handlers

import (
	"btcpp-web/external/getters"
	"btcpp-web/internal/auth"
	"btcpp-web/internal/config"
	"btcpp-web/internal/eventplan"
	"btcpp-web/internal/types"
	"context"
	"fmt"
	"github.com/alexedwards/scs/v2"
	"github.com/gorilla/mux"
	"github.com/jackc/pgx/v5/pgxpool"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func wizardTestApp(t *testing.T) *config.AppContext {
	t.Helper()
	if os.Getenv("BTCPP_EVENT_WIZARD_TEST") != "1" {
		t.Skip("set BTCPP_EVENT_WIZARD_TEST=1 with a migrated disposable database")
	}
	pool, e := pgxpool.New(context.Background(), os.Getenv("DATABASE_URL"))
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(pool.Close)
	wd, _ := os.Getwd()
	if e = os.Chdir("../.."); e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { os.Chdir(wd) })
	app := &config.AppContext{DB: pool, Session: scs.New(), Env: &types.EnvConfig{MailOff: true, Host: "127.0.0.1", Port: "8117"}, Err: log.Default(), Infos: log.Default()}
	app.Session.Cookie.Name = "event_wizard_test"
	if e = loadTemplates(app); e != nil {
		t.Fatal(e)
	}
	return app
}
func TestEventWizardAccessAndStudentPricing(t *testing.T) {
	app := wizardTestApp(t)
	c := context.Background()
	var confID string
	b := eventplan.Basics{Tag: fmt.Sprintf("handler-%d", os.Getpid()), Name: "Wizard test", Edition: "global", Timezone: "UTC"}
	var e error
	confID, e = getters.CreatePlannedEvent(app, b, "")
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { app.DB.Exec(c, `DELETE FROM conferences WHERE id=$1`, confID) })
	person, e := getters.CreateSpeaker(app, getters.SpeakerInput{Name: "Wizard admin", Email: b.Tag + "@example.test"})
	if e != nil {
		t.Fatal(e)
	}
	defer app.DB.Exec(c, `DELETE FROM people WHERE id=$1`, person)
	app.DB.Exec(c, `INSERT INTO people_roles(person_id,scope,position) VALUES($1,$2,'admin')`, person, b.Tag)
	root := mux.NewRouter()
	registerEventWizard(root, app)
	request := func(path, body, csrf string) *httptest.ResponseRecorder {
		t.Helper()
		method := "GET"
		if body != "" {
			method = "POST"
		}
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-CSRF-Token", csrf)
		w := httptest.NewRecorder()
		app.Session.LoadAndSave(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if e := auth.LoginPerson(app, r, person, auth.MethodEmailLink); e != nil {
				t.Fatal(e)
			}
			app.Session.Put(r.Context(), authMethodsCSRFKey, "test-token")
			root.ServeHTTP(w, r)
		})).ServeHTTP(w, req)
		return w
	}
	if w := request("/admin/events/new", "", ""); w.Code != 303 {
		t.Fatalf("scoped admin create: %d", w.Code)
	}
	path := "/" + b.Tag + "/admin/setup"
	if w := request(path, "", ""); w.Code != 200 || !strings.Contains(w.Body.String(), "event-wizard-data") {
		t.Fatalf("render %d %s", w.Code, w.Body.String())
	}
	body := `{"section":"readiness","revision":0,"data":{"tasks":[]}}`
	if w := request(path, body, "wrong"); w.Code != 403 {
		t.Fatal("CSRF bypass", w.Code)
	}
	if w := request(path, body, "test-token"); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := request(path, body, "test-token"); w.Code != 409 {
		t.Fatal("stale write", w.Code, w.Body.String())
	}
	app.DB.Exec(c, `UPDATE people_roles SET position='staff' WHERE person_id=$1`, person)
	if w := request(path, body, "test-token"); w.Code != 403 {
		t.Fatal("staff wrote settings", w.Code)
	}
	if w := request(path, "", ""); w.Code != 200 {
		t.Fatal("staff cannot view", w.Code)
	}
	var ticketID string
	e = app.DB.QueryRow(c, `INSERT INTO conference_tickets(conference_id,ticket_key,tier,base_price,btc_price,usd_price,student_price,max_count,currency,sales_end_at) VALUES($1,'early','Early',100,100,100,25,100,'USD',now()+interval '1 year') RETURNING id::text`, confID).Scan(&ticketID)
	if e != nil {
		t.Fatal(e)
	}
	invalidateConferenceCache(app)
	_, _, price, kind, e := determineTixPrice(app, ticketID+"+student")
	if e != nil || price != 25 || kind != types.TicketTypeStudent {
		t.Fatalf("student checkout %d %s %v", price, kind, e)
	}
	app.DB.Exec(c, `UPDATE conference_tickets SET student_price=NULL WHERE id=$1`, ticketID)
	invalidateConferenceCache(app)
	if _, _, _, _, e = determineTixPrice(app, ticketID+"+student"); e == nil {
		t.Fatal("disabled student checkout available")
	}
}
func TestEventWizardPreview(t *testing.T) {
	if os.Getenv("BTCPP_EVENT_WIZARD_PREVIEW") != "1" {
		t.Skip("local preview only")
	}
	app := wizardTestApp(t)
	c := context.Background()
	person, e := getters.CreateSpeaker(app, getters.SpeakerInput{Name: "Event planner preview", Email: fmt.Sprintf("preview-%d@example.test", os.Getpid())})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = app.DB.Exec(c, `INSERT INTO people_roles(person_id,scope,position) VALUES($1,'global','admin') ON CONFLICT DO NOTHING`, person); e != nil {
		t.Fatal(e)
	}
	root := mux.NewRouter()
	registerEventWizard(root, app)
	registerOrganizationShops(root, app)
	var org string
	if e = app.DB.QueryRow(c, `INSERT INTO organizations(id,name) VALUES('00000000-0000-4000-8000-000000008117','Tokyo merch demo') ON CONFLICT(id) DO UPDATE SET name=excluded.name RETURNING id::text`).Scan(&org); e != nil {
		t.Fatal(e)
	}
	root.HandleFunc("/shop/cart", func(w http.ResponseWriter, r *http.Request) { ShopCart(w, r, app) }).Methods("GET")
	root.HandleFunc("/shop/cart/add", func(w http.ResponseWriter, r *http.Request) { ShopCartAdd(w, r, app) }).Methods("POST")
	root.HandleFunc("/shop/checkout", func(w http.ResponseWriter, r *http.Request) { ShopCheckout(w, r, app) }).Methods("GET")
	root.HandleFunc("/shop/shipping-rates", func(w http.ResponseWriter, r *http.Request) { ShopShippingRates(w, r, app) }).Methods("POST")
	root.HandleFunc("/preview/login", func(w http.ResponseWriter, r *http.Request) {
		if e := auth.LoginPerson(app, r, person, auth.MethodEmailLink); e != nil {
			http.Error(w, e.Error(), 500)
			return
		}
		http.Redirect(w, r, "/admin/events", 303)
	})
	root.PathPrefix("/static/").Handler(http.StripPrefix("/static/", http.FileServer(http.Dir("static"))))
	t.Log("Preview: http://127.0.0.1:8117/preview/login")
	if e = http.ListenAndServe("127.0.0.1:8117", app.Session.LoadAndSave(root)); e != nil {
		t.Fatal(e)
	}
}
