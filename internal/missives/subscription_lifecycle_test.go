package missives

import (
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	texttemplate "text/template"
	"time"

	"btcpp-web/external/getters"
	"btcpp-web/internal/config"
	"btcpp-web/internal/helpers"
	"btcpp-web/internal/types"
	mailer "github.com/base58btc/mailer/mail"
	"github.com/gorilla/mux"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestSubscribeTokenRejectsMalformedTimestamp(t *testing.T) {
	for _, token := range []string{"x-61-62-", "x-61-62-00", "x-61-62-000000000000000000", "x-61-62-zz"} {
		if _, err := ParseSubscribeToken([]byte("secret"), token); err == nil {
			t.Fatalf("accepted %q", token)
		}
	}
}

func TestSubscriptionLifecyclePostgres(t *testing.T) {
	if os.Getenv("BTCPP_POSTGRES_SMOKE") != "1" {
		t.Skip("requires isolated local PostgreSQL")
	}
	pool, err := pgxpool.New(context.Background(), os.Getenv("DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if h := pool.Config().ConnConfig.Host; h != "127.0.0.1" && h != "localhost" {
		t.Fatal("test requires localhost database")
	}
	failMail, deliveries, cancellations := false, 0, 0
	var jobKeys []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "DELETE" {
			cancellations++
		} else {
			deliveries++
			var mail mailer.MailRequest
			if err := json.NewDecoder(r.Body).Decode(&mail); err != nil {
				t.Error(err)
			}
			jobKeys = append(jobKeys, mail.JobKey)
		}
		if failMail {
			io.WriteString(w, `{"success":false,"code":503,"message":"unavailable"}`)
		} else {
			io.WriteString(w, `{"success":true,"code":200}`)
		}
	}))
	defer server.Close()
	tmpl := template.Must(template.New("root").Parse(`{{define "emails/tmp.tmpl"}}{{.Content}}{{end}}{{define "emails/subscribe_ok.tmpl"}}Subscribed{{end}}{{define "emails/unsubscribe_ok.tmpl"}}Unsubscribed{{end}}`))
	app := &config.AppContext{DB: pool, EmailCache: make(map[string]*texttemplate.Template), Env: &types.EnvConfig{Prod: true, Host: "btcpp.dev", MailEndpoint: server.URL, MailerSecret: "test"}, TemplateCache: tmpl, Infos: log.New(io.Discard, "", 0), Err: log.New(io.Discard, "", 0)}
	suffix := fmt.Sprint(time.Now().UnixNano())
	list := "audit-" + suffix
	email := "audit-" + suffix + "@example.test"
	defer pool.Exec(context.Background(), `DELETE FROM subscribers WHERE email LIKE $1`, "%"+suffix+"@example.test")
	defer pool.Exec(context.Background(), `DELETE FROM missives WHERE $1 = ANY(newsletters)`, list)
	if err := getters.CreateMissive(app, "Audit welcome", "Welcome!", "onsub", []string{list}); err != nil {
		t.Fatal(err)
	}
	router := mux.NewRouter()
	RegisterNewsletterHandlers(router, app)
	_, token := helpers.GetSubscribeToken(app.Env.HMACKey[:], email, list, uint64(time.Now().UnixNano()))
	visit := func(path string, want int) {
		t.Helper()
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != want {
			t.Fatalf("%s: %d %s", path, w.Code, w.Body.String())
		}
	}
	check := func(address string, lists ...string) {
		t.Helper()
		sub, err := getters.FindSubscriber(app, address)
		if err != nil || sub == nil {
			t.Fatalf("lookup: %v", err)
		}
		if len(sub.Subs) != len(lists) {
			t.Fatalf("got %d memberships; want %v", len(sub.Subs), lists)
		}
		for _, name := range lists {
			found := false
			for _, s := range sub.Subs {
				if s.Name == name {
					found = true
				}
			}
			if !found {
				t.Fatalf("missing %s", name)
			}
		}
	}
	visit("/confirm/"+token, 200)
	check(email, list)
	if deliveries != 1 {
		t.Fatalf("new subscriber welcome deliveries=%d", deliveries)
	}
	visit("/confirm/"+token, 200)
	check(email, list)
	if len(jobKeys) != 2 || jobKeys[0] == "" || jobKeys[0] != jobKeys[1] {
		t.Fatalf("repeat confirmation did not reuse dedupe key: %v", jobKeys)
	}
	failMail = true
	visit("/newsletter/unsubscribe/"+token, 503)
	check(email)
	failMail = false
	visit("/newsletter/unsubscribe/"+token, 200)
	if cancellations != 2 {
		t.Fatalf("cancellation was not retried: %d", cancellations)
	}
	failMail = true
	visit("/confirm/"+token, 503)
	check(email, list)
	failMail = false
	visit("/confirm/"+token, 200)
	check(email, list)
	visit("/confirm/x-61-62-00", 303)
	for _, optIn := range []bool{false, true} {
		address := fmt.Sprintf("ticket-%t-%s@example.test", optIn, suffix)
		if err := NewTicketSub(app, address, list, "genpop", optIn); err != nil {
			t.Fatal(err)
		}
		want := []string{list, "genpop", list + "-genpop"}
		if optIn {
			want = append(want, "newsletter")
		}
		check(address, want...)
		// Repeat purchases preserve memberships and do not create duplicates.
		if err := NewTicketSub(app, address, list, "genpop", false); err != nil {
			t.Fatal(err)
		}
		check(address, want...)
		for _, kind := range []string{"talkapp", "volapp"} {
			address := fmt.Sprintf("%s-%t-%s@example.test", kind, optIn, suffix)
			lists := MakeApplicationSublist(list, kind, optIn)
			if err := NewSubs(app, address, lists); err != nil {
				t.Fatal(err)
			}
			check(address, lists...)
		}
	}
}
