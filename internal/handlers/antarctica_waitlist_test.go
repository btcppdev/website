package handlers

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"btcpp-web/external/getters"
	"btcpp-web/internal/auth"
	"btcpp-web/internal/emails"
	"btcpp-web/internal/expedition"
	"github.com/alexedwards/scs/v2"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestAntarcticaSoldOut(t *testing.T) {
	app := antarcticaTestTemplates(t)
	page := newAntarcticaPage(nil, false)
	page.RemainingBerths = 0
	var out bytes.Buffer
	if err := app.TemplateCache.ExecuteTemplate(&out, "conf/antarctica-checkout.tmpl", page); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "Join the waitlist") || strings.Contains(out.String(), `id="expedition-checkout"`) {
		t.Fatal("sold-out checkout must offer waitlist instead of booking form")
	}
	out.Reset()
	if err := app.TemplateCache.ExecuteTemplate(&out, "conf/antarctica-waitlist.tmpl", antarcticaWaitlistPage{Email: "owner@example.test", CSRF: "test"}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), `name="consent"`) || !strings.Contains(out.String(), `name="action" value="join"`) {
		t.Fatal("joining must work without a separate consent checkbox")
	}
}

func TestAntarcticaWaitlistPostgres(t *testing.T) {
	uri := os.Getenv("EXPEDITION_COMMERCE_TEST_DATABASE_URL")
	if uri == "" {
		t.Skip("requires isolated migrated database")
	}
	app := antarcticaTestTemplates(t)
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, uri)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	app.DB = pool
	app.Session = scs.New()
	defer enableAntarcticaSalesForTest(t, app)()
	app.Env.StripeKey = "sk_test_fixture"
	app.Env.MailEndpoint = "http://never-contacted.invalid"
	app.Env.Host = "localhost"
	policy := getters.ServiceLegalPolicy{ProductCode: expedition.ProductCode, Version: uuid.NewString(), TermsText: "Fixture", PrivacyText: "Fixture", EffectiveAt: time.Now().UTC().Truncate(time.Microsecond)}
	if err = getters.RegisterServiceLegalPolicy(app, policy); err != nil {
		t.Fatal(err)
	}
	if err = getters.ActivateServiceLegalPolicy(app, policy.ProductCode, policy.Version); err != nil {
		t.Fatal(err)
	}
	var originalStaff int
	pool.QueryRow(ctx, `SELECT staff_berths FROM expedition_settings WHERE singleton`).Scan(&originalStaff)
	defer pool.Exec(ctx, `UPDATE expedition_settings SET staff_berths=$1 WHERE singleton`, originalStaff)
	if _, err = pool.Exec(ctx, `UPDATE expedition_settings SET staff_berths=25 WHERE singleton`); err != nil {
		t.Fatal(err)
	}
	var person, contact string
	if err = pool.QueryRow(ctx, `INSERT INTO people(name) VALUES('Waitlist fixture') RETURNING id::text`).Scan(&person); err != nil {
		t.Fatal(err)
	}
	defer pool.Exec(ctx, `DELETE FROM people WHERE id=$1`, person)
	email := person + "@example.test"
	if err = pool.QueryRow(ctx, `INSERT INTO person_emails(person_id,email,is_primary,verified_at) VALUES($1,$2,true,now()) RETURNING contact_id::text`, person, email).Scan(&contact); err != nil {
		t.Fatal(err)
	}
	router := http.NewServeMux()
	router.HandleFunc("/login-fixture", func(w http.ResponseWriter, r *http.Request) {
		if e := auth.LoginPerson(app, r, person, auth.MethodPassword); e != nil {
			t.Error(e)
		}
		w.WriteHeader(204)
	})
	router.HandleFunc("/waitlist", func(w http.ResponseWriter, r *http.Request) { AntarcticaWaitlist(w, r, app) })
	server := httptest.NewServer(app.Session.LoadAndSave(router))
	defer server.Close()
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	res, err := client.Get(server.URL + "/waitlist")
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != 303 {
		t.Fatal("anonymous must verify account", res.StatusCode)
	}
	res, err = client.Get(server.URL + "/login-fixture")
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	res, err = client.Get(server.URL + "/waitlist")
	if err != nil {
		t.Fatal(err)
	}
	var html bytes.Buffer
	html.ReadFrom(res.Body)
	res.Body.Close()
	m := regexp.MustCompile(`name="csrf" value="([^"]+)"`).FindStringSubmatch(html.String())
	if len(m) != 2 {
		t.Fatal(res.StatusCode, html.String())
	}
	csrf := m[1]
	post := func(token, action string) int {
		t.Helper()
		res, e := client.PostForm(server.URL+"/waitlist", url.Values{"csrf": {token}, "action": {action}})
		if e != nil {
			t.Fatal(e)
		}
		res.Body.Close()
		return res.StatusCode
	}
	if code := post("wrong", "join"); code != 403 {
		t.Fatal("CSRF", code)
	}
	if code := post(csrf, "join"); code != 303 {
		t.Fatal("join", code)
	}
	var key string
	pool.QueryRow(ctx, `SELECT notification_key::text FROM expedition_waitlist WHERE person_id=$1`, person).Scan(&key)
	if code := post(csrf, "join"); code != 303 {
		t.Fatal("replay", code)
	}
	var retryKey string
	pool.QueryRow(ctx, `SELECT notification_key::text FROM expedition_waitlist WHERE person_id=$1`, person).Scan(&retryKey)
	if retryKey != key {
		t.Fatal("duplicate signup changed notification key")
	}
	sends := 0
	send := func(m *emails.Mail) error {
		sends++
		if m.Email != email || m.JobKey != "antarctica-waitlist:"+key || !strings.Contains(string(m.TextBody), "not a reservation") {
			t.Error("incorrect notification", m)
		}
		return nil
	}
	if sent, e := processAntarcticaWaitlist(ctx, app, send); sent || e != nil {
		t.Fatal("sold-out send", sent, e)
	}
	pool.Exec(ctx, `UPDATE expedition_settings SET staff_berths=3 WHERE singleton`)
	pool.Exec(ctx, `INSERT INTO expedition_inventory(berth,capacity,sales_open,booking_closes_at,hold_seconds) VALUES('shared',18,true,'2028-01-01',3600) ON CONFLICT(berth) DO UPDATE SET sales_open=true,booking_closes_at='2028-01-01'`)
	app.Env.MailOff = true
	if sent, e := processAntarcticaWaitlist(ctx, app, send); sent || e != nil {
		t.Fatal("disabled mail consumed signup", sent, e)
	}
	app.Env.MailOff = false
	if sent, e := processAntarcticaWaitlist(ctx, app, func(m *emails.Mail) error {
		if m.JobKey != "antarctica-waitlist:"+key {
			t.Error("unstable job key")
		}
		return errors.New("ambiguous delivery")
	}); !sent || e == nil {
		t.Fatal("failed delivery", sent, e)
	}
	if sent, e := processAntarcticaWaitlist(ctx, app, send); !sent || e != nil {
		t.Fatal("retry", sent, e)
	}
	if sent, e := processAntarcticaWaitlist(ctx, app, send); sent || e != nil || sends != 1 {
		t.Fatal("duplicate send", sent, e, sends)
	}
	// A retry of the original join cannot reset an already-sent alert.
	pool.Exec(ctx, `UPDATE expedition_settings SET staff_berths=25 WHERE singleton`)
	if code := post(csrf, "join"); code != 303 {
		t.Fatal(code)
	}
	var notified bool
	pool.QueryRow(ctx, `SELECT notified_at IS NOT NULL FROM expedition_waitlist WHERE person_id=$1`, person).Scan(&notified)
	if !notified {
		t.Fatal("join retry reset notification")
	}
	if code := post(csrf, "leave"); code != 303 {
		t.Fatal("leave", code)
	}
	pool.Exec(ctx, `UPDATE expedition_settings SET staff_berths=3 WHERE singleton`)
	if sent, e := processAntarcticaWaitlist(ctx, app, send); sent || e != nil {
		t.Fatal("unsubscribed send", sent, e)
	}
	if e := joinAntarcticaWaitlist(ctx, app, person, contact); !errors.Is(e, expedition.ErrConflict) {
		t.Fatal("available expedition allowed join", e)
	}
}
