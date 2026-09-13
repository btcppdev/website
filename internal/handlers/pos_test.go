package handlers

import (
	"btcpp-web/external/getters"
	"btcpp-web/internal/auth"
	"btcpp-web/internal/config"
	"btcpp-web/internal/types"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
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
	"sync"
	"testing"
	"time"
)

func TestPOSCurrency(t *testing.T) {
	for _, tc := range []struct {
		currencies []string
		want       string
	}{{[]string{"EUR", "EUR"}, "EUR"}, {[]string{"BTC", " eur "}, "EUR"}, {[]string{"EUR", "USD"}, ""}, {nil, ""}} {
		conf := &types.Conf{}
		for _, c := range tc.currencies {
			conf.Tickets = append(conf.Tickets, &types.ConfTicket{Currency: c})
		}
		got, err := posCurrency(conf)
		if got != tc.want || (tc.want == "" && err == nil) {
			t.Fatalf("%v: %q %v", tc.currencies, got, err)
		}
	}
}
func TestPOSPINAuthorization(t *testing.T) {
	session := scs.New()
	app := &config.AppContext{Session: session, Env: &types.EnvConfig{RegistryPin: "1234"}}
	for _, pin := range []string{"", "9999", "1234"} {
		r := httptest.NewRequest("GET", "/", nil)
		ctx, err := session.Load(r.Context(), "")
		if err != nil {
			t.Fatal(err)
		}
		r = r.WithContext(ctx)
		session.Put(ctx, "pin", pin)
		if posAuthorized(r, app) != (pin == "1234") {
			t.Fatalf("PIN authorization failed for %q", pin)
		}
	}
	app.Env.RegistryPin = ""
	r := httptest.NewRequest("GET", "/", nil)
	ctx, _ := session.Load(r.Context(), "")
	if posAuthorized(r.WithContext(ctx), app) {
		t.Fatal("empty configuration must not grant access")
	}
}

