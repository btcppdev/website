package handlers

import (
	"context"
	htmltemplate "html/template"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	texttemplate "text/template"
	"time"

	"btcpp-web/external/getters"
	"btcpp-web/internal/config"
	"btcpp-web/internal/types"
	"github.com/alexedwards/scs/v2"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestEmailLoginRetryDestinationPostgres(t *testing.T) {
	if os.Getenv("BTCPP_POSTGRES_SMOKE") != "1" {
		t.Skip("requires local postgres")
	}
	pool, err := pgxpool.New(context.Background(), os.Getenv("DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	id := uuid.NewString()
	_, err = pool.Exec(context.Background(), `INSERT INTO missives (id, public_uid, title, only_for, markdown) VALUES ($1, 9223372036854775806, 'Login fixture', 'vollogin', '{{.VolShiftLink}}')`, id)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Exec(context.Background(), `DELETE FROM missives WHERE id=$1`, id)
	for _, path := range []string{"/login", "/auth/resend"} {
		for _, throttled := range []bool{false, true} {
			for _, destination := range []string{"/oauth/authorize?client_id=alerts&state=return%2Bstate", "https://untrusted.example/path"} {
				t.Run(path+"/"+destination+"/throttled="+map[bool]string{true: "yes", false: "no"}[throttled], func(t *testing.T) {
					manager := scs.New()
					requestContext, err := manager.Load(context.Background(), "")
					if err != nil {
						t.Fatal(err)
					}
					manager.Put(requestContext, authMethodsCSRFKey, "fixture-csrf")
					app := &config.AppContext{DB: pool, Session: manager, Env: &types.EnvConfig{MailOff: true}, Infos: log.New(io.Discard, "", 0), Err: log.New(io.Discard, "", 0), EmailCache: make(map[string]*texttemplate.Template), TemplateCache: htmltemplate.Must(htmltemplate.New("emails/tmp.tmpl").Parse("{{.Content}}"))}
					// Isolate rate limits from other tests and previous table cases.
					copy(app.Env.HMACKey[:], uuid.NewString())
					email := uuid.NewString() + "@example.test"
					defer pool.Exec(context.Background(), `DELETE FROM magic_login_tokens WHERE email=$1`, email)
					want := destination
					if strings.HasPrefix(destination, "https:") {
						want = "/dashboard"
					}
					form := url.Values{"Email": {email}, "Next": {destination}, "csrf": {"fixture-csrf"}}
					if path == "/auth/resend" {
						token, err := getters.CreateMagicLoginToken(app, email, want)
						if err != nil {
							t.Fatal(err)
						}
						form.Set("token", token)
					}
					request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode())).WithContext(requestContext)
					request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
					if throttled {
						maximum, window := 10, time.Minute
						if path == "/auth/resend" {
							maximum, window = 5, 15*time.Minute
						}
						for i := 0; i < maximum; i++ {
							if !allowAuthAttempt(app, request, email, maximum, window) {
								t.Fatal("fixture unexpectedly throttled")
							}
						}
					}
					response := httptest.NewRecorder()
					if path == "/login" {
						Login(response, request, app)
					} else {
						MagicLinkResend(response, request, app)
					}
					location, err := url.Parse(response.Header().Get("Location"))
					if err != nil || response.Code != http.StatusSeeOther || location.Path != "/login" || location.Query().Get("next") != want || location.Query().Get("flash") == "" {
						t.Fatalf("retry destination lost: status=%d location=%s body=%s", response.Code, response.Header().Get("Location"), response.Body.String())
					}
					var count int
					if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM magic_login_tokens WHERE email=$1 AND next_path=$2`, email, want).Scan(&count); err != nil {
						t.Fatal(err)
					}
					expected := 0
					if path == "/auth/resend" {
						expected++
					}
					if !throttled {
						expected++
					}
					if count != expected {
						t.Fatalf("tokens with correct destination=%d want %d", count, expected)
					}
				})
			}
		}
	}
}
