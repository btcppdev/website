package handlers

import (
	"btcpp-web/external/getters"
	"btcpp-web/internal/auth"
	"context"
	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestOrganizationShopAccess(t *testing.T) {
	app := wizardTestApp(t)
	c := context.Background()
	org := uuid.NewString()
	other := uuid.NewString()
	for _, id := range []string{org, other} {
		if _, e := app.DB.Exec(c, `INSERT INTO organizations(id,name) VALUES($1,$2)`, id, "Shop access "+id); e != nil {
			t.Fatal(e)
		}
	}
	person, e := getters.CreateSpeaker(app, getters.SpeakerInput{Name: "Shop manager", Email: "shop-" + org + "@example.test"})
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		app.DB.Exec(c, `DELETE FROM organization_shop_profiles WHERE organization_id=$1`, org)
		app.DB.Exec(c, `DELETE FROM organizations WHERE id::text=ANY($1)`, []string{org, other})
		app.DB.Exec(c, `DELETE FROM people WHERE id=$1`, person)
	})
	if _, e = app.DB.Exec(c, `INSERT INTO organization_memberships(organization_id,person_id,role) VALUES($1,$2,'manager')`, org, person); e != nil {
		t.Fatal(e)
	}
	router := mux.NewRouter()
	registerOrganizationShops(router, app)
	request := func(path string, form url.Values) *httptest.ResponseRecorder {
		method := "GET"
		if form != nil {
			method = "POST"
		}
		r := httptest.NewRequest(method, path, strings.NewReader(form.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()
		app.Session.LoadAndSave(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if e := auth.LoginPerson(app, r, person, auth.MethodEmailLink); e != nil {
				t.Fatal(e)
			}
			app.Session.Put(r.Context(), authMethodsCSRFKey, "shop-test-token")
			router.ServeHTTP(w, r)
		})).ServeHTTP(w, r)
		return w
	}
	base := "/dashboard/orgs/" + org + "/shop"
	for _, tab := range []string{"profile", "catalog", "product", "orders", "discounts"} {
		if w := request(base+"?tab="+tab, nil); w.Code != 200 || !strings.Contains(w.Body.String(), "Sold by bitcoin++") {
			t.Fatalf("render %s %d %s", tab, w.Code, w.Body.String())
		}
	}
	if w := request(base, nil); !strings.Contains(w.Body.String(), `aria-label="Dashboard sections"`) || !strings.Contains(w.Body.String(), `aria-current="page">/shop`) {
		t.Fatal("shop is missing the shared workspace navigation", w.Body.String())
	}
	if w := request("/dashboard/orgs/"+other+"/shop", nil); w.Code != 403 {
		t.Fatal("foreign org readable", w.Code)
	}
	if w := request(base, url.Values{"action": {"profile"}, "csrf": {"wrong"}}); w.Code != 403 {
		t.Fatal("CSRF bypass", w.Code)
	}
	form := url.Values{"action": {"profile"}, "csrf": {"shop-test-token"}, "revision": {"0"}, "ship_contact": {"Test"}, "ship_email": {"test@example.test"}, "ship_phone": {"+15550000000"}, "ship_line1": {"1 Test St"}, "ship_city": {"Austin"}, "ship_region": {"TX"}, "ship_postal_code": {"78701"}, "ship_country": {"US"}, "billing_name": {"Test"}, "billing_line1": {"1 Test St"}, "billing_city": {"Austin"}, "billing_postal_code": {"78701"}, "billing_country": {"US"}}
	if w := request(base, form); w.Code != 200 {
		t.Fatal("save", w.Code, w.Body.String())
	}
	if w := request(base, form); w.Code != 409 {
		t.Fatal("stale save", w.Code, w.Body.String())
	}
	if _, e = app.DB.Exec(c, `UPDATE organization_memberships SET role='member' WHERE person_id=$1`, person); e != nil {
		t.Fatal(e)
	}
	if w := request(base, nil); w.Code != 403 {
		t.Fatal("member access", w.Code)
	}
	if _, e = app.DB.Exec(c, `UPDATE organization_memberships SET role='manager',status='removed' WHERE person_id=$1`, person); e != nil {
		t.Fatal(e)
	}
	if w := request(base, form); w.Code != 403 {
		t.Fatal("removed manager write", w.Code)
	}
	if _, e = app.DB.Exec(c, `INSERT INTO people_roles(person_id,scope,position) VALUES($1,'global','admin')`, person); e != nil {
		t.Fatal(e)
	}
	if w := request("/dashboard/orgs/"+other+"/shop", nil); w.Code != 200 || !strings.Contains(w.Body.String(), "Organization workspace · admin") {
		t.Fatal("global admin without membership cannot view shop workspace", w.Code, w.Body.String())
	}
}