// Uses only a disposable database and a mock OpenNode server. POS_BROWSER_PREVIEW
// keeps the same fixture available for manual/mobile browser verification.
func TestPOSFlow(t *testing.T) {
	if os.Getenv("BTCPP_POSTGRES_SMOKE") != "1" {
		t.Skip("requires isolated migrated test database")
	}
	pool, err := pgxpool.New(context.Background(), os.Getenv("DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	wd, _ := os.Getwd()
	if err = os.Chdir("../.."); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(wd)
	app := &config.AppContext{DB: pool, Session: scs.New(), Env: &types.EnvConfig{RegistryPin: "1234"}, Err: log.New(io.Discard, "", 0), Infos: log.New(io.Discard, "", 0)}
	if err = loadTemplates(app); err != nil {
		t.Fatal(err)
	}
	c := context.Background()
	suffix := uuid.NewString()
	var conf, product, variant string
	tag := "pos-" + suffix
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(pool.QueryRow(c, `INSERT INTO conferences(tag,active,description,date_desc,start_date,end_date,timezone,location,venue) VALUES($1,true,'Berlin POS preview','September 2026',now(),now()+interval '2 days','Europe/Berlin','Berlin','Test venue') RETURNING id::text`, tag).Scan(&conf))
	must(pool.QueryRow(c, `INSERT INTO merch_products(tag,slug,name,status) VALUES($1,$1,'Core hat','published') RETURNING id::text`, suffix).Scan(&product))
	must(pool.QueryRow(c, `INSERT INTO merch_variants(product_id,sku,label) VALUES($1,$2,'One size / rust') RETURNING id::text`, product, suffix).Scan(&variant))
	_, err = getters.AddMerchProductImage(app, product, "/static/img/merch/core-hat.avif", "", "Core hat", 0, true)
	must(err)
	if os.Getenv("POS_BROWSER_PREVIEW") == "1" {
		for _, demo := range []struct {
			name, image, label string
			price              int64
			stock              int
		}{
			{"Libbit hat", "/static/img/merch/libbit-hat.avif", "One size / black", 30000, 6},
			{"LibreRelay hat", "/static/img/merch/librerelay-hat.avif", "One size / blue", 35000, 0},
		} {
			demoKey := uuid.NewString()
			var demoProduct, demoVariant string
			must(pool.QueryRow(c, `INSERT INTO merch_products(tag,slug,name,status) VALUES($1,$1,$2,'published') RETURNING id::text`, demoKey, demo.name).Scan(&demoProduct))
			must(pool.QueryRow(c, `INSERT INTO merch_variants(product_id,sku,label) VALUES($1,$2,$3) RETURNING id::text`, demoProduct, demoKey, demo.label).Scan(&demoVariant))
			_, err = getters.AddMerchProductImage(app, demoProduct, demo.image, "", demo.name, 0, true)
			must(err)
			must(getters.AdjustMerchInventory(app, demoVariant, "initial", 12, "", "preview"))
			must(getters.POSConfigure(app, conf, demoVariant, "", uuid.NewString(), true, demo.price, demo.stock))
		}
	}
	// Reuse the real getter's ticket input so currency hydration follows production.
	_, err = pool.Exec(c, `INSERT INTO conference_tickets(conference_id,ticket_key,tier,currency) VALUES($1,$2,'General','EUR')`, conf, suffix)
	must(err)
	must(getters.AdjustMerchInventory(app, variant, "initial", 12, "", "preview"))
	must(getters.POSConfigure(app, conf, variant, "", uuid.NewString(), true, 25000, 8))
	must(getters.POSSetEnabled(app, conf, "admin", true))
	var lock sync.Mutex
	charges := map[string]*getters.POSCharge{}
	creates := 0
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		lock.Lock()
		defer lock.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/v1/rates":
			io.WriteString(w, `{"data":{"BTCEUR":{"EUR":80000}}}`)
		case r.URL.Path == "/v1/charges":
			var in struct {
				Amount  int64  `json:"amount"`
				OrderID string `json:"order_id"`
			}
			if json.NewDecoder(r.Body).Decode(&in) != nil {
				http.Error(w, "bad", 400)
				return
			}
			creates++
			id := uuid.NewString()
			charges[id] = &getters.POSCharge{ID: id, OrderID: in.OrderID, Status: "unpaid", Price: in.Amount}
			fmt.Fprintf(w, `{"data":{"id":%q,"amount":%d,"order_id":%q,"lightning_invoice":{"payreq":"lnbc250u1testpreviewinvoice","expires_at":%d}}}`, id, in.Amount, in.OrderID, time.Now().Add(10*time.Minute).Unix())
		case strings.HasPrefix(r.URL.Path, "/v2/charge/"):
			id := strings.TrimPrefix(r.URL.Path, "/v2/charge/")
			if charges[id] == nil {
				http.NotFound(w, r)
				return
			}
			json.NewEncoder(w).Encode(map[string]any{"data": charges[id]})
		default:
			http.NotFound(w, r)
		}
	}))
	defer provider.Close()
	app.Env.OpenNode = types.OpenNodeConfig{Endpoint: provider.URL + "/v1", Key: "mock-key"}
	router := mux.NewRouter()
	if os.Getenv("POS_BROWSER_PREVIEW") == "1" {
		// Familiar development-event URLs resolve to the current isolated fixture.
		router.HandleFunc("/dev26/admin/merch-pos", func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, "/preview/admin", http.StatusSeeOther)
		}).Methods("GET")
		router.HandleFunc("/dev26/merch/sell", func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, "/preview", http.StatusSeeOther) }).Methods("GET")
		router.NotFoundHandler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusNotFound)
			io.WriteString(w, `<!doctype html><html lang="en"><head><meta name="viewport" content="width=device-width, initial-scale=1"><title>POS preview · Page not found</title><link rel="stylesheet" href="/static/css/pos.css"></head><body><header class="pos-header"><img src="/static/img/logo_blk.svg" alt="bitcoin++"><div><strong>Local preview</strong><span>Merch register</span></div></header><main class="pos-main"><section class="unlock"><span class="eyebrow">404 / Demo page not found</span><h1>Try the demo event.</h1><p>This preview contains one test event. Use these links to open its register or manage its stock and pricing.</p><p><a class="button" href="/dev26/admin/merch-pos">Admin setup →</a></p><p><a href="/dev26/merch/sell">Open mobile register →</a></p></section></main></body></html>`)
		})
	}
	registerPOSRoutes(router, app)
	router.PathPrefix("/static/").Handler(http.StripPrefix("/static/", http.FileServer(http.Dir("static"))))
	var adminID string
	must(pool.QueryRow(c, `INSERT INTO people(name) VALUES('POS preview admin') RETURNING id::text`).Scan(&adminID))
	_, err = pool.Exec(c, `INSERT INTO person_emails(person_id,email,is_primary) VALUES($1,$2,true)`, adminID, suffix+"@example.test")
	must(err)
	_, err = pool.Exec(c, `INSERT INTO people_roles(person_id,scope,position) VALUES($1,$2,'admin')`, adminID, tag)
	must(err)
	router.HandleFunc("/preview/admin", func(w http.ResponseWriter, r *http.Request) {
		if err := auth.LoginPerson(app, r, adminID, auth.MethodPassword); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		http.Redirect(w, r, "/"+tag+"/admin/merch-pos", 303)
	})
	router.HandleFunc("/preview", func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, "/"+tag+"/merch/sell", 303) })
	if os.Getenv("POS_BROWSER_PREVIEW") == "1" {
		router.HandleFunc("/preview/pay", func(w http.ResponseWriter, r *http.Request) {
			lock.Lock()
			defer lock.Unlock()
			for _, charge := range charges {
				charge.Status = "paid"
			}
			w.Write([]byte("Mock payments marked paid"))
		})
		t.Logf("PREVIEW http://127.0.0.1:19433/%s/merch/sell PIN 1234", tag)
		t.Fatal(http.ListenAndServe("127.0.0.1:19433", app.Session.LoadAndSave(router)))
		return
	}
	server := httptest.NewServer(app.Session.LoadAndSave(router))
	defer server.Close()
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}
	base := server.URL + "/" + tag + "/merch/sell"
	get := func(path string) (int, string) {
		t.Helper()
		resp, e := client.Get(path)
		must(e)
		defer resp.Body.Close()
		b, e := io.ReadAll(resp.Body)
		must(e)
		return resp.StatusCode, string(b)
	}
	post := func(values url.Values) (int, string, string) {
		t.Helper()
		resp, e := client.PostForm(base, values)
		must(e)
		defer resp.Body.Close()
		b, e := io.ReadAll(resp.Body)
		must(e)
		return resp.StatusCode, string(b), resp.Request.URL.String()
	}
	_, html := get(base)
	re := regexp.MustCompile(`name="csrf" value="([^"]+)"`)
	csrf := re.FindStringSubmatch(html)[1]
	if status, _, _ := post(url.Values{"action": {"unlock"}, "pin": {"1234"}}); status != 403 {
		t.Fatalf("missing CSRF=%d", status)
	}
	if status, _, _ := post(url.Values{"csrf": {csrf}, "action": {"checkout"}}); status != 401 {
		t.Fatalf("missing PIN=%d", status)
	}
	if status, _, _ := post(url.Values{"csrf": {csrf}, "action": {"unlock"}, "pin": {"1234"}}); status != 200 {
		t.Fatalf("unlock=%d", status)
	}
	csrf = re.FindStringSubmatch(func() string { _, h := get(base); return h }())[1]
	// A volunteer PIN must not authorize the setup page.
	status, _ := get(server.URL + "/" + tag + "/admin/merch-pos")
	if status == 200 {
		t.Fatal("volunteer accessed admin setup")
	}
	cart := fmt.Sprintf(`[{"variant_id":%q,"quantity":2,"price_sats":25000}]`, variant)
	request := uuid.NewString()
	values := url.Values{"csrf": {csrf}, "action": {"checkout"}, "request_id": {request}, "cart": {cart}}
	status, html, saleURL := post(values)
	if status != 200 || !strings.Contains(html, "Scan. Pay. Done.") {
		t.Fatalf("checkout: %d %s", status, html)
	}
	_, _, retryURL := post(values)
	lock.Lock()
	count := creates
	lock.Unlock()
	if count != 1 || retryURL != saleURL {
		t.Fatalf("duplicate invoices=%d", count)
	}
	parsed, _ := url.Parse(saleURL)
	saleID := parsed.Query().Get("sale")
	status, _ = get(base + "/qr/" + saleID)
	if status != 200 {
		t.Fatalf("QR=%d", status)
	}
	_, body := get(base + "/status/" + saleID)
	if !strings.Contains(body, `"status":"pending"`) {
		t.Fatal(body)
	}
	lock.Lock()
	var chargeID string
	for id := range charges {
		chargeID = id
	}
	lock.Unlock()
	callback := server.URL + "/callback/opennode-pos/" + saleID
	callbackPost := func(hash string) int {
		resp, e := client.PostForm(callback, url.Values{"id": {chargeID}, "status": {"paid"}, "hashed_order": {hash}})
		must(e)
		defer resp.Body.Close()
		return resp.StatusCode
	}
	if callbackPost("forged") != 403 {
		t.Fatal("forged callback accepted")
	}
	mac := hmac.New(sha256.New, []byte("mock-key"))
	mac.Write([]byte(chargeID))
	signature := hex.EncodeToString(mac.Sum(nil))
	if code := callbackPost(signature); code != 200 {
		t.Fatalf("unpaid callback=%d", code)
	}
	_, body = get(base + "/status/" + saleID)
	if !strings.Contains(body, `"status":"pending"`) {
		t.Fatal("trusted forged status rather than provider", body)
	}
	lock.Lock()
	for _, charge := range charges {
		charge.Status = "paid"
	}
	lock.Unlock()
	for i := 0; i < 2; i++ {
		if code := callbackPost(signature); code != 200 {
			t.Fatalf("paid callback=%d", code)
		}
	}
	_, body = get(base + "/status/" + saleID)
	if !strings.Contains(body, `"status":"paid"`) {
		t.Fatal(body)
	}
	status, _, _ = post(url.Values{"csrf": {csrf}, "action": {"handover"}, "sale": {saleID}})
	if status != 200 {
		t.Fatalf("handover=%d", status)
	}
	sale, err := getters.POSGetSale(app, saleID, conf)
	must(err)
	if sale.HandedOverAt == nil {
		t.Fatal("handover not persisted")
	}
	if status, _, _ = post(url.Values{"csrf": {csrf}, "action": {"lock"}}); status != 200 {
		t.Fatalf("lock=%d", status)
	}
	status, _ = get(base + "/status/" + saleID)
	if status != 401 {
		t.Fatalf("locked status accessible=%d", status)
	}
	status, html = get(server.URL + "/preview/admin")
	if status != 200 || !strings.Contains(html, "Set up the merch table.") {
		t.Fatalf("admin page %d: %s", status, html)
	}
	csrf = re.FindStringSubmatch(html)[1]
	operation := regexp.MustCompile(`name="operation_id" value="([^"]+)"`).FindStringSubmatch(html)[1]
	adminValues := url.Values{"csrf": {csrf}, "action": {"stock"}, "operation_id": {operation}, "variant": {variant}, "enabled": {"on"}, "price": {"26000"}, "transfer": {"-1"}}
	for i := 0; i < 2; i++ {
		resp, e := client.PostForm(server.URL+"/"+tag+"/admin/merch-pos", adminValues)
		must(e)
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		if resp.StatusCode != 200 {
			t.Fatalf("admin save=%d", resp.StatusCode)
		}
	}
	var available int
	var price int64
	must(pool.QueryRow(c, `SELECT available,price_sats FROM conference_pos_stock WHERE conference_id=$1 AND variant_id=$2`, conf, variant).Scan(&available, &price))
	if available != 5 || price != 26000 {
		t.Fatalf("admin save/replay: stock=%d price=%d", available, price)
	}
}
