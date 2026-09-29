package missives

import (
	"encoding/json"
	"html/template"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"regexp"
	"strings"
	"testing"

	"btcpp-web/internal/config"
	"btcpp-web/internal/types"
	mailer "github.com/base58btc/mailer/mail"
	"github.com/gorilla/mux"
)

func TestPublicNewsletterSignupForms(t *testing.T) {
	var delivered []mailer.MailRequest
	fail := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request mailer.MailRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
		}
		delivered = append(delivered, request)
		w.Header().Set("Content-Type", "application/json")
		if fail {
			io.WriteString(w, `{"success":false,"code":503,"message":"unavailable"}`)
		} else {
			io.WriteString(w, `{"success":true,"code":200}`)
		}
	}))
	defer server.Close()
	tmpl := template.Must(template.New("root").Parse(`{{define "emails/tmp.tmpl"}}{{.Content}}{{end}}`))
	tmpl = template.Must(tmpl.ParseFiles("../../templates/emails/confirm-sub.tmpl", "../../templates/section/ok.tmpl", "../../templates/section/err.tmpl"))
	// ParseFiles uses basenames; the application uses paths as template names.
	for _, pair := range [][2]string{{"emails/confirm-sub.tmpl", "confirm-sub.tmpl"}, {"section/ok.tmpl", "ok.tmpl"}, {"section/err.tmpl", "err.tmpl"}} {
		tmpl = template.Must(tmpl.AddParseTree(pair[0], tmpl.Lookup(pair[1]).Tree))
	}
	app := &config.AppContext{Env: &types.EnvConfig{Prod: true, Host: "btcpp.dev", MailEndpoint: server.URL, MailerSecret: "local-test"}, Infos: log.New(io.Discard, "", 0), Err: log.New(io.Discard, "", 0), TemplateCache: tmpl}
	router := mux.NewRouter()
	RegisterNewsletterHandlers(router, app)
	post := func(path, field, value string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", path, strings.NewReader(url.Values{field: {value}}.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)
		return w
	}
	for _, file := range []string{"embeds/index.tmpl", "section/generic_conf_page.tmpl", "section/maillist.tmpl"} {
		t.Run(file, func(t *testing.T) {
			raw, err := os.ReadFile("../../templates/" + file)
			if err != nil {
				t.Fatal(err)
			}
			form := regexp.MustCompile(`(?s)<form[^>]*hx-post="[^"]*subscribe".*?</form>`).Find(raw)
			path := regexp.MustCompile(`hx-post="([^"]+)"`).FindSubmatch(form)
			field := regexp.MustCompile(`(?s)<input[^>]*type="email"[^>]*name="([^"]+)"`).FindSubmatch(form)
			if len(path) != 2 || len(field) != 2 {
				t.Fatal("newsletter form not found")
			}
			if !strings.Contains(string(form), `method="post"`) || !strings.Contains(string(form), `action="`+string(path[1])+`"`) {
				t.Fatal("native form fallback missing")
			}
			before := len(delivered)
			w := post(string(path[1]), string(field[1]), "signup@example.test")
			if w.Code != 200 || !strings.Contains(w.Body.String(), "Subscription confirmation sent") || len(delivered) != before+1 {
				t.Fatalf("signup failed: %d %s", w.Code, w.Body.String())
			}
			mail := delivered[len(delivered)-1]
			if mail.ToAddr != "signup@example.test" {
				t.Fatalf("wrong recipient %s", mail.ToAddr)
			}
			token := regexp.MustCompile(`/confirm/([a-zA-Z0-9-]+)`).FindStringSubmatch(mail.TextBody)
			if len(token) != 2 {
				t.Fatal("confirmation link missing")
			}
			parsed, err := ParseSubscribeToken(app.Env.HMACKey[:], token[1])
			if err != nil || parsed.Email != "signup@example.test" || parsed.Newsletter != "newsletter" {
				t.Fatalf("invalid confirmation: %+v %v", parsed, err)
			}
		})
	}
	before := len(delivered)
	w := post("/newsletter/subscribe", "newsletter-email", "invalid")
	if len(delivered) != before || !strings.Contains(w.Body.String(), "not a valid email") {
		t.Fatal("invalid input sent mail")
	}
	fail = true
	w = post("/newsletter/subscribe", "newsletter-email", "signup@example.test")
	if !strings.Contains(w.Body.String(), "Unable to subscribe") || strings.Contains(w.Body.String(), "confirmation sent") {
		t.Fatal("mailer failure reported as success")
	}
}
