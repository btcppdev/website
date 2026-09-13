package getters

import (
	"context"
	"github.com/google/uuid"
	"strings"
	"testing"
)

func TestPOSAccountOwnership(t *testing.T) {
	app := databaseSmokeContext(t)
	conf, _ := insertSmokeConference(t, app)
	c := context.Background()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	var owner, other, sale string
	must(app.DB.QueryRow(c, `INSERT INTO people(name) VALUES('POS owner') RETURNING id::text`).Scan(&owner))
	must(app.DB.QueryRow(c, `INSERT INTO people(name) VALUES('POS other') RETURNING id::text`).Scan(&other))
	email := uuid.NewString() + "@example.test"
	otherEmail := uuid.NewString() + "@example.test"
	_, err := app.DB.Exec(c, `INSERT INTO person_emails(person_id,email,is_primary) VALUES($1,$2,false),($3,$4,true)`, owner, email, other, otherEmail)
	must(err)
	must(app.DB.QueryRow(c, `INSERT INTO conference_pos_sales(conference_id,request_id,operator_id,total_sats,currency,local_per_btc) VALUES($1,$2,'test',50000,'EUR',80000) RETURNING id::text`, conf, uuid.NewString()).Scan(&sale))
	must(POSLinkVerifiedBuyer(app, sale, conf, email))
	if _, _, err := POSPurchaseForPerson(app, owner, sale); err == nil {
		t.Fatal("unpaid purchase linked")
	}
	_, err = app.DB.Exec(c, `UPDATE conference_pos_sales SET status='paid' WHERE id=$1`, sale)
	must(err)
	must(POSLinkVerifiedBuyer(app, sale, conf, "unknown@example.test"))
	if _, _, err := POSPurchaseForPerson(app, owner, sale); err == nil {
		t.Fatal("unknown email linked")
	}
	must(POSLinkVerifiedBuyer(app, sale, uuid.NewString(), email))
	if _, _, err := POSPurchaseForPerson(app, owner, sale); err == nil {
		t.Fatal("wrong conference linked")
	}
	must(POSLinkVerifiedBuyer(app, sale, conf, " "+strings.ToUpper(email)+" "))
	purchases, err := POSPurchasesForPerson(app, owner, 100)
	must(err)
	if len(purchases) != 1 || purchases[0].TotalSats != 50000 || purchases[0].LocalEstimate != 40 {
		t.Fatalf("wrong purchases %+v", purchases)
	}
	must(POSLinkVerifiedBuyer(app, sale, conf, otherEmail))
	if _, _, err := POSPurchaseForPerson(app, other, sale); err == nil {
		t.Fatal("resend transferred ownership")
	}
	if _, _, err := POSPurchaseForPerson(app, owner, sale); err != nil {
		t.Fatal(err)
	}
}
