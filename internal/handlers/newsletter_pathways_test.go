package handlers

import (
	"context"
	"encoding/json"
	"html/template"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"

	"btcpp-web/internal/config"
	"btcpp-web/internal/types"
	mailer "github.com/base58btc/mailer/mail"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestNewsletterCheckboxDecoding(t *testing.T) {
	for _, tc := range []struct {
		values []string
		want   bool
	}{{nil, false}, {[]string{"on"}, true}, {[]string{"false"}, false}, {[]string{"false", "on"}, true}} {
		var form struct{ Subscribe bool }
		if err := newFormDecoder().Decode(&form, url.Values{"Subscribe": tc.values}); err != nil {
			t.Fatal(err)
		}
		if form.Subscribe != tc.want {
			t.Fatalf("%v decoded as %t", tc.values, form.Subscribe)
		}
	}
}

func TestSponsorInquiryNewsletterOptInPostgres(t *testing.T) {
	if os.Getenv("BTCPP_POSTGRES_SMOKE") != "1" {
		t.Skip("requires isolated local PostgreSQL")
	}
	pool, err := pgxpool.New(context.Background(), os.Getenv("DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if h := pool.Config().ConnConfig.Host; h != "127.0.0.1" && h != "localhost" {
		t.Fatal("requires localhost database")
	}
	var sent []mailer.MailRequest
	failConfirm := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var mail mailer.MailRequest
		if err := json.NewDecoder(r.Body).Decode(&mail); err != nil {
			t.Error(err)
		}
		sent = append(sent, mail)
		if failConfirm && strings.Contains(mail.TextBody, "/confirm/") {
			io.WriteString(w, `{"success":false,"code":503,"message":"unavailable"}`)
		} else {
			io.WriteString(w, `{"success":true,"code":200}`)
		}
	}))
	defer server.Close()
	tmpl := template.Must(template.New("root").Parse(`{{define "emails/tmp.tmpl"}}{{.Content}}{{end}}`))
	// Use the actual confirmation template so this test checks the generated link.
	raw, err := os.ReadFile("../../templates/emails/confirm-sub.tmpl")
	if err != nil {
		t.Fatal(err)
	}
	tmpl = template.Must(tmpl.New("emails/confirm-sub.tmpl").Parse(string(raw)))
	app := &config.AppContext{DB: pool, Env: &types.EnvConfig{Prod: true, Host: "btcpp.dev", MailEndpoint: server.URL, MailerSecret: "test"}, TemplateCache: tmpl, Infos: log.New(io.Discard, "", 0), Err: log.New(io.Discard, "", 0)}
	for _, tc := range []struct {
		optIn, fail bool
		want        int
	}{{false, false, 2}, {true, false, 3}, {true, true, 3}} {
		sent = nil
		failConfirm = tc.fail
		data := url.Values{"Name": {"Test Sponsor"}, "Email": {"sponsor@example.test"}, "Org": {"Test"}, "Captcha": {"5"}}
		if tc.optIn {
			data.Set("Subscribe", "on")
		}
		r := httptest.NewRequest("POST", "/sponsor", strings.NewReader(data.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()
		SponsorPage(w, r, app)
		if len(sent) != tc.want {
			t.Fatalf("optIn=%t fail=%t: sent %d want %d; response %s", tc.optIn, tc.fail, len(sent), tc.want, w.Body.String())
		}
		if tc.optIn && !strings.Contains(sent[2].TextBody, "/confirm/") {
			t.Fatal("confirmation link missing")
		}
		if tc.fail && !strings.Contains(w.Body.String(), "confirmation could not be sent") {
			t.Fatalf("failure hidden: %s", w.Body.String())
		}
	}
}
