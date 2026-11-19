package handlers

import (
	"context"
	"errors"
	"html/template"
	"os"
	"strings"
	"testing"

	"btcpp-web/external/getters"
	"btcpp-web/internal/config"
	"btcpp-web/internal/emails"
	"btcpp-web/internal/types"
	"github.com/google/uuid"
)

func TestOrganizationSaleEmail(t *testing.T) {
	tmpl, err := template.New("notice").Funcs(template.FuncMap{"shopFulfillmentLabel": shopFulfillmentLabel}).ParseFiles("../../templates/shop/organization_sale_email.tmpl")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = tmpl.AddParseTree("shop/organization_sale_email.tmpl", tmpl.Lookup("organization_sale_email.tmpl").Tree); err != nil {
		t.Fatal(err)
	}
	app := &config.AppContext{Env: &types.EnvConfig{Prod: true, Host: "btcpp.dev"}, TemplateCache: tmpl}
	n := &getters.OrganizationSaleNotice{ID: uuid.NewString(), OrganizationID: uuid.NewString(), OrganizationName: "Tokyo makers", Reference: "SALE-123", Currency: "sats", Event: "Tokyo", HasSupplierAmount: true, SupplierAmount: 8500, Items: []getters.OrganizationSaleItem{{Name: "Tee <script>", Variant: "Large", Quantity: 2, Fulfillment: "pos_takeaway"}}}
	one, err := organizationSaleMail(app, n, "OWNER@example.test")
	if err != nil {
		t.Fatal(err)
	}
	two, err := organizationSaleMail(app, n, "owner@example.test")
	if err != nil {
		t.Fatal(err)
	}
	if one.JobKey != two.JobKey || one.Email != "owner@example.test" {
		t.Fatal("unstable recipient key")
	}
	for _, body := range []string{string(one.HTMLBody), string(one.TextBody)} {
		for _, want := range []string{"8500 sats", "Tokyo", "Point of sale", "https://btcpp.dev/dashboard/orgs/" + n.OrganizationID + "/shop?tab=orders"} {
			if !strings.Contains(body, want) {
				t.Fatalf("missing %q", want)
			}
		}
	}
	if strings.Contains(string(one.HTMLBody), "Tee <script>") || !strings.Contains(string(one.HTMLBody), "Tee &lt;script&gt;") {
		t.Fatal("unescaped product")
	}
	n.ID = uuid.NewString()
	other, err := organizationSaleMail(app, n, "owner@example.test")
	if err != nil {
		t.Fatal(err)
	}
	if one.JobKey == other.JobKey {
		t.Fatal("different purchases share delivery ID")
	}
}

