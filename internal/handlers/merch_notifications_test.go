package handlers

import (
	"btcpp-web/external/getters"
	"btcpp-web/internal/auth"
	"btcpp-web/internal/config"
	"btcpp-web/internal/emails"
	"btcpp-web/internal/types"
	"context"
	"errors"
	"html/template"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func notificationTemplates(t *testing.T) *template.Template {
	t.Helper()
	tmpl, err := template.New("notice").Funcs(template.FuncMap{"merchMoney": merchMoney, "shopFulfillmentLabel": shopFulfillmentLabel}).ParseFiles("../../templates/shop/admin_sale_email.tmpl")
	if err != nil {
		t.Fatal(err)
	}
	// ParseFiles uses the basename; production template loader uses relative paths.
	if _, err := tmpl.AddParseTree("shop/admin_sale_email.tmpl", tmpl.Lookup("admin_sale_email.tmpl").Tree); err != nil {
		t.Fatal(err)
	}
	return tmpl
}

func TestMerchSaleEmail(t *testing.T) {
	ctx := &config.AppContext{Env: &types.EnvConfig{Prod: true, Host: "btcpp.dev"}, TemplateCache: notificationTemplates(t)}
	order := &types.ShopOrder{ID: uuid.NewString(), PublicID: "SHOP-123", Currency: "USD", TotalCents: 6500, Items: []*types.ShopOrderItem{{VariantID: uuid.NewString(), ProductNameSnapshot: "Canvas <script>", VariantLabelSnapshot: "Builder badge", Quantity: 1, FulfillmentMethod: types.ShopFulfillmentShip}}}
	one, err := merchSaleMail(ctx, order, "ADMIN@example.test")
	if err != nil {
		t.Fatal(err)
	}
	two, _ := merchSaleMail(ctx, order, "admin@example.test")
	other, _ := merchSaleMail(ctx, order, "other@example.test")
	if one.JobKey != two.JobKey || one.JobKey == other.JobKey {
		t.Fatal("recipient idempotency keys")
	}
	if strings.Contains(string(one.HTMLBody), "Canvas <script>") || !strings.Contains(string(one.HTMLBody), "Canvas &lt;script&gt;") {
		t.Fatal("unescaped product")
	}
	if !strings.Contains(string(one.TextBody), order.ID) || !strings.Contains(string(one.HTMLBody), "$65") {
		t.Fatal("missing order link or total")
	}
	if os.Getenv("MERCH_NOTICE_PREVIEW") == "1" {
		if err := os.WriteFile("/private/tmp/merch-sale-notification.html", one.HTMLBody, 0600); err != nil {
			t.Fatal(err)
		}
	}
	for _, tc := range []struct {
		tag  string
		want bool
	}{{"merch-admin", true}, {"global-admin", true}, {"berlin-admin", false}, {"merch-staff", false}} {
		id := &auth.Identity{Roles: auth.ParseRoles([]string{tc.tag})}
		if id.Satisfies(auth.Spec{Conf: "merch", Role: auth.RoleAdmin}) != tc.want {
			t.Fatal("merch access", tc.tag)
		}
	}
}

func TestMerchSaleNotificationRetry(t *testing.T) {
	if os.Getenv("BTCPP_POSTGRES_SMOKE") != "1" {
		t.Skip("requires migrated local database")
	}
	pool, err := pgxpool.New(context.Background(), os.Getenv("DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	ctx := &config.AppContext{DB: pool, Env: &types.EnvConfig{Prod: true, Host: "btcpp.dev"}, TemplateCache: notificationTemplates(t)}
	suffix := uuid.NewString()
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(context.Background(), q, args...); err != nil {
			t.Fatal(err)
		}
	}
	var admins []string
	for _, role := range []string{"merch", "merch", "global"} {
		id := uuid.NewString()
		admins = append(admins, id)
		exec(`INSERT INTO people(id,name) VALUES($1,'Notice test')`, id)
		exec(`INSERT INTO people_roles(person_id,scope,position) VALUES($1,$2,'admin')`, id, role)
		exec(`INSERT INTO person_emails(person_id,email,is_primary,verified_at) VALUES($1,$2,true,now())`, id, id+"@example.test")
	}
	defer func() {
		for _, id := range admins {
			pool.Exec(context.Background(), `DELETE FROM people WHERE id=$1`, id)
		}
	}()
	product, err := getters.CreateMerchProduct(ctx, getters.MerchProductInput{Tag: suffix, Slug: suffix, Name: "Notice test", BasePriceCents: 6500, Currency: "USD", Status: "published"})
	if err != nil {
		t.Fatal(err)
	}
	variant, err := getters.CreateMerchVariant(ctx, getters.MerchVariantInput{ProductID: product, SKU: suffix, Label: "Default", Status: "active", InventoryPolicy: "unlimited"})
	if err != nil {
		t.Fatal(err)
	}
	order, err := getters.CreateShopOrder(ctx, getters.ShopOrderInput{BuyerEmail: "buyer@example.test", TotalCents: 6500}, []getters.ShopOrderItemInput{{ProductID: product, VariantID: variant, Quantity: 1, UnitPriceCents: 6500, LineTotalCents: 6500, ProductNameSnapshot: "Notice test", FulfillmentMethod: types.ShopFulfillmentShip}})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		pool.Exec(context.Background(), `DELETE FROM shop_orders WHERE id=$1`, order.ID)
		pool.Exec(context.Background(), `DELETE FROM merch_products WHERE id=$1`, product)
	}()
	count := func() int {
		var n int
		if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM merch_sale_notifications WHERE order_id=$1`, order.ID).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	if count() != 0 {
		t.Fatal("unpaid order notified")
	}
	for i := 0; i < 2; i++ {
		if _, err := getters.MarkShopOrderPaid(ctx, order.ID, "stripe", suffix, 0, 6500); err != nil {
			t.Fatal(err)
		}
	}
	if count() != 1 {
		t.Fatal("payment replay duplicated notice")
	}
	sent := map[string]bool{}
	fail := true
	send := func(_ *config.AppContext, m *emails.Mail) error {
		if m.Email == admins[2]+"@example.test" {
			t.Fatal("global admin implicitly notified")
		}
		if fail && m.Email == admins[1]+"@example.test" {
			return errors.New("mailer unavailable")
		}
		sent[m.JobKey] = true
		return nil
	}
	if err := deliverMerchSaleNotification(ctx, order.ID, send); err == nil {
		t.Fatal("partial failure marked successful")
	}
	var complete bool
	if err := pool.QueryRow(context.Background(), `SELECT sent_at IS NOT NULL FROM merch_sale_notifications WHERE order_id=$1`, order.ID).Scan(&complete); err != nil || complete {
		t.Fatal("failed delivery marked complete", err)
	}
	fail = false
	if err := deliverMerchSaleNotification(ctx, order.ID, send); err != nil {
		t.Fatal(err)
	}
	if len(sent) != 2 {
		t.Fatal("expected one stable mail job per merch admin", len(sent))
	}
	if err := pool.QueryRow(context.Background(), `SELECT sent_at IS NOT NULL FROM merch_sale_notifications WHERE order_id=$1`, order.ID).Scan(&complete); err != nil || !complete {
		t.Fatal("successful delivery not marked complete", err)
	}
}
