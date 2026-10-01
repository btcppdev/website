package handlers

import (
	"context"
	"encoding/json"
	"html"
	"html/template"
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
	"time"

	"btcpp-web/external/getters"
	"btcpp-web/internal/auth"
	"btcpp-web/internal/config"
	"btcpp-web/internal/types"
	"github.com/alexedwards/scs/v2"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestClosureAdminHTTPPostgres(t *testing.T) {
	if os.Getenv("BTCPP_POSTGRES_SMOKE") != "1" {
		t.Skip("requires isolated migrated PostgreSQL")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, os.Getenv("DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	for _, product := range []string{"alerts.repository-slots", "insider.membership.annual"} {
		t.Run(product, func(t *testing.T) {
			app := &config.AppContext{DB: pool, Session: scs.New(), Env: &types.EnvConfig{}, Err: log.New(io.Discard, "", 0), Infos: log.New(io.Discard, "", 0)}
			raw, err := os.ReadFile("../../templates/admin/service_closure.tmpl")
			if err != nil {
				t.Fatal(err)
			}
			app.TemplateCache, err = template.New("admin/service_closure.tmpl").Parse(`{{define "mainnav"}}{{end}}` + string(raw))
			if err != nil {
				t.Fatal(err)
			}
			var person string
			if err := pool.QueryRow(ctx, `INSERT INTO people(name) VALUES('Closure HTTP fixture') RETURNING id::text`).Scan(&person); err != nil {
				t.Fatal(err)
			}
			if _, err := pool.Exec(ctx, `INSERT INTO person_emails(person_id,email,is_primary,verified_at) VALUES($1,$2,true,now())`, person, person+"@example.test"); err != nil {
				t.Fatal(err)
			}
			if _, err := pool.Exec(ctx, `INSERT INTO people_roles(person_id,scope,position) VALUES($1,'global','admin')`, person); err != nil {
				t.Fatal(err)
			}
			start := time.Now().UTC().Truncate(time.Second)
			end := start.AddDate(1, 0, 0)
			closure := start.Add(end.Sub(start) / 2)
			purchase, err := getters.CreateServicePurchase(app, person, uuid.NewString(), getters.ServiceQuote{ProductCode: product, Name: "Closure fixture", Provider: "opennode", Quantity: 1, UnitCents: 1000, TotalCents: 1000, PeriodStart: &start, PeriodEnd: &end, ExpiresAt: start.Add(time.Hour)})
			if err != nil {
				t.Fatal(err)
			}
			attempt, err := getters.BeginServicePayment(app, person, purchase.OrderID)
			if err != nil {
				t.Fatal(err)
			}
			reference := uuid.NewString()
			if err := getters.BindServicePayment(app, attempt.ID, "opennode", reference); err != nil {
				t.Fatal(err)
			}
			zero := int64(0)
			if err := getters.RecordServicePayment(app, getters.VerifiedServicePayment{AttemptID: attempt.ID, Provider: "opennode", Reference: reference, EventID: "paid:" + reference, Currency: "USD", TotalCents: 1000, PaidAt: start, OpenNodeBitcoin: &getters.OpenNodeBitcoinEvidence{PriceSatoshis: 1001, OverpaidSatoshis: &zero}}); err != nil {
				t.Fatal(err)
			}
			address := "bc1fixture-refund"
			script := "0014" + strings.Repeat("11", 20)
			var calls atomic.Int64
			node := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				user, password, ok := r.BasicAuth()
				if !ok || user != "fixture" || password != "fixture" {
					t.Error("missing node credentials")
				}
				var req struct {
					Method string            `json:"method"`
					Params []json.RawMessage `json:"params"`
				}
				if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
					t.Error(err)
					return
				}
				var result any
				switch req.Method {
				case "getblockchaininfo":
					result = map[string]any{"chain": "main", "blocks": 100, "headers": 100, "bestblockhash": strings.Repeat("c", 64), "time": time.Now().Unix(), "initialblockdownload": false}
				case "validateaddress":
					result = map[string]any{"isvalid": true, "address": address, "scriptPubKey": script}
				case "getblockheader":
					var hash string
					json.Unmarshal(req.Params[0], &hash)
					result = map[string]any{"hash": hash, "height": 95, "confirmations": 6}
				case "getrawtransaction":
					var txid, block string
					json.Unmarshal(req.Params[0], &txid)
					json.Unmarshal(req.Params[2], &block)
					result = map[string]any{"txid": txid, "blockhash": block, "in_active_chain": true, "vin": []any{map[string]any{"txid": strings.Repeat("d", 64)}}, "vout": []any{map[string]any{"n": 0, "value": json.Number("0.00000500"), "scriptPubKey": map[string]any{"hex": script}}}}
				default:
					t.Error("unexpected node operation", req.Method)
				}
				json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": "payout-verification", "result": result})
			}))
			defer node.Close()
			t.Setenv("BITCOIN_RPC_URL", node.URL)
			t.Setenv("BITCOIN_RPC_USER", "fixture")
			t.Setenv("BITCOIN_RPC_PASSWORD", "fixture")
			t.Setenv("BITCOIN_RPC_CHAIN", "main")
			mux := http.NewServeMux()
			mux.HandleFunc("/login-fixture", func(w http.ResponseWriter, r *http.Request) {
				if err := auth.LoginPerson(app, r, person, auth.MethodPassword); err != nil {
					t.Error(err)
				}
				w.WriteHeader(204)
			})
			mux.HandleFunc("/admin/service-closure", func(w http.ResponseWriter, r *http.Request) { AdminServiceClosure(w, r, app) })
			server := httptest.NewServer(app.Session.LoadAndSave(mux))
			defer server.Close()
			jar, _ := cookiejar.New(nil)
			client := &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
			call := func(path string, form url.Values) (int, string, string) {
				t.Helper()
				var response *http.Response
				var err error
				if form == nil {
					response, err = client.Get(server.URL + path)
				} else {
					response, err = client.PostForm(server.URL+path, form)
				}
				if err != nil {
					t.Fatal(err)
				}
				defer response.Body.Close()
				body, err := io.ReadAll(response.Body)
				if err != nil {
					t.Fatal(err)
				}
				return response.StatusCode, string(body), response.Header.Get("Location")
			}
			hidden := func(body, name string) string {
				t.Helper()
				match := regexp.MustCompile(`name="` + name + `" value="([^"]+)"`).FindStringSubmatch(body)
				if len(match) != 2 {
					t.Fatalf("missing %s in %s", name, body)
				}
				return html.UnescapeString(match[1])
			}
			if code, _, _ := call("/admin/service-closure", nil); code != 303 {
				t.Fatal("anonymous allowed", code)
			}
			if code, _, _ := call("/login-fixture", nil); code != 204 {
				t.Fatal("login", code)
			}
			path := "/admin/service-closure?" + url.Values{"attempt": {attempt.ID}, "closure": {closure.Format(time.RFC3339)}}.Encode()
			code, body, _ := call(path, nil)
			if code != 200 {
				t.Fatal(code, body)
			}
			form := url.Values{"action": {"review"}, "reason": {"Confirmed closure"}, "confirmed": {"yes"}}
			for _, name := range []string{"csrf", "review", "attempt", "closure", "amount"} {
				form.Set(name, hidden(body, name))
			}
			if form.Get("amount") != "500" {
				t.Fatal("wrong refund preview", form)
			}
			code, body, location := call("/admin/service-closure", form)
			if code != 303 {
				t.Fatal(code, body)
			}
			code, body, _ = call(location, nil)
			if code != 200 || !strings.Contains(body, "Verify and approve address") {
				t.Fatal(code, body)
			}
			destination := url.Values{"action": {"destination"}, "csrf": {hidden(body, "csrf")}, "review": {form.Get("review")}, "address": {address}, "reason": {"Confirmed recipient"}, "confirmed": {"yes"}}
			code, body, _ = call("/admin/service-closure", destination)
			if code != 303 {
				t.Fatal(code, body)
			}
			code, body, _ = call(location, nil)
			if code != 200 || !strings.Contains(body, "Verify and record payout") {
				t.Fatal(code, body)
			}
			txid := strings.ReplaceAll(uuid.NewString(), "-", "") + strings.ReplaceAll(uuid.NewString(), "-", "")
			payout := url.Values{"action": {"payout"}, "csrf": {hidden(body, "csrf")}, "review": {form.Get("review")}, "txid": {txid}, "block": {strings.Repeat("b", 64)}, "vout": {"0"}, "confirmed": {"yes"}}
			code, body, _ = call("/admin/service-closure", payout)
			if code != 303 {
				t.Fatal(code, body)
			}
			before := calls.Load()
			code, body, _ = call("/admin/service-closure", payout)
			if code != 303 || calls.Load() != before {
				t.Fatal("replay contacted node or failed", code, body)
			}
			code, body, _ = call(location, nil)
			if code != 200 || !strings.Contains(body, "Payout recorded") || strings.Contains(body, "Verify and record payout") {
				t.Fatal(code, body)
			}
			record, err := getters.InspectBitcoinClosurePayout(ctx, app, form.Get("review"))
			if err != nil || record.ActorID != person || record.Refund.AmountCents != 499 || *record.Refund.AmountSatoshis != 500 {
				t.Fatal("wrong financial record", record, err)
			}
			var count int
			if err := pool.QueryRow(ctx, `SELECT count(*) FROM service_payment_refunds WHERE attempt_id=$1`, attempt.ID).Scan(&count); err != nil || count != 1 {
				t.Fatal("duplicate refund", count, err)
			}
		})
	}
}
