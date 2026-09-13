package getters

import (
	"btcpp-web/internal/config"
	"btcpp-web/internal/types"
	"context"
	"encoding/json"
	"fmt"
	"github.com/google/uuid"
	"net/http"
	"net/http/httptest"
	"sort"
	"sync"
	"testing"
	"time"
)

func TestPOSExactSatsRequest(t *testing.T) {
	id := uuid.NewString()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]json.RawMessage
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Error(err)
		}
		if _, ok := payload["currency"]; ok {
			t.Error("sats charge must omit currency")
		}
		if string(payload["amount"]) != "25001" {
			t.Errorf("amount=%s", payload["amount"])
		}
		fmt.Fprintf(w, `{"data":{"id":"charge-1","amount":25001,"order_id":%q,"lightning_invoice":{"payreq":"lnbc-test","expires_at":1900000000}}}`, id)
	}))
	defer server.Close()
	app := &config.AppContext{Env: &types.EnvConfig{OpenNode: types.OpenNodeConfig{Endpoint: server.URL, Key: "test"}}}
	p, err := POSOpenNodeCreate(app, &types.POSSale{ID: id, TotalSats: 25001})
	if err != nil || p.Amount != 25001 {
		t.Fatalf("charge=%+v err=%v", p, err)
	}
}
func TestPOSCartValidation(t *testing.T) {
	id := uuid.NewString()
	for _, lines := range [][]types.POSCartLine{nil, {{VariantID: id, Quantity: 0, PriceSats: 1}}, {{VariantID: id, Quantity: 1, PriceSats: 0}}, {{VariantID: id, Quantity: 1000, PriceSats: 500000000}}, {{VariantID: id, Quantity: 1, PriceSats: 1}, {VariantID: id, Quantity: 1, PriceSats: 1}}} {
		if ValidatePOSCart(lines) == nil {
			t.Fatalf("accepted bad cart %+v", lines)
		}
	}
	if err := ValidatePOSCart([]types.POSCartLine{{VariantID: id, Quantity: 2, PriceSats: 25001}}); err != nil {
		t.Fatal(err)
	}
}
func TestPOSInventoryLifecycle(t *testing.T) {
	app := databaseSmokeContext(t)
	conf, _ := insertSmokeConference(t, app)
	c := context.Background()
	var product, variant string
	suffix := uuid.NewString()
	if err := app.DB.QueryRow(c, `INSERT INTO merch_products(tag,slug,name,status) VALUES($1,$1,'POS test shirt','published') RETURNING id::text`, suffix).Scan(&product); err != nil {
		t.Fatal(err)
	}
	if err := app.DB.QueryRow(c, `INSERT INTO merch_variants(product_id,sku,label) VALUES($1,$2,'Medium') RETURNING id::text`, product, suffix).Scan(&variant); err != nil {
		t.Fatal(err)
	}
	if err := AdjustMerchInventory(app, variant, "initial", 10, "", "test"); err != nil {
		t.Fatal(err)
	}
	operation := uuid.NewString()
	for i := 0; i < 2; i++ {
		if err := POSConfigure(app, conf, variant, "", operation, true, 25001, 6); err != nil {
			t.Fatal(err)
		}
	}

	products, _, err := POSProducts(app, conf)
	if err != nil {
		t.Fatal(err)
	}
	var found types.POSProduct
	for _, p := range products {
		if p.VariantID == variant {
			found = p
		}
	}
	if found.Available != 6 || found.Central != 4 {
		t.Fatalf("allocation=%+v", found)
	}
	if err := POSSetEnabled(app, conf, "admin", true); err != nil {
		t.Fatal(err)
	}
	if _, _, err := POSCreateSale(app, conf, "register", uuid.NewString(), "EUR", 80000, []types.POSCartLine{{VariantID: variant, Quantity: 1, PriceSats: 1}}); err == nil {
		t.Fatal("accepted client price override")
	}
	request := uuid.NewString()
	lines := []types.POSCartLine{{VariantID: variant, Quantity: 2, PriceSats: 25001}}
	sale, created, err := POSCreateSale(app, conf, "register", request, "EUR", 80000, lines)
	if err != nil || !created {
		t.Fatalf("create: %v %v", created, err)
	}
	repeat, created, err := POSCreateSale(app, conf, "register", request, "EUR", 80000, lines)
	if err != nil || created || repeat.ID != sale.ID {
		t.Fatalf("retry: %+v %v %v", repeat, created, err)
	}
	if _, _, err = POSCreateSale(app, conf, "other", request, "EUR", 80000, lines); err == nil {
		t.Fatal("other register reused sale")
	}
	if err = POSAttachCharge(app, sale.ID, &types.OpenNodePayment{ID: "test-" + sale.ID, Amount: 50002, LNInvoice: types.OpenNodeLightningInvoice{Invoice: "ln-test", ExpiresAt: uint64(time.Now().Add(10 * time.Minute).Unix())}}); err != nil {
		t.Fatal(err)
	}
	if err = POSApplyPayment(app, sale.ID, "test-"+sale.ID, "paid", 50001); err == nil {
		t.Fatal("wrong amount accepted")
	}
	if err = POSApplyPayment(app, sale.ID, "wrong", "paid", 50002); err == nil {
		t.Fatal("wrong charge accepted")
	}
	for i := 0; i < 2; i++ {
		if err = POSApplyPayment(app, sale.ID, "test-"+sale.ID, "paid", 50002); err != nil {
			t.Fatal(err)
		}
	}
	if err = POSApplyPayment(app, sale.ID, "test-"+sale.ID, "expired", 50002); err != nil {
		t.Fatal(err)
	}
	if err = POSHandOver(app, sale.ID, conf, "register"); err != nil {
		t.Fatal(err)
	}
	// Two customers compete for the final four units. Exactly one can reserve three.
	var wg sync.WaitGroup
	var mu sync.Mutex
	var winners []*types.POSSale
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s, _, e := POSCreateSale(app, conf, "register", uuid.NewString(), "EUR", 80000, []types.POSCartLine{{VariantID: variant, Quantity: 3, PriceSats: 25001}})
			if e == nil {
				mu.Lock()
				winners = append(winners, s)
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if len(winners) != 1 {
		t.Fatalf("oversell: %d reservations", len(winners))
	}
	expired := winners[0]
	for i := 0; i < 2; i++ {
		if err = POSApplyPayment(app, expired.ID, "expired-"+expired.ID, "expired", 75003); err != nil {
			t.Fatal(err)
		}
	}
	var stock int
	if err = app.DB.QueryRow(c, `SELECT available FROM conference_pos_stock WHERE conference_id=$1 AND variant_id=$2`, conf, variant).Scan(&stock); err != nil || stock != 4 {
		t.Fatalf("expiry stock=%d err=%v", stock, err)
	}
	if err = POSConfigure(app, conf, variant, "", uuid.NewString(), true, 25001, -5); err == nil {
		t.Fatal("returned reserved or sold stock")
	}
	if err = POSConfigure(app, conf, variant, "", uuid.NewString(), true, 25001, -4); err != nil {
		t.Fatal(err)
	}
	if err = POSApplyPayment(app, expired.ID, "expired-"+expired.ID, "paid", 75003); err != nil {
		t.Fatal(err)
	}
	s, err := POSGetSale(app, expired.ID, conf)
	if err != nil || s.Status != "review" {
		t.Fatalf("late payment %+v %v", s, err)
	}
	if err = POSConfigure(app, conf, variant, "", uuid.NewString(), true, 25001, 1); err != nil {
		t.Fatal(err)
	}
	abandoned, _, err := POSCreateSale(app, conf, "register", uuid.NewString(), "EUR", 80000, []types.POSCartLine{{VariantID: variant, Quantity: 1, PriceSats: 25001}})
	if err != nil {
		t.Fatal(err)
	}
	if err = POSCancelUnconfirmed(app, abandoned.ID, conf, "admin"); err == nil {
		t.Fatal("cancelled an in-flight creation")
	}
	if _, err = app.DB.Exec(c, `UPDATE conference_pos_sales SET created_at=now()-interval '3 minutes' WHERE id=$1`, abandoned.ID); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err = POSCancelUnconfirmed(app, abandoned.ID, conf, "admin"); err != nil {
			t.Fatal(err)
		}
	}
	if err = POSConfigure(app, conf, variant, "", uuid.NewString(), true, 25001, -1); err != nil {
		t.Fatal(err)
	}
	if err = app.DB.QueryRow(c, `SELECT sum(quantity_delta) FROM merch_inventory_events WHERE variant_id=$1`, variant).Scan(&stock); err != nil || stock != 8 {
		t.Fatalf("central final=%d err=%v", stock, err)
	}
}

