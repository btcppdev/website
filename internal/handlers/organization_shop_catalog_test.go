package handlers

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"btcpp-web/external/getters"
	"btcpp-web/internal/auth"
	"github.com/google/uuid"
	"github.com/gorilla/mux"
)

func TestOrganizationShopCatalogBrandingAndCategories(t *testing.T) {
	app := wizardTestApp(t)
	c := context.Background()
	org := uuid.NewString()
	name := "Merch partner " + org
	logo := "/static/img/partner-logo.svg"
	var productID, person string
	t.Cleanup(func() {
		app.DB.Exec(c, `DELETE FROM merch_products WHERE id::text=$1`, productID)
		app.DB.Exec(c, `DELETE FROM organizations WHERE id=$1`, org)
		app.DB.Exec(c, `DELETE FROM people WHERE id::text=$1`, person)
	})
	if _, err := app.DB.Exec(c, `INSERT INTO organizations(id,name,logo_light_url,notes) VALUES($1,$2,$3,'private supplier note')`, org, name, logo); err != nil {
		t.Fatal(err)
	}
	var err error
	person, err = getters.CreateSpeaker(app, getters.SpeakerInput{Name: "Merch category manager", Email: org + "@example.test"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = app.DB.Exec(c, `INSERT INTO organization_memberships(organization_id,person_id,role) VALUES($1,$2,'manager')`, org, person); err != nil {
		t.Fatal(err)
	}
	in := getters.OrganizationProductInput{MerchProductInput: getters.MerchProductInput{Slug: "catalog-" + org, Name: "Partner tee", ProductType: "apparel", Status: "draft", BasePriceCents: 4000, Currency: "USD", StripeTaxCode: "txcd_99999999", CountryOfOrigin: "US"}, OnlineEnabled: true}
	productID, err = getters.SaveOrganizationProduct(app, org, in)
	if err != nil {
		t.Fatal(err)
	}
	before, err := getters.GetMerchProductByID(app, productID)
	if err != nil {
		t.Fatal(err)
	}
	root := mux.NewRouter()
	registerOrganizationShops(root, app)
	root.HandleFunc("/admin/merch/{id}", func(w http.ResponseWriter, r *http.Request) { AdminMerchUpdate(w, r, app) }).Methods("POST")
	root.HandleFunc("/shop/all", func(w http.ResponseWriter, r *http.Request) { ShopCollection(w, r, app) }).Methods("GET")
	root.HandleFunc("/shop/{slug}", func(w http.ResponseWriter, r *http.Request) { ShopItem(w, r, app) }).Methods("GET")
	request := func(path string, form url.Values) *httptest.ResponseRecorder {
		method := "GET"
		if form != nil {
			method = "POST"
		}
		r := httptest.NewRequest(method, path, strings.NewReader(form.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()
		app.Session.LoadAndSave(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if err := auth.LoginPerson(app, r, person, auth.MethodEmailLink); err != nil {
				t.Fatal(err)
			}
			root.ServeHTTP(w, r)
		})).ServeHTTP(w, r)
		return w
	}
	form := url.Values{"tag": {before.Tag}, "slug": {before.Slug}, "name": {before.Name}, "product_type": {"exclusive"}, "status": {"published"}, "base_price": {"40.00"}, "currency": {"USD"}, "stripe_tax_code": {in.StripeTaxCode}, "country_of_origin": {"US"}, "requires_shipping": {"on"}}
	if w := request("/admin/merch/"+productID, form); w.Code != http.StatusSeeOther || !strings.HasPrefix(w.Header().Get("Location"), "/dashboard?error=") {
		t.Fatalf("org manager gained shop-admin access: %d", w.Code)
	}
	if _, err := app.DB.Exec(c, `INSERT INTO people_roles(person_id,scope,position) VALUES($1,'merch','admin')`, person); err != nil {
		t.Fatal(err)
	}
	if w := request("/admin/merch/"+productID, form); w.Code != http.StatusSeeOther || strings.Contains(w.Header().Get("Location"), "err=") {
		t.Fatalf("shop admin could not recategorize: %d %s", w.Code, w.Header().Get("Location"))
	}
	after, err := getters.GetMerchProductByID(app, productID)
	if err != nil || after.ProductType != "exclusive" || after.FulfillmentOrganizationID != org || !after.OnlineEnabled || after.OrganizationName != name || after.OrganizationLogoURL != logo {
		t.Fatalf("category or organization changed incorrectly: %+v %v", after, err)
	}
	in.ID, in.UpdatedAt = productID, before.UpdatedAt
	if _, err := getters.SaveOrganizationProduct(app, org, in); !errors.Is(err, getters.ErrShopEditConflict) {
		t.Fatalf("stale org form could overwrite admin category: %v", err)
	}
	path := "/dashboard/orgs/" + org + "/shop?tab=product&product=" + productID
	if w := request(path, nil); w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `<option value="exclusive" selected>Attendee exclusives</option>`) {
		t.Fatalf("org editor lost admin category: %d", w.Code)
	}
	for _, path := range []string{"/shop/all?cat=exclusive", "/shop/" + before.Slug} {
		w := request(path, nil)
		if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), logo) || !strings.Contains(w.Body.String(), name) || strings.Contains(w.Body.String(), "private supplier note") {
			t.Fatalf("public product branding: %s %d %s", path, w.Code, w.Body.String())
		}
	}
	if w := request("/shop/all?cat=apparel", nil); strings.Contains(w.Body.String(), before.Name) {
		t.Fatal("product still appears in its old category")
	}
	if _, err := app.DB.Exec(c, `UPDATE organizations SET logo_light_url='' WHERE id=$1`, org); err != nil {
		t.Fatal(err)
	}
	if w := request("/shop/"+before.Slug, nil); w.Code != http.StatusOK || strings.Contains(w.Body.String(), logo) || !strings.Contains(w.Body.String(), name) {
		t.Fatal("missing logo did not fall back to organization name")
	}
}
