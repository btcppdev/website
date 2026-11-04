package handlers

import (
	"btcpp-web/external/getters"
	"btcpp-web/internal/auth"
	"btcpp-web/internal/config"
	"btcpp-web/internal/types"
	"context"
	"github.com/alexedwards/scs/v2"
	"github.com/jackc/pgx/v5/pgxpool"
	"io"
	"log"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
)

func TestDatabaseSmokeTemporaryPasswordLogin(t *testing.T) {
	if os.Getenv("BTCPP_POSTGRES_SMOKE") != "1" {
		t.Skip("requires isolated PostgreSQL test database")
	}
	pool, err := pgxpool.New(context.Background(), os.Getenv("DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	app := &config.AppContext{DB: pool, Session: scs.New(), Env: &types.EnvConfig{}, Err: log.New(io.Discard, "", 0), Infos: log.New(io.Discard, "", 0)}
	var person string
	if err := pool.QueryRow(context.Background(), `INSERT INTO people(name) VALUES('Temporary password test') RETURNING id::text`).Scan(&person); err != nil {
		t.Fatal(err)
	}
	defer pool.Exec(context.Background(), `DELETE FROM people WHERE id=$1::uuid`, person)
	email := "temporary-" + person + "@example.test"
	if _, err := pool.Exec(context.Background(), `INSERT INTO person_emails(person_id,email,is_primary) VALUES($1::uuid,$2,true)`, person, email); err != nil {
		t.Fatal(err)
	}
	hash, err := auth.HashPassword("temporary password for testing")
	if err != nil {
		t.Fatal(err)
	}
	if err := getters.IssueTemporaryPassword(app, person, hash); err != nil {
		t.Fatal(err)
	}
	login := httptest.NewRequest("POST", "/login/password", strings.NewReader(url.Values{"csrf": {"test-csrf"}, "Email": {email}, "Password": {"temporary password for testing"}, "Next": {"/dashboard"}}.Encode()))
	login.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	sessionCtx, err := app.Session.Load(login.Context(), "")
	if err != nil {
		t.Fatal(err)
	}
	login = login.WithContext(sessionCtx)
	app.Session.Put(sessionCtx, authMethodsCSRFKey, "test-csrf")
	w := httptest.NewRecorder()
	PasswordLogin(w, login, app)
	if w.Code != 303 || w.Header().Get("Location") != "/change-temporary-password" {
		t.Fatalf("temporary login: %d %s", w.Code, w.Header().Get("Location"))
	}
	if identity, err := auth.Resolve(login, app); identity != nil || err != nil {
		t.Fatal("temporary login granted account access", identity, err)
	}
	app.Session.Put(sessionCtx, authMethodsCSRFKey, "change-csrf")
	change := httptest.NewRequest("POST", "/change-temporary-password", strings.NewReader(url.Values{"csrf": {"change-csrf"}, "password": {"my own permanent password"}, "password_confirm": {"my own permanent password"}}.Encode())).WithContext(sessionCtx)
	change.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w = httptest.NewRecorder()
	ChangeTemporaryPassword(w, change, app)
	if w.Code != 303 || w.Header().Get("Location") != "/dashboard" {
		t.Fatalf("change: %d %s", w.Code, w.Body.String())
	}
	if app.Session.GetString(sessionCtx, auth.SessionPersonIDKey) != person || app.Session.GetString(sessionCtx, temporaryHashKey) != "" {
		t.Fatal("password change did not promote restricted session")
	}
	credential, err := getters.GetPersonPasswordCredential(app, person)
	if err != nil {
		t.Fatal(err)
	}
	if credential.MustChangePassword || !auth.CheckPassword(credential.PasswordHash, "my own permanent password") || auth.CheckPassword(credential.PasswordHash, "temporary password for testing") {
		t.Fatal("temporary credential not replaced")
	}
}
