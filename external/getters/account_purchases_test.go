package getters

import (
	"github.com/google/uuid"
	"testing"
)

func TestAccountPurchasesPostgres(t *testing.T) {
	app := databaseSmokeContext(t)
	ctx := app.DatabaseContext()
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := app.DB.Exec(ctx, q, args...); err != nil {
			t.Fatal(err)
		}
	}
	owner, other := uuid.NewString(), uuid.NewString()
	for _, person := range []string{owner, other} {
		exec(`INSERT INTO people(id,name) VALUES($1,'Purchase fixture')`, person)
	}
	t.Cleanup(func() { exec(`DELETE FROM people WHERE id IN ($1,$2)`, owner, other) })
	refs := map[string]bool{}
	for _, kind := range []string{"merch", "ticket", "mixed", "service"} {
		ref := uuid.NewString()
		refs[ref] = true
		exec(`INSERT INTO shop_orders(public_id,buyer_email,buyer_person_id,checkout_kind,total_cents,status,payment_provider,payment_provider_id) VALUES($1,'purchase@example.test',$2,$3,1234,'paid','stripe',$1)`, ref, owner, kind)
		if kind == "ticket" {
			exec(`INSERT INTO registrations(ref_id,checkout_id,person_id,email,amount_paid,currency) VALUES($1,$2,$3,'purchase@example.test',12.34,'USD')`, uuid.NewString(), ref, owner)
		}
	}
	exec(`INSERT INTO shop_orders(public_id,buyer_email,buyer_person_id,total_cents) VALUES($1,'purchase@example.test',$2,999)`, uuid.NewString(), other)
	legacy := uuid.NewString()
	refs[legacy] = true
	exec(`INSERT INTO registrations(ref_id,person_id,email,item_bought,amount_paid,currency,platform,revoked) VALUES($1,$2,'purchase@example.test','Older ticket',20,'EUR','stripe',true)`, legacy, owner)
	conf, _ := insertSmokeConference(t, app)
	sale := uuid.NewString()
	refs[sale] = true
	exec(`INSERT INTO conference_pos_sales(id,conference_id,request_id,operator_id,total_sats,currency,local_per_btc,buyer_person_id,status) VALUES($1,$2,$3,'test',50000,'EUR',80000,$4,'paid')`, sale, conf, uuid.NewString(), owner)
	got, more, err := AccountPurchases(app, owner, 1)
	if err != nil || more || len(got) != 6 {
		t.Fatalf("history: %+v more=%v %v", got, more, err)
	}
	for _, p := range got {
		if !refs[p.Reference] {
			t.Fatalf("foreign or duplicate row: %+v", p)
		}
		delete(refs, p.Reference)
		if p.Kind == "order" && (p.Amount != "12.34" || p.Currency != "USD") {
			t.Fatal(p)
		}
		if p.Kind == "pos" && (p.Amount != "50000" || p.Currency != "sats") {
			t.Fatal(p)
		}
		if p.Reference == legacy && p.Status != "Ticket revoked" {
			t.Fatal(p)
		}
	}
	for i := 0; i < 51; i++ {
		exec(`INSERT INTO shop_orders(public_id,buyer_email,buyer_person_id) VALUES($1,'purchase@example.test',$2)`, uuid.NewString(), owner)
	}
	first, more, err := AccountPurchases(app, owner, 1)
	if err != nil || !more || len(first) != 50 {
		t.Fatal(len(first), more, err)
	}
	second, more, err := AccountPurchases(app, owner, 2)
	if err != nil || more || len(second) != 7 {
		t.Fatal(len(second), more, err)
	}
	seen := map[string]bool{}
	for _, p := range append(first, second...) {
		if seen[p.Reference] {
			t.Fatal("pagination duplicate")
		}
		seen[p.Reference] = true
	}
	exec(`UPDATE people SET is_deleted_account=true WHERE id=$1`, owner)
	got, _, err = AccountPurchases(app, owner, 1)
	if err != nil || len(got) != 0 {
		t.Fatal(got, err)
	}
}
