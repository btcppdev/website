package prizepool

import (
	bip353 "btcpp-web/internal/bip353"
	"context"
	"errors"
	"strings"
	"testing"
)

type fakeDNS struct {
	record *bip353.Record
	fail   bool
}

func (d *fakeDNS) Get(ctx context.Context, user string) (bip353.Record, error) {
	if d.record == nil {
		return bip353.Record{}, bip353.ErrNotFound
	}
	return *d.record, nil
}
func (d *fakeDNS) Put(ctx context.Context, e bip353.Entry) (bip353.Result, error) {
	if d.fail {
		return bip353.Result{}, errors.New("DNS unavailable")
	}
	d.record = &bip353.Record{Content: e.URI}
	return bip353.Result{Record: *d.record}, nil
}
func TestProvisionRetryAndDestinationConflict(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	seedPool(t, db, "berlin26", "")
	var conf string
	if err := db.QueryRow(ctx, `UPDATE community_prize_pools SET status='draft',description='Berlin community prizes' RETURNING conference_id::text`).Scan(&conf); err != nil {
		t.Fatal(err)
	}
	offers, adds := 0, 0
	hasEndpoint := false
	rpc := fakeRPC{func(m string, p any) (any, error) {
		switch m {
		case "getinfo":
			return map[string]string{"id": "node", "network": "bitcoin"}, nil
		case "offer":
			offers++
			return map[string]any{"offer_id": strings.Repeat("b", 64), "bolt12": "lno1example", "active": true}, nil
		case "clnurl-list":
			if hasEndpoint {
				return map[string]any{"endpoints": []map[string]string{{"name": "berlin26", "description": "Berlin community prizes"}}}, nil
			}
			return map[string]any{"endpoints": []any{}}, nil
		case "clnurl-add":
			adds++
			hasEndpoint = true
			return map[string]any{}, nil
		}
		return nil, errors.New("unexpected method")
	}}
	s := Settings{NodeID: "node", Domain: "zap.btcplusplus.dev", Network: "bitcoin"}
	dns := &fakeDNS{fail: true}
	if Provision(ctx, db, conf, "admin", s, rpc, dns) == nil {
		t.Fatal("expected DNS failure")
	}
	dns.fail = false
	if err := Provision(ctx, db, conf, "admin", s, rpc, dns); err != nil {
		t.Fatal(err)
	}
	if offers != 1 || adds != 1 {
		t.Fatalf("duplicated setup: offers=%d endpoints=%d", offers, adds)
	}
	dns.record.Content = "bitcoin:?lno=lno1somebodyelse"
	if Provision(ctx, db, conf, "admin", s, rpc, dns) == nil {
		t.Fatal("overwrote existing address")
	}
	if dns.record.Content != "bitcoin:?lno=lno1somebodyelse" {
		t.Fatal("DNS changed")
	}
}

func TestClosingRetryKeepsCutoff(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	pool := seedPool(t, db, "berlin26", strings.Repeat("b", 64))
	var conf string
	if err := db.QueryRow(ctx, `SELECT conference_id::text FROM community_prize_pools WHERE id=$1`, pool).Scan(&conf); err != nil {
		t.Fatal(err)
	}
	fail := true
	rpc := fakeRPC{func(m string, p any) (any, error) {
		if m == "disableoffer" && fail {
			return nil, errors.New("offline")
		}
		if m == "clnurl-list" {
			return map[string]any{"endpoints": []any{}}, nil
		}
		return map[string]any{}, nil
	}}
	if ClosePool(ctx, db, conf, "admin", rpc) == nil {
		t.Fatal("expected offline error")
	}
	var cutoff string
	if err := db.QueryRow(ctx, `SELECT closed_at::text FROM community_prize_pools WHERE id=$1`, pool).Scan(&cutoff); err != nil {
		t.Fatal(err)
	}
	fail = false
	if err := ClosePool(ctx, db, conf, "admin", rpc); err != nil {
		t.Fatal(err)
	}
	var status, after string
	if err := db.QueryRow(ctx, `SELECT status,closed_at::text FROM community_prize_pools WHERE id=$1`, pool).Scan(&status, &after); err != nil || status != "closed" || after != cutoff {
		t.Fatalf("status=%s cutoff moved=%v %v", status, after != cutoff, err)
	}
}
