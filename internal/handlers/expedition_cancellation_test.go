package handlers

import (
	"bytes"
	"context"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"

	"btcpp-web/external/getters"
	"btcpp-web/internal/auth"
	"github.com/alexedwards/scs/v2"
	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestExpeditionCancellationTemplate(t *testing.T) {
	app := antarcticaTestTemplates(t)
	page := expeditionCancellationPage{Booking: &getters.ExpeditionCancellation{ID: uuid.NewString(), State: "confirmed", Provider: "stripe", Quantity: 3, Eligible: true, Terms: "Accepted <terms>", Token: "snapshot"}, CanCancel: true, CSRF: "csrf", Paid: "$50,100", Deposit: "$10,020", RefundDue: "$40,080", Refunded: "$0"}
	var out bytes.Buffer
	if err := app.TemplateCache.ExecuteTemplate(&out, "admin/antarctica-bookings.tmpl", page); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"$40,080", "Cancel booking &amp; release berths", "does not send a refund", "Accepted &lt;terms&gt;", `name="csrf" value="csrf"`, `name="snapshot" value="snapshot"`} {
		if !strings.Contains(out.String(), want) {
			t.Fatal("missing cancellation information", want)
		}
	}
}

func TestExpeditionCancellationAuthorization(t *testing.T) {
	uri := os.Getenv("EXPEDITION_COMMERCE_TEST_DATABASE_URL")
	if uri == "" {
		t.Skip("isolated migrated PostgreSQL required")
	}
	app := antarcticaTestTemplates(t)
	app.Session = scs.New()
	app.Infos = app.Err
	pool, err := pgxpool.New(context.Background(), uri)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	app.DB = pool
	admin, traveler := uuid.NewString(), uuid.NewString()
	if _, err = pool.Exec(context.Background(), `INSERT INTO people(id,name) VALUES($1,'Cancellation admin'),($2,'Cancellation traveler')`, admin, traveler); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(context.Background(), `INSERT INTO people_roles(person_id,scope,position) VALUES($1,'global','admin')`, admin); err != nil {
		t.Fatal(err)
	}
	defer func() {
		pool.Exec(context.Background(), `DELETE FROM people_roles WHERE person_id=$1`, admin)
		pool.Exec(context.Background(), `DELETE FROM people WHERE id IN ($1,$2)`, admin, traveler)
	}()
	request := func(person, method, csrf string) *httptest.ResponseRecorder {
		t.Helper()
		values := url.Values{"csrf": {csrf}, "confirmed": {"yes"}, "reason": {"test"}}
		r := httptest.NewRequest(method, "/admin/antarctica/bookings", strings.NewReader(values.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		if method == "POST" {
			r = mux.SetURLVars(r, map[string]string{"booking": uuid.NewString()})
		}
		ctx, err := app.Session.Load(r.Context(), "")
		if err != nil {
			t.Fatal(err)
		}
		r = r.WithContext(ctx)
		if person != "" {
			if err = auth.LoginPerson(app, r, person, auth.MethodPassword); err != nil {
				t.Fatal(err)
			}
		}
		app.Session.Put(ctx, authMethodsCSRFKey, "expected-csrf")
		w := httptest.NewRecorder()
		AdminExpeditionBookings(w, r, app)
		return w
	}
	for _, person := range []string{"", traveler} {
		for _, method := range []string{"GET", "POST"} {
			w := request(person, method, "expected-csrf")
			if w.Code == 200 || w.Code == 503 {
				t.Fatal("non-admin reached bookings", w.Code, w.Body.String())
			}
		}
	}
	if w := request(admin, "POST", "wrong"); w.Code != 403 {
		t.Fatal("CSRF accepted", w.Code)
	}
	if w := request(admin, "GET", ""); w.Code != 200 {
		t.Fatal("admin listing", w.Code, w.Body.String())
	}
}
