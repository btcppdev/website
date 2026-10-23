package handlers

import (
	"btcpp-web/internal/auth"
	"btcpp-web/internal/config"
	"btcpp-web/internal/types"
	"context"
	"github.com/alexedwards/scs/v2"
	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/jackc/pgx/v5/pgxpool"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"

	"btcpp-web/external/getters"
	"bytes"
	"html/template"
	"os"
	"strings"
	"testing"
	"time"
)

func TestPurchaseInvoicePDFEscapesBillingAndShowsRecordedTax(t *testing.T) {
	raw, err := os.ReadFile("../../templates/purchase_invoice_pdf.tmpl")
	if err != nil {
		t.Fatal(err)
	}
	tmpl, err := template.New("invoice").Parse(string(raw))
	if err != nil {
		t.Fatal(err)
	}
	invoice := getters.PurchaseInvoice{ID: 42, IssuedAt: time.Now(), Snapshot: getters.InvoiceSnapshot{SellerName: "btcplusplus LLC", SellerAddress: "900 Olive St\nAustin TX 78702\nUSA", SellerEIN: "93-4644668", Currency: "USD", Buyer: getters.InvoiceBuyer{Name: "<script>bad()</script>", Address: "123 Test St", City: "Austin", Country: "USA"}, Subtotal: 1000, Tax: 100, Total: 1100, Items: []getters.InvoiceItem{{Name: "Ticket <img src=x>", Quantity: 1, Amount: 1000}}}}
	var out bytes.Buffer
	if err = tmpl.Execute(&out, invoice); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"BTCPP-0000000042", "93-4644668", "Sales tax", "1.00", "11.00", "&lt;script&gt;", "&lt;img"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("missing %q", want)
		}
	}
	if strings.Contains(out.String(), "<script>") || strings.Contains(out.String(), "<img src=x>") {
		t.Fatal("unescaped invoice fields")
	}
}

func TestPurchaseInvoiceHTTPPostgres(t *testing.T) {
	if os.Getenv("BTCPP_POSTGRES_SMOKE") != "1" {
		t.Skip("requires disposable PostgreSQL")
	}
	pool, err := pgxpool.New(context.Background(), os.Getenv("DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	owner, other, ref := uuid.NewString(), uuid.NewString(), uuid.NewString()
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(context.Background(), q, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`INSERT INTO people(id,name) VALUES($1,'Invoice buyer'),($2,'Other buyer')`, owner, other)
	var order string
	if err = pool.QueryRow(context.Background(), `INSERT INTO shop_orders(public_id,buyer_email,buyer_name,buyer_person_id,status,subtotal_cents,sales_tax_amount_cents,total_cents,payment_provider) VALUES($1,'invoice@example.test','Example Co',$2,'paid',1000,100,1100,'stripe') RETURNING id::text`, ref, owner).Scan(&order); err != nil {
		t.Fatal(err)
	}
	exec(`INSERT INTO shop_order_items(order_id,quantity,unit_price_cents,line_total_cents,product_name_snapshot) VALUES($1,1,1000,1000,'Conference admission')`, order)
	defer func() {
		exec(`DELETE FROM purchase_invoices WHERE order_id=$1`, order)
		exec(`DELETE FROM shop_orders WHERE id=$1`, order)
		exec(`DELETE FROM people WHERE id IN ($1,$2)`, owner, other)
	}()
	wd, _ := os.Getwd()
	if err = os.Chdir("../.."); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(wd)
	session := scs.New()
	app := &config.AppContext{DB: pool, Session: session, Env: &types.EnvConfig{}, Infos: log.New(io.Discard, "", 0), Err: log.New(io.Discard, "", 0)}
	if err = loadTemplates(app); err != nil {
		t.Fatal(err)
	}
	router := mux.NewRouter()
	router.HandleFunc("/dashboard/orders/{order}/invoice", func(w http.ResponseWriter, r *http.Request) { DashboardPurchaseInvoice(w, r, app) })
	request := func(person, method, query string, form url.Values) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(method, "/dashboard/orders/"+ref+"/invoice"+query, strings.NewReader(form.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		c, err := session.Load(r.Context(), "")
		if err != nil {
			t.Fatal(err)
		}
		r = r.WithContext(c)
		if person != "" {
			if err := auth.LoginPerson(app, r, person, auth.MethodPassword); err != nil {
				t.Fatal(err)
			}
		}
		session.Put(c, authMethodsCSRFKey, "test-csrf")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)
		return w
	}
	if w := request("", "GET", "", nil); w.Code != 303 {
		t.Fatal("anonymous request", w.Code)
	}
	if w := request(other, "GET", "", nil); w.Code != 404 {
		t.Fatal("foreign invoice", w.Code)
	}
	w := request(owner, "GET", "", nil)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "Billing street address") {
		t.Fatal("billing form", w.Code)
	}
	if dir := os.Getenv("INVOICE_PREVIEW_DIR"); dir != "" {
		os.WriteFile(filepath.Join(dir, "form.html"), w.Body.Bytes(), 0600)
	}
	form := url.Values{"name": {"Example Co"}, "address": {"123 Example St"}, "city": {"Austin"}, "postal": {"78702"}, "country": {"USA"}, "tax_id": {"TEST-123"}}
	if w = request(owner, "POST", "", form); w.Code != 403 {
		t.Fatal("CSRF accepted", w.Code)
	}
	form.Set("csrf", "test-csrf")
	if w = request(owner, "POST", "", form); w.Code != 303 {
		t.Fatal("issue", w.Code, w.Body.String())
	}
	if w = request(other, "GET", "?download=pdf", nil); w.Code != 404 {
		t.Fatal("foreign PDF", w.Code)
	}
	if w = request(owner, "GET", "", nil); w.Code != 200 || !strings.Contains(w.Body.String(), "Download invoice (PDF)") {
		t.Fatal("issued view", w.Code)
	}
	if os.Getenv("INVOICE_PDF_TEST") == "1" {
		w = request(owner, "GET", "?download=pdf", nil)
		if w.Code != 200 || !bytes.HasPrefix(w.Body.Bytes(), []byte("%PDF-")) || w.Header().Get("Cache-Control") != "private, no-store" {
			t.Fatal("PDF download", w.Code, w.Body.String())
		}
		if dir := os.Getenv("INVOICE_PREVIEW_DIR"); dir != "" {
			os.WriteFile(filepath.Join(dir, "invoice.pdf"), w.Body.Bytes(), 0600)
		}
	}
}
