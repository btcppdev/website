package handlers

import (
	"btcpp-web/external/getters"
	"btcpp-web/internal/auth"
	"btcpp-web/internal/config"
	"btcpp-web/internal/types"
	"context"
	"fmt"
	"github.com/alexedwards/scs/v2"
	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/jackc/pgx/v5/pgxpool"
	"io"
	"log"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"
)

func TestBadgeCanvasShopFlow(t *testing.T) {
	if os.Getenv("BTCPP_POSTGRES_SMOKE") != "1" {
		t.Skip("requires disposable migrated database")
	}
	previousRate := shopRate
	var testRate atomic.Int64
	testRate.Store(100000)
	shopRate = &merchExchangeRate{fetch: func(string) (float64, error) { return float64(testRate.Load()), nil }}
	defer func() { shopRate = previousRate }()
	pool, err := pgxpool.New(context.Background(), os.Getenv("DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	wd, _ := os.Getwd()
	if err := os.Chdir("../.."); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(wd)
	app := &config.AppContext{DB: pool, Session: scs.New(), Env: &types.EnvConfig{MailOff: true}, Err: log.New(os.Stderr, "", 0), Infos: log.New(io.Discard, "", 0)}
	if err := loadTemplates(app); err != nil {
		t.Fatal(err)
	}
	id := func(q string, args ...any) string {
		t.Helper()
		var value string
		if err := pool.QueryRow(context.Background(), q, args...).Scan(&value); err != nil {
			t.Fatal(err)
		}
		return value
	}
	suffix := uuid.NewString()
	slug := "canvas-preview-" + suffix
	person := id(`INSERT INTO people(name) VALUES ('Canvas Preview') RETURNING id::text`)
	other := id(`INSERT INTO people(name) VALUES ('Other badge owner') RETURNING id::text`)
	org := id(`INSERT INTO organizations(name) VALUES ($1) RETURNING id::text`, "Preview badge issuer "+suffix)
	product, err := getters.CreateMerchProduct(app, getters.MerchProductInput{Tag: slug, Slug: slug, Name: "Your badge, on canvas.", Subtitle: "9×9-inch badge canvas", Description: "A canvas for the things you have earned. Choose an issued badge and make it part of your space.", ProductType: types.MerchProductTypeBadgeCanvas, Status: types.MerchProductStatusPublished, BasePriceCents: 6500, Currency: "USD", RequiresShipping: true, AllowEventPickup: true})
	if err != nil {
		t.Fatal(err)
	}
	variant, err := getters.CreateMerchVariant(app, getters.MerchVariantInput{ProductID: product, SKU: slug, Label: "9×9-inch canvas", InventoryPolicy: types.MerchInventoryPolicyUnlimited, Status: "active", WeightGrams: 500, LengthMM: 260, WidthMM: 260, HeightMM: 50})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := getters.AddMerchProductImage(app, product, "/static/img/merch/badge-canvas.svg", "", "Example badge canvas", 0, true); err != nil {
		t.Fatal(err)
	}
	grant := func(owner, name, art, state string) string {
		return id(`INSERT INTO organization_badge_grants(organization_id,recipient_person_id,issuer_pubkey,badge_identifier,badge_name,badge_image_url,subject_profile_url,state,award_event_id) VALUES($1,$2,$3,$4,$4,$5,'https://example.test/profile',$6,$7) RETURNING id::text`, org, owner, strings.Repeat("a", 64), name, art, state, strings.Repeat("b", 64))
	}
	one := grant(person, "Builder badge", "http://127.0.0.1:8096/static/img/merch/badge-canvas.svg", "issued")
	two := grant(person, "Community badge", "http://127.0.0.1:8096/static/img/rebrand/breakthroughs.jpg", "accepted")
	foreign := grant(other, "Other person's badge", "http://127.0.0.1:8096/static/img/merch/badge-canvas.svg", "issued")
	defer func() {
		pool.Exec(context.Background(), `DELETE FROM merch_products WHERE id=$1`, product)
		pool.Exec(context.Background(), `DELETE FROM organizations WHERE id=$1`, org)
		pool.Exec(context.Background(), `DELETE FROM people WHERE id IN ($1,$2)`, person, other)
	}()
	root := mux.NewRouter()
	root.HandleFunc("/preview", func(w http.ResponseWriter, r *http.Request) {
		if err := auth.LoginPerson(app, r, person, auth.MethodEmailLink); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		http.Redirect(w, r, "/shop/"+slug, 303)
	})
	root.PathPrefix("/static/").Handler(http.StripPrefix("/static/", http.FileServer(http.Dir("static"))))
	root.HandleFunc("/shop", func(w http.ResponseWriter, r *http.Request) { ShopHome(w, r, app) }).Methods("GET")
	root.HandleFunc("/shop/all", func(w http.ResponseWriter, r *http.Request) { ShopCollection(w, r, app) }).Methods("GET")
	root.HandleFunc("/shop/cart", func(w http.ResponseWriter, r *http.Request) { ShopCart(w, r, app) }).Methods("GET")
	root.HandleFunc("/shop/cart", func(w http.ResponseWriter, r *http.Request) { ShopCartUpdate(w, r, app) }).Methods("POST")
	root.HandleFunc("/shop/cart/add", func(w http.ResponseWriter, r *http.Request) { ShopCartAdd(w, r, app) }).Methods("POST")
	root.HandleFunc("/shop/checkout", func(w http.ResponseWriter, r *http.Request) { ShopCheckout(w, r, app) }).Methods("GET")
	// No payment or notification endpoints are mounted in the preview.
	root.HandleFunc("/shop/{slug}", func(w http.ResponseWriter, r *http.Request) { ShopItem(w, r, app) }).Methods("GET")
	root.HandleFunc("/preview/admin", func(w http.ResponseWriter, r *http.Request) {
		p, err := getters.GetMerchProductBySlug(app, slug, false)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		page := baseShopPage(app, r, "Admin editor preview")
		page.Product = p
		page.Flash = "Local editor preview · changes are not saved"
		renderShopTemplate(w, r, app, "admin/merch_edit.tmpl", page)
	}).Methods("GET")
	root.HandleFunc("/preview/admin/new", func(w http.ResponseWriter, r *http.Request) {
		page := baseShopPage(app, r, "New product preview")
		page.Product = &types.MerchProduct{Status: "draft", Currency: "USD"}
		page.Flash = "Local editor preview · changes are not saved"
		renderShopTemplate(w, r, app, "admin/merch_new.tmpl", page)
	}).Methods("GET")
	handler := app.Session.LoadAndSave(root)
	server := httptest.NewServer(handler)
	defer server.Close()
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar, CheckRedirect: func(r *http.Request, via []*http.Request) error { return http.ErrUseLastResponse }}
	get := func(path string) (int, string) {
		t.Helper()
		resp, err := client.Get(server.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, string(body)
	}
	post := func(path string, values url.Values) int {
		t.Helper()
		resp, err := client.PostForm(server.URL+path, values)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		return resp.StatusCode
	}
	if code, body := get("/shop/" + slug); code != 200 || !strings.Contains(body, "Sign in to choose") {
		t.Fatal("anonymous canvas page", code)
	}
	get("/preview")
	code, body := get("/shop/" + slug)
	if code != 200 || !strings.Contains(body, "Builder badge") || strings.Contains(body, foreign) {
		t.Fatal("badge selector ownership", code)
	}
	if !strings.Contains(body, "65k sats</strong>") || !strings.Contains(body, "$65 USD") || !strings.Contains(body, "9×9-inch canvas") {
		t.Fatal("canvas size and satoshi-first dollar-pegged price missing")
	}
	matches := regexp.MustCompile(`name="csrf" value="([^"]+)"`).FindStringSubmatch(body)
	if len(matches) != 2 {
		t.Fatal("missing canvas CSRF")
	}
	csrf := matches[1]
	if post("/shop/cart/add", url.Values{"variant_id": {variant}, "badge_ref": {one}}) != 403 {
		t.Fatal("missing CSRF accepted")
	}
	add := func(grant string) {
		post("/shop/cart/add", url.Values{"variant_id": {variant}, "badge_ref": {grant}, "csrf": {csrf}, "qty": {"1"}})
	}
	add(foreign)
	if _, body := get("/shop/cart"); strings.Contains(body, "Other person's badge") {
		t.Fatal("foreign badge in cart")
	}
	add(one)
	add(two)
	_, body = get("/shop/cart")
	if !strings.Contains(body, "qty_"+variant+":"+one) || !strings.Contains(body, "qty_"+variant+":"+two) {
		t.Fatal("designs combined in cart", body)
	}
	post("/shop/cart", url.Values{"qty_" + variant + ":" + one: {"2"}, "qty_" + variant + ":" + two: {"1"}})
	if _, body := get("/shop/checkout"); !strings.Contains(body, "Builder badge") || !strings.Contains(body, "Community badge") {
		t.Fatal("checkout lost designs", body)
	}
	if _, err := pool.Exec(context.Background(), `UPDATE organization_badge_grants SET state='revoked' WHERE id=$1`, one); err != nil {
		t.Fatal(err)
	}
	if code, _ := get("/shop/checkout"); code != 303 {
		t.Fatal("revoked design reached checkout")
	}
	_, body = get("/shop/cart")
	if !strings.Contains(body, "Unavailable badge") {
		t.Fatal("unavailable badge cannot be removed")
	}
	post("/shop/cart", url.Values{"qty_" + variant + ":" + one: {"0"}, "qty_" + variant + ":" + two: {"1"}})
	if code, _ := get("/shop/checkout"); code != 200 {
		t.Fatal("valid design could not check out after removal", code)
	}

	// A normal merchandise item also displays sats, and an old cart reloads both
	// the catalog price (including variant adjustments) and FX quote at checkout.
	standard, err := getters.CreateMerchProduct(app, getters.MerchProductInput{Tag: "shirt-" + suffix, Slug: "shirt-" + suffix, Name: "Test shirt", ProductType: "shirt", Status: types.MerchProductStatusPublished, BasePriceCents: 2000, Currency: "USD", RequiresShipping: true})
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Exec(context.Background(), `DELETE FROM merch_products WHERE id=$1`, standard)
	standardVariant, err := getters.CreateMerchVariant(app, getters.MerchVariantInput{ProductID: standard, SKU: "shirt-" + suffix, Label: "Medium", InventoryPolicy: types.MerchInventoryPolicyUnlimited, Status: "active"})
	if err != nil {
		t.Fatal(err)
	}
	if code, body := get("/shop/shirt-" + suffix); code != 200 || !strings.Contains(body, "20k sats</strong>") {
		t.Fatal("standard merchandise satoshi display", code)
	}
	if code, body := get("/shop"); code != 200 || !strings.Contains(body, "shop-category-nav") || !strings.Contains(body, "Print one of your badges") || strings.Contains(body, "bestsellers") {
		t.Fatal("new shop landing page", code)
	}
	if code, body := get("/shop/all?cat=apparel"); code != 200 || !strings.Contains(body, "Wear your bitcoin side.") || !strings.Contains(body, "Test shirt") || strings.Contains(body, "Your badge, on canvas.") {
		t.Fatal("category browsing", code)
	}
	post("/shop/cart/add", url.Values{"variant_id": {standardVariant}, "qty": {"1"}})
	if _, err := pool.Exec(context.Background(), `UPDATE merch_products SET base_price_cents=2800 WHERE id=$1`, standard); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(context.Background(), `UPDATE merch_variants SET price_delta_cents=200 WHERE id=$1`, standardVariant); err != nil {
		t.Fatal(err)
	}
	testRate.Store(200000)
	if code, body := get("/shop/checkout"); code != 200 || !strings.Contains(body, "15k sats") || !strings.Contains(body, `data-subtotal-cents="9500"`) || !strings.Contains(body, `data-btc-usd="200000"`) {
		t.Fatal("checkout did not refresh item price and rate", code, body)
	}
	post("/shop/cart", url.Values{"qty_" + standardVariant: {"0"}})
	testRate.Store(100000)
	shopRate.refresh()
	for _, path := range []string{"/preview/admin", "/preview/admin/new"} {
		if code, body := get(path); code != 200 || !strings.Contains(body, `name="base_price"`) || !strings.Contains(body, "Shipping &amp; advanced settings") {
			t.Fatal("admin editor render", path, code)
		}
	}
	if os.Getenv("CANVAS_BROWSER_PREVIEW") == "1" {
		pool.Exec(context.Background(), `UPDATE organization_badge_grants SET state='issued' WHERE id=$1`, one)
		studio := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			if r.URL.Path != "/api/public/btcpp/people/"+person+"/badges" {
				fmt.Fprint(w, `{"issued":[]}`)
				return
			}
			fmt.Fprintf(w, `{"issued":[{"definition":{"issuer_pubkey":%q,"identifier":"builder","name":"Builder badge","image_url":"http://127.0.0.1:8096/preview/art/builder.svg"},"award":{"event_id":%q}},{"definition":{"issuer_pubkey":%q,"identifier":"community","name":"Community badge · independent issuer","image_url":"http://127.0.0.1:8096/preview/art/community.svg"},"award":{"event_id":%q}}]}`, strings.Repeat("a", 64), strings.Repeat("e", 64), strings.Repeat("d", 64), strings.Repeat("f", 64))
		}))
		defer studio.Close()
		app.Env.BadgeStudioURL = studio.URL
		root.HandleFunc("/preview/art/{design}.svg", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "image/svg+xml")
			color, label := "#ffb65e", "BUILDER"
			if mux.Vars(r)["design"] == "community" {
				color, label = "#b6ff5c", "COMMUNITY"
			}
			fmt.Fprintf(w, `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 500 500"><rect width="500" height="500" fill="%s"/><circle cx="250" cy="225" r="150" fill="none" stroke="#171b17" stroke-width="5"/><path d="M142 225h88m-44-44v88m84-44h88m-44-44v88" stroke="#171b17" stroke-width="15"/><text x="250" y="432" text-anchor="middle" font-family="monospace" font-size="30" fill="#171b17">%s</text></svg>`, color, label)
		})
		t.Log("Canvas preview: http://127.0.0.1:8096/preview ($65 USD base price; synthetic parcel dimensions; payments disabled)")
		t.Fatal(http.ListenAndServe("127.0.0.1:8096", handler))
	}
}
