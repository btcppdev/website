package handlers

import (
	"context"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"testing"
	"time"

	"btcpp-web/external/getters"
	"btcpp-web/internal/auth"
	"btcpp-web/internal/config"
	"btcpp-web/internal/types"

	"github.com/alexedwards/scs/v2"
	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestSponsorPayoutHTTPFlow(t *testing.T) {
	if os.Getenv("BTCPP_POSTGRES_SMOKE") != "1" {
		t.Skip("requires disposable migrated PostgreSQL database")
	}
	c := context.Background()
	pool, err := pgxpool.New(c, os.Getenv("DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	app := &config.AppContext{DB: pool, Session: scs.New(), Env: &types.EnvConfig{MailOff: true}, Err: log.New(io.Discard, "", 0), Infos: log.New(io.Discard, "", 0)}
	wd, _ := os.Getwd()
	if err := os.Chdir("../.."); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(wd)
	if err := loadTemplates(app); err != nil {
		t.Fatal(err)
	}
	id := func(query string, args ...any) string {
		t.Helper()
		var value string
		if err := pool.QueryRow(c, query, args...).Scan(&value); err != nil {
			t.Fatal(err)
		}
		return value
	}
	run := func(query string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(c, query, args...); err != nil {
			t.Fatal(err)
		}
	}
	owner := id(`INSERT INTO people(name) VALUES ('Sponsor manager') RETURNING id::text`)
	winner := id(`INSERT INTO people(name,lightning_address,bitcoin_address,tax_form_type,tax_form_uploaded_at)
		VALUES ('Alex Builder','alex@example.test','bc1qexamplepayoutaddressforlayouttestingonly','w9',now()) RETURNING id::text`)
	org := id(`INSERT INTO organizations(name,tagline) VALUES($1,'Supporting the next generation of Bitcoin projects') RETURNING id::text`, "Bitcoin Builders "+uuid.NewString()[:8])
	conf := id(`INSERT INTO conferences(tag,description,start_date,end_date,timezone)
		VALUES($1,'bitcoin++ Hackathon',now()-interval '7 days',now()-interval '5 days','UTC') RETURNING id::text`, "payout-"+uuid.NewString())
	defer func() {
		run(`DELETE FROM conferences WHERE id=$1`, conf)
		run(`DELETE FROM organizations WHERE id=$1`, org)
		run(`DELETE FROM people WHERE id IN ($1,$2)`, owner, winner)
	}()
	sponsorship := id(`INSERT INTO sponsorships(organization_id,name,level,status) VALUES($1,'Hackathon sponsor','Gold','Paid') RETURNING id::text`, org)
	run(`INSERT INTO sponsorships_conferences(sponsorship_id,conference_id) VALUES($1,$2)`, sponsorship, conf)
	run(`INSERT INTO organization_memberships(organization_id,person_id,role,status) VALUES($1,$2,'owner','active')`, org, owner)
	competition, err := getters.CreateCompetition(app, getters.CompetitionInput{ConferenceID: conf, Title: "Build on Bitcoin"})
	if err != nil {
		t.Fatal(err)
	}
	project, err := getters.CreateProject(app, getters.ProjectInput{CompetitionID: competition, CreatedByPersonID: winner, Title: "Lightning Garden", Slug: "garden-" + uuid.NewString()})
	if err != nil {
		t.Fatal(err)
	}
	award, err := getters.CreateAward(app, getters.AwardInput{CompetitionID: competition, SponsoredByOrgID: org, Title: "Best Bitcoin experience", Status: getters.AwardStatusAvailable})
	if err != nil {
		t.Fatal(err)
	}
	if err := getters.AssignProjectAward(app, award, project); err != nil {
		t.Fatal(err)
	}
	prize, err := getters.CreatePrize(app, getters.PrizeInput{AwardID: award, Title: "1M sats", ValueText: "1000000", PrizeType: getters.PrizeTypeSats})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := getters.PrepareCashPrizeDistributions(app, competition, award, project, prize); err != nil {
		t.Fatal(err)
	}
	run(`UPDATE competitions SET results_finalized_at=now() WHERE id=$1`, competition)
	distribution := id(`SELECT id::text FROM award_distributions WHERE prize_id=$1`, prize)
	request := func(method, endpoint string, values url.Values, loggedIn bool) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(method, endpoint, strings.NewReader(values.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		sessionCtx, err := app.Session.Load(r.Context(), "")
		if err != nil {
			t.Fatal(err)
		}
		r = r.WithContext(sessionCtx)
		app.Session.Put(sessionCtx, authMethodsCSRFKey, "payout-csrf")
		if loggedIn {
			if err := auth.LoginPerson(app, r, owner, auth.MethodEmailLink); err != nil {
				t.Fatal(err)
			}
		}
		r = mux.SetURLVars(r, map[string]string{"organizationID": org, "distributionID": distribution})
		w := httptest.NewRecorder()
		if method == "GET" {
			SponsorDashboard(w, r, app)
		} else {
			SponsorDashboardPayoutPaid(w, r, app)
		}
		return w
	}
	dashboardURL := "/dashboard/sponsor/" + org
	paidURL := dashboardURL + "/payouts/" + distribution + "/paid"
	valid := url.Values{"csrf": {"payout-csrf"}, "ConfirmPaid": {"yes"}, "PaymentReference": {"fixture-payment-hash"}}
	for _, method := range []string{"GET", "POST"} {
		w := request(method, dashboardURL, valid, false)
		if w.Code != http.StatusSeeOther || !strings.HasPrefix(w.Header().Get("Location"), "/login?") {
			t.Fatalf("anonymous %s was not redirected to login: %d", method, w.Code)
		}
	}
	w := request("GET", dashboardURL, nil, true)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "alex@example.test") || w.Header().Get("Cache-Control") != "private, no-store" {
		t.Fatalf("payout dashboard failed: %d %s", w.Code, w.Body.String())
	}
	for _, invalid := range []url.Values{
		{"ConfirmPaid": {"yes"}, "PaymentReference": {"fixture-payment-hash"}},
		{"csrf": {"wrong"}, "ConfirmPaid": {"yes"}, "PaymentReference": {"fixture-payment-hash"}},
		{"csrf": {"payout-csrf"}, "PaymentReference": {"fixture-payment-hash"}},
		{"csrf": {"payout-csrf"}, "ConfirmPaid": {"yes"}},
	} {
		if w := request("POST", paidURL, invalid, true); w.Code != http.StatusBadRequest {
			t.Fatalf("invalid confirmation accepted: %d %s", w.Code, w.Body.String())
		}
	}
	run(`UPDATE organization_memberships SET role='member' WHERE organization_id=$1`, org)
	if w := request("POST", paidURL, valid, true); w.Code != http.StatusForbidden {
		t.Fatalf("ordinary member confirmed payment: %d", w.Code)
	}
	run(`UPDATE organization_memberships SET role='manager' WHERE organization_id=$1`, org)
	for i := 0; i < 2; i++ {
		if w := request("POST", paidURL, valid, true); w.Code != http.StatusSeeOther || !strings.HasSuffix(w.Header().Get("Location"), "#payouts") {
			t.Fatalf("payment confirmation failed: %d %s", w.Code, w.Body.String())
		}
	}
	w = request("GET", dashboardURL, nil, true)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "No further payment is needed") || strings.Contains(w.Body.String(), "alex@example.test") {
		t.Fatal("confirmed payment still shows payment instructions")
	}
	if os.Getenv("SPONSOR_PAYOUT_PREVIEW") != "1" {
		return
	}
	// Optional local visual preview uses these synthetic records only.
	run(`UPDATE award_distributions SET status='pending', completed_at=NULL, completed_by=NULL WHERE id=$1`, distribution)
	root := mux.NewRouter()
	root.HandleFunc("/preview", func(w http.ResponseWriter, r *http.Request) {
		if err := auth.LoginPerson(app, r, owner, auth.MethodEmailLink); err != nil {
			http.Error(w, "Preview login failed", 500)
			return
		}
		http.Redirect(w, r, dashboardURL+"#payouts", http.StatusSeeOther)
	})
	root.HandleFunc("/dashboard/sponsor/{organizationID}", func(w http.ResponseWriter, r *http.Request) { SponsorDashboard(w, r, app) }).Methods("GET")
	root.HandleFunc("/dashboard/sponsor/{organizationID}/payouts/{distributionID}/paid", func(w http.ResponseWriter, r *http.Request) { SponsorDashboardPayoutPaid(w, r, app) }).Methods("POST")
	root.PathPrefix("/static/").Handler(http.StripPrefix("/static/", http.FileServer(http.Dir("static"))))
	server := &http.Server{Addr: "127.0.0.1:8113", Handler: app.Session.LoadAndSave(root), ReadHeaderTimeout: 5 * time.Second}
	stop, cancel := signal.NotifyContext(c, os.Interrupt, syscall.SIGTERM)
	defer cancel()
	go func() {
		<-stop.Done()
		shutdown, done := context.WithTimeout(c, 5*time.Second)
		defer done()
		server.Shutdown(shutdown)
	}()
	t.Log("Sponsor payout preview: http://127.0.0.1:8113/preview")
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		t.Fatal(err)
	}
}