func TestOrganizationSaleNotifications(t *testing.T) {
	app := wizardTestApp(t)
	c := context.Background()
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := app.DB.Exec(c, q, args...); err != nil {
			t.Fatal(err)
		}
	}
	orgA, orgB := uuid.NewString(), uuid.NewString()
	orderID, reference := uuid.NewString(), "SHOP-"+uuid.NewString()
	people := []string{}
	t.Cleanup(func() {
		app.DB.Exec(c, `DELETE FROM shop_orders WHERE id=$1`, orderID)
		app.DB.Exec(c, `DELETE FROM organizations WHERE id IN ($1,$2)`, orgA, orgB)
		for _, person := range people {
			app.DB.Exec(c, `DELETE FROM people WHERE id=$1`, person)
		}
	})
	exec(`INSERT INTO organizations(id,name) VALUES($1,'Alpha <makers>'),($2,'Bravo goods')`, orgA, orgB)
	member := func(org, role, status string, verified bool) (string, string) {
		t.Helper()
		person := uuid.NewString()
		people = append(people, person)
		email := person + "@example.test"
		exec(`INSERT INTO people(id,name) VALUES($1,'Notice recipient')`, person)
		if verified {
			exec(`INSERT INTO person_emails(person_id,email,is_primary,verified_at) VALUES($1,$2,true,now())`, person, email)
		}
		exec(`INSERT INTO organization_memberships(organization_id,person_id,role,status) VALUES($1,$2,$3,$4)`, org, person, role, status)
		return person, email
	}
	ownerID, owner := member(orgA, "owner", "active", true)
	_, secondOwner := member(orgA, "owner", "active", true)
	managerID, _ := member(orgA, "manager", "active", false)
	manager := "verified-" + managerID + "@example.test"
	exec(`INSERT INTO person_emails(person_id,email,verified_at,created_at) VALUES($1,$2,now(),now()-interval '1 day'),($1,$3,now(),now())`, managerID, manager, "extra-"+manager)
	member(orgA, "manager", "removed", true)
	member(orgA, "member", "active", true)
	member(orgA, "manager", "active", false)
	_, otherOwner := member(orgB, "owner", "active", true)
	// A person managing both orgs gets separate, correctly scoped notices.
	exec(`INSERT INTO organization_memberships(organization_id,person_id,role) VALUES($1,$2,'manager')`, orgB, ownerID)
	exec(`INSERT INTO shop_orders(id,public_id,buyer_email,status,currency,total_cents) VALUES($1,$2,'buyer@example.test','pending','USD',8500)`, orderID, reference)
	for i, org := range []string{orgA, orgA, orgB} {
		name, price, supplier := "Alpha tee", 2000, 1700
		if i == 1 {
			name = "Alpha cap"
		}
		if i == 2 {
			name, price, supplier = "Bravo-only sticker", 4000, 3400
		}
		exec(`INSERT INTO shop_order_items(order_id,fulfillment_organization_id,quantity,unit_price_cents,line_total_cents,product_name_snapshot,variant_label_snapshot,fulfillment_method,organization_fee_bps,organization_fee_cents,supplier_amount_cents)
 VALUES($1,$2,1,$3,$3,$4,'Default','ship',1500,$5,$6)`, orderID, org, price, name, price-supplier, supplier)
	}
	exec(`INSERT INTO shop_order_items(order_id,quantity,unit_price_cents,line_total_cents,product_name_snapshot,fulfillment_method) VALUES($1,1,500,500,'bitcoin++-only pin','ship')`, orderID)
	var count int
	if err := app.DB.QueryRow(c, `SELECT count(*) FROM organization_sale_notifications WHERE order_id=$1`, orderID).Scan(&count); err != nil || count != 0 {
		t.Fatal("unpaid order notified", count, err)
	}
	for range 2 {
		if _, err := getters.MarkShopOrderPaid(app, orderID, "stripe", reference, 0, 8500); err != nil {
			t.Fatal(err)
		}
	}
	if err := app.DB.QueryRow(c, `SELECT count(*) FROM organization_sale_notifications WHERE order_id=$1`, orderID).Scan(&count); err != nil || count != 2 {
		t.Fatal("expected one notice per org after replay", count, err)
	}
	var noticeA, noticeB string
	for org, target := range map[string]*string{orgA: &noticeA, orgB: &noticeB} {
		if err := app.DB.QueryRow(c, `SELECT id::text FROM organization_sale_notifications WHERE order_id=$1 AND organization_id=$2`, orderID, org).Scan(target); err != nil {
			t.Fatal(err)
		}
	}
	// MAIL_OFF must leave notices pending and unclaimed.
	processOrganizationSaleNotifications(app)
	var untouched bool
	if err := app.DB.QueryRow(c, `SELECT sent_at IS NULL AND next_attempt_at=created_at FROM organization_sale_notifications WHERE id=$1`, noticeA).Scan(&untouched); err != nil || !untouched {
		t.Fatal("mail-off changed queue", err)
	}
	allowed := map[string]bool{owner: true, secondOwner: true, manager: true}
	jobs, recipients := map[string]bool{}, map[string]bool{}
	fail := true
	sendA := func(_ *config.AppContext, m *emails.Mail) error {
		if !allowed[m.Email] {
			t.Fatalf("unexpected recipient %s", m.Email)
		}
		for _, body := range []string{string(m.HTMLBody), string(m.TextBody)} {
			if strings.Contains(body, "Bravo-only") || strings.Contains(body, "bitcoin++-only") || strings.Contains(body, orgB) {
				t.Fatal("another seller leaked into notice")
			}
			for _, want := range []string{"Alpha tee", "Alpha cap", "34.00 USD", "Ship to buyer", "/dashboard/orgs/" + orgA + "/shop?tab=orders"} {
				if !strings.Contains(body, want) {
					t.Fatalf("missing %q", want)
				}
			}
		}
		if strings.Contains(string(m.HTMLBody), "Alpha <makers>") {
			t.Fatal("organization name was not escaped")
		}
		if fail && m.Email == manager {
			return errors.New("temporary mailer failure")
		}
		jobs[m.JobKey], recipients[m.Email] = true, true
		if os.Getenv("ORG_SALE_NOTICE_PREVIEW") == "1" {
			if err := os.WriteFile("/private/tmp/organization-sale-email.html", m.HTMLBody, 0600); err != nil {
				t.Fatal(err)
			}
		}
		return nil
	}
	if err := deliverOrganizationSaleNotification(app, noticeA, sendA); err == nil {
		t.Fatal("partial failure marked successful")
	}
	var complete bool
	if err := app.DB.QueryRow(c, `SELECT sent_at IS NOT NULL FROM organization_sale_notifications WHERE id=$1`, noticeA).Scan(&complete); err != nil || complete {
		t.Fatal("failed notice marked complete", err)
	}
	fail = false
	if err := deliverOrganizationSaleNotification(app, noticeA, sendA); err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 3 || len(recipients) != 3 {
		t.Fatal("missing owners/manager or unstable retry IDs", len(jobs), len(recipients))
	}
	if err := app.DB.QueryRow(c, `SELECT sent_at IS NOT NULL FROM organization_sale_notifications WHERE id=$1`, noticeA).Scan(&complete); err != nil || !complete {
		t.Fatal("successful notice pending", err)
	}
	// Re-read current memberships on a retry: removed managers cannot receive mail.
	exec(`UPDATE organization_memberships SET status='removed' WHERE organization_id=$1 AND person_id=$2`, orgB, ownerID)
	if err := deliverOrganizationSaleNotification(app, noticeB, func(_ *config.AppContext, m *emails.Mail) error {
		if m.Email != otherOwner {
			t.Fatal("removed manager notified")
		}
		if jobs[m.JobKey] || !strings.Contains(string(m.TextBody), "Bravo-only sticker") || strings.Contains(string(m.TextBody), "Alpha tee") {
			t.Fatal("notice not isolated by organization")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	// No verified recipient must leave the notice retryable, not lose it.
	exec(`UPDATE organization_sale_notifications SET sent_at=NULL WHERE id=$1`, noticeB)
	exec(`UPDATE organization_memberships SET status='removed' WHERE organization_id=$1`, orgB)
	if err := deliverOrganizationSaleNotification(app, noticeB, func(_ *config.AppContext, _ *emails.Mail) error { t.Fatal("sent without recipients"); return nil }); err == nil {
		t.Fatal("missing recipients silently discarded")
	}
	// Exercise the production mail renderer without fabricating an old payout split.
	n, err := getters.GetOrganizationSaleNotice(app, noticeA)
	if err != nil {
		t.Fatal(err)
	}
	n.HasSupplierAmount = false
	m, err := organizationSaleMail(app, n, owner)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(m.TextBody), "34.00") {
		t.Fatal("invented earnings for missing historical terms")
	}
}
