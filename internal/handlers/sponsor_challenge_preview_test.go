package handlers

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"testing"
	"time"

	"btcpp-web/external/getters"
	"btcpp-web/internal/auth"
	"btcpp-web/internal/config"
	"btcpp-web/internal/db"
	"btcpp-web/internal/types"
	"github.com/alexedwards/scs/v2"
	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Explicitly opt-in, local-only sandbox using an isolated disposable schema.
func TestSponsorChallengePreview(t *testing.T) {
	if os.Getenv("SPONSOR_CHALLENGE_PREVIEW") != "1" {
		t.Skip("local preview only")
	}
	c := context.Background()
	base, err := pgxpool.New(c, os.Getenv("DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	defer base.Close()
	schema := "sponsor_preview_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, err = base.Exec(c, "CREATE SCHEMA "+schema); err != nil {
		t.Fatal(err)
	}
	defer base.Exec(c, "DROP SCHEMA "+schema+" CASCADE")
	cfg, err := pgxpool.ParseConfig(os.Getenv("DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = schema + ",public"
	pool, err := pgxpool.NewWithConfig(c, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	wd, _ := os.Getwd()
	if err = os.Chdir("../.."); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(wd)
	migrations, err := db.LoadMigrations("db/migrations")
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range migrations {
		if _, err = pool.Exec(c, m.SQL); err != nil {
			t.Fatalf("migration %s: %v", m.Name, err)
		}
	}
	app := &config.AppContext{DB: pool, Session: scs.New(), Env: &types.EnvConfig{MailOff: true}, Err: log.New(os.Stderr, "preview: ", 0), Infos: log.New(os.Stdout, "preview: ", 0)}
	app.Session.Cookie.Name = "sponsor_challenge_preview"
	if err = loadTemplates(app); err != nil {
		t.Fatal(err)
	}
	id := func(q string, args ...any) string {
		t.Helper()
		var s string
		if e := pool.QueryRow(c, q, args...).Scan(&s); e != nil {
			t.Fatal(e)
		}
		return s
	}
	run := func(q string, args ...any) {
		t.Helper()
		if _, e := pool.Exec(c, q, args...); e != nil {
			t.Fatal(e)
		}
	}
	person, err := getters.CreateSpeaker(app, getters.SpeakerInput{Name: "Sponsor Preview", Email: "sponsor-preview@example.test"})
	if err != nil {
		t.Fatal(err)
	}
	conf := id(`INSERT INTO conferences(tag,active,description,publication_status,start_date,end_date,timezone) VALUES('preview26',true,'bitcoin++ Sponsor Preview','published',now()+interval '30 days',now()+interval '32 days','UTC') RETURNING id::text`)
	org := id(`INSERT INTO organizations(name,tagline) VALUES('Example Bitcoin Builders','Building better tools for Bitcoin') RETURNING id::text`)
	sponsorship := id(`INSERT INTO sponsorships(organization_id,name,level,status) VALUES($1,'Hackathon sponsor','Gold','Paid') RETURNING id::text`, org)
	run(`INSERT INTO sponsorships_conferences(sponsorship_id,conference_id) VALUES($1,$2)`, sponsorship, conf)
	run(`INSERT INTO organization_memberships(organization_id,person_id,role,status) VALUES($1,$2,'owner','active')`, org, person)
	run(`INSERT INTO sponsorship_entitlements(sponsorship_id,conference_id,ticket_allocation,sponsor_award_limit) VALUES($1,$2,20,3)`, sponsorship, conf)
	if _, err = getters.CreateCompetition(app, getters.CompetitionInput{ConferenceID: conf, Title: "Build on Bitcoin", Visibility: "public"}); err != nil {
		t.Fatal(err)
	}
	root := mux.NewRouter()
	login := func(w http.ResponseWriter, r *http.Request) {
		if e := auth.LoginPerson(app, r, person, auth.MethodEmailLink); e != nil {
			http.Error(w, e.Error(), 500)
			return
		}
		http.Redirect(w, r, "/dashboard/sponsor/"+org+"#sponsorships", 303)
	}
	root.HandleFunc("/preview/login", login).Methods("GET")
	root.HandleFunc("/login", login).Methods("GET")
	root.HandleFunc("/dashboard/sponsor", login).Methods("GET")
	root.HandleFunc("/dashboard/sponsor/{organizationID}", func(w http.ResponseWriter, r *http.Request) { SponsorDashboard(w, r, app) }).Methods("GET")
	root.HandleFunc("/dashboard/sponsor/{organizationID}/prize-proposals", func(w http.ResponseWriter, r *http.Request) { SponsorDashboardPrizeProposalCreate(w, r, app) }).Methods("POST")
	root.HandleFunc("/dashboard/sponsor/{organizationID}/prize-proposals/{proposalID}", func(w http.ResponseWriter, r *http.Request) { SponsorDashboardPrizeProposalUpdate(w, r, app) }).Methods("POST")
	root.PathPrefix("/static/").Handler(http.StripPrefix("/static/", http.FileServer(http.Dir("static"))))
	server := &http.Server{Addr: "127.0.0.1:8112", Handler: app.Session.LoadAndSave(root), ReadHeaderTimeout: 10 * time.Second}
	stop, cancel := signal.NotifyContext(c, os.Interrupt, syscall.SIGTERM)
	defer cancel()
	go func() {
		<-stop.Done()
		ctx, done := context.WithTimeout(c, 5*time.Second)
		defer done()
		server.Shutdown(ctx)
	}()
	t.Log("Preview login: http://127.0.0.1:8112/preview/login")
	if err = server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		t.Fatal(err)
	}
}