func TestPOSConfigureItemsAtomic(t *testing.T) {
	app := databaseSmokeContext(t)
	conf, _ := insertSmokeConference(t, app)
	c := context.Background()
	var product string
	key := uuid.NewString()
	if err := app.DB.QueryRow(c, `INSERT INTO merch_products(tag,slug,name,status) VALUES($1,$1,'Bulk setup test','published') RETURNING id::text`, key).Scan(&product); err != nil {
		t.Fatal(err)
	}
	ids := []string{uuid.NewString(), uuid.NewString()}
	sort.Strings(ids)
	for _, id := range ids {
		if _, err := app.DB.Exec(c, `INSERT INTO merch_variants(id,product_id,sku,label) VALUES($1,$2,$3,'Test size')`, id, product, id); err != nil {
			t.Fatal(err)
		}
		if err := AdjustMerchInventory(app, id, "initial", 2, "", "test"); err != nil {
			t.Fatal(err)
		}
	}
	operation := uuid.NewString()
	items := []POSItemUpdate{{VariantID: ids[0], Enabled: true, PriceSats: 1000, Transfer: 1}, {VariantID: ids[1], Enabled: true, PriceSats: 2000, Transfer: 3}}
	if err := POSConfigureItems(app, conf, "", operation, items); err == nil {
		t.Fatal("accepted insufficient stock")
	}
	var count int
	if err := app.DB.QueryRow(c, `SELECT count(*) FROM conference_pos_stock WHERE conference_id=$1`, conf).Scan(&count); err != nil || count != 0 {
		t.Fatalf("failed bulk save left partial rows: %d %v", count, err)
	}
	for _, id := range ids {
		var stock int
		if err := app.DB.QueryRow(c, `SELECT sum(quantity_delta) FROM merch_inventory_events WHERE variant_id=$1`, id).Scan(&stock); err != nil || stock != 2 {
			t.Fatalf("partial inventory transfer: %d %v", stock, err)
		}
	}
	items[1].Transfer = 1
	for i := 0; i < 2; i++ {
		if err := POSConfigureItems(app, conf, "", operation, items); err != nil {
			t.Fatal(err)
		}
	}
	var available int
	if err := app.DB.QueryRow(c, `SELECT sum(available) FROM conference_pos_stock WHERE conference_id=$1`, conf).Scan(&available); err != nil || available != 2 {
		t.Fatalf("bulk save/replay stock: %d %v", available, err)
	}
	if err := app.DB.QueryRow(c, `SELECT count(*) FROM conference_pos_events WHERE conference_id=$1 AND operation_id=$2`, conf, operation).Scan(&count); err != nil || count != 2 {
		t.Fatalf("bulk audit/replay: %d %v", count, err)
	}
}
