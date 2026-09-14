package prizepool

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

type fakeRPC struct {
	call func(string, any) (any, error)
}

func (f fakeRPC) Call(ctx context.Context, m string, p, out any) error {
	v, e := f.call(m, p)
	if e != nil {
		return e
	}
	b, _ := json.Marshal(v)
	return json.Unmarshal(b, out)
}
func TestObserverRecoveryAndAtomicCursor(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	seedPool(t, db, "berlin26", strings.Repeat("b", 64))
	in := Invoice{Hash: strings.Repeat("a", 64), Index: 1, Status: "paid", Amount: 10000, PaidAt: 1700000000, OfferID: strings.Repeat("b", 64)}
	fail := true
	rpc := fakeRPC{func(m string, p any) (any, error) {
		switch m {
		case "getinfo":
			return map[string]string{"id": "node", "network": "bitcoin"}, nil
		case "waitanyinvoice":
			return Invoice{Hash: in.Hash, Index: in.Index}, nil
		case "listinvoices":
			if fail {
				return nil, errors.New("disconnected")
			}
			return map[string]any{"invoices": []Invoice{in}}, nil
		}
		return nil, errors.New("unexpected method")
	}}
	s := Settings{NodeID: "node", Network: "bitcoin"}
	if Observe(ctx, db, rpc, s) == nil {
		t.Fatal("disconnect ignored")
	}
	var index int64
	if err := db.QueryRow(ctx, `SELECT pay_index FROM community_payment_cursors WHERE node_id='node'`).Scan(&index); err != nil || index != 0 {
		t.Fatalf("advanced after failure: %d %v", index, err)
	}
	fail = false
	if err := Observe(ctx, db, rpc, s); err != nil {
		t.Fatal(err)
	}
	// Inject a transaction failure after receipt insertion; nothing may be committed.
	_, err := db.Exec(ctx, `CREATE FUNCTION fail_credit() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'simulated crash'; END $$;
 CREATE TRIGGER fail_credit BEFORE INSERT ON community_pool_credits FOR EACH ROW EXECUTE FUNCTION fail_credit()`)
	if err != nil {
		t.Fatal(err)
	}
	second := in
	second.Hash = strings.Repeat("c", 64)
	second.Index = 2
	if Credit(ctx, db, "node", second) == nil {
		t.Fatal("expected simulated crash")
	}
	var count int
	if err = db.QueryRow(ctx, `SELECT count(*) FROM community_payment_receipts WHERE payment_hash=$1`, second.Hash).Scan(&count); err != nil || count != 0 {
		t.Fatal("partial receipt commit")
	}
	if err = db.QueryRow(ctx, `SELECT pay_index FROM community_payment_cursors WHERE node_id='node'`).Scan(&index); err != nil || index != 1 {
		t.Fatal("partial cursor commit")
	}
	if _, err = db.Exec(ctx, `DROP TRIGGER fail_credit ON community_pool_credits`); err != nil {
		t.Fatal(err)
	}
	if err = Credit(ctx, db, "node", second); err != nil {
		t.Fatal(err)
	}
	idle := fakeRPC{func(m string, p any) (any, error) {
		if m == "getinfo" {
			return map[string]string{"id": "node", "network": "bitcoin"}, nil
		}
		return nil, &RPCError{Code: 904}
	}}
	if err = Observe(ctx, db, idle, s); err != nil {
		t.Fatal(err)
	}
	var synced bool
	if err = db.QueryRow(ctx, `SELECT synced_at IS NOT NULL FROM community_payment_cursors WHERE node_id='node'`).Scan(&synced); err != nil || !synced {
		t.Fatal("missing successful catch-up time")
	}
}
