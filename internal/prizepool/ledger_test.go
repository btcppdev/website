package prizepool

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestIdentifier(t *testing.T) {
	cases := []struct{ in, want string }{
		{`[["text/plain","Community berlin26@zap.btcplusplus.dev"],["text/identifier","berlin26@zap.btcplusplus.dev"]]`, "berlin26@zap.btcplusplus.dev"},
		{`{"kind":9734,"content":"berlin26@zap.btcplusplus.dev"}`, ""},
		{`[["text/plain","berlin26@zap.btcplusplus.dev"]]`, ""},
		{`[["text/identifier","berlin26@zap.btcplusplus.dev"],["text/identifier","other@zap.btcplusplus.dev"]]`, ""},
		{`[["text/identifier","berlin26@evil.example"]]`, "berlin26@evil.example"},
		{`[["text/identifier","../bad@zap.btcplusplus.dev"]]`, ""},
	}
	for _, c := range cases {
		if got := Identifier(c.in); got != c.want {
			t.Errorf("Identifier(%q)=%q, want %q", c.in, got, c.want)
		}
	}
}
func TestMSat(t *testing.T) {
	for _, v := range []string{`1234`, `"1234msat"`} {
		var m MSat
		if err := json.Unmarshal([]byte(v), &m); err != nil || m != 1234 {
			t.Fatalf("%s: %d %v", v, m, err)
		}
	}
	if FormatMSat(1234) != "1.234" {
		t.Fatal("lost precision")
	}
}
func testDB(t *testing.T) *pgxpool.Pool {
	t.Helper()
	url := os.Getenv("PRIZE_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("set PRIZE_TEST_DATABASE_URL to a disposable PostgreSQL database")
	}
	ctx := context.Background()
	db, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	schema := "prize_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, err = db.Exec(ctx, `CREATE SCHEMA `+schema); err != nil {
		t.Fatal(err)
	}
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = schema
	isolated, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		isolated.Close()
		_, _ = db.Exec(context.Background(), `DROP SCHEMA `+schema+` CASCADE`)
		db.Close()
	})
	_, err = isolated.Exec(ctx, `CREATE TABLE conferences(id uuid PRIMARY KEY); CREATE TABLE prizes(id uuid PRIMARY KEY)`)
	if err != nil {
		t.Fatal(err)
	}
	migration, err := os.ReadFile("../../db/migrations/105_community_prize_pools.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = isolated.Exec(ctx, string(migration)); err != nil {
		t.Fatal(err)
	}
	migration, err = os.ReadFile("../../db/migrations/106_community_node_config.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = isolated.Exec(ctx, string(migration)); err != nil {
		t.Fatal(err)
	}
	return isolated
}
func seedPool(t *testing.T, db *pgxpool.Pool, slug, offer string) string {
	t.Helper()
	ctx := context.Background()
	conf := uuid.NewString()
	if _, err := db.Exec(ctx, `INSERT INTO conferences VALUES($1)`, conf); err != nil {
		t.Fatal(err)
	}
	var id string
	err := db.QueryRow(ctx, `INSERT INTO community_prize_pools(conference_id,slug,node_id,description,offer_id,status) VALUES($1,$2,'node','prizes',$3,'open') RETURNING id::text`, conf, slug, offer).Scan(&id)
	if err != nil {
		t.Fatal(err)
	}
	return id
}
func TestReceiptReplayAndConcurrentCredit(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	pool := seedPool(t, db, "berlin26", strings.Repeat("b", 64))
	in := Invoice{Hash: strings.Repeat("a", 64), Index: 1, Amount: 1234567, PaidAt: 1700000000, Status: "paid", Description: `[["text/plain","Prize pool"],["text/identifier","berlin26@zap.btcplusplus.dev"]]`}
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); errs <- Credit(ctx, db, "node", in) }()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	var count, total int64
	if err := db.QueryRow(ctx, `SELECT count(*),sum(r.received_msat)::bigint FROM community_pool_credits c JOIN community_payment_receipts r USING(node_id,payment_hash) WHERE pool_id=$1`, pool).Scan(&count, &total); err != nil {
		t.Fatal(err)
	}
	if count != 1 || total != 1234567 {
		t.Fatalf("double credit: %d %d", count, total)
	}
	// A conflicting replay must fail without mutating the credited amount or cursor.
	bad := in
	bad.Amount++
	if Credit(ctx, db, "node", bad) == nil {
		t.Fatal("accepted conflicting replay")
	}
	// BOLT12 is attributed without any website-created invoice or description.
	bolt := in
	bolt.Hash = strings.Repeat("c", 64)
	bolt.Index = 2
	bolt.Description = ""
	bolt.OfferID = strings.Repeat("b", 64)
	if err := Credit(ctx, db, "node", bolt); err != nil {
		t.Fatal(err)
	}
	// Retain unmatched receipts and attribute later when mapping appears.
	pending := in
	pending.Hash = strings.Repeat("d", 64)
	pending.Index = 3
	pending.Description = `[["text/identifier","new26@zap.btcplusplus.dev"]]`
	if err := Credit(ctx, db, "node", pending); err != nil {
		t.Fatal(err)
	}
	seedPool(t, db, "new26", "")
	if err := Reattribute(ctx, db, "node"); err != nil {
		t.Fatal(err)
	}
	// Closing must not lose funds received later.
	if _, err := db.Exec(ctx, `UPDATE community_prize_pools SET status='closed',closed_at=$2 WHERE id=$1`, pool, time.Unix(in.PaidAt-1, 0)); err != nil {
		t.Fatal(err)
	}
	late := in
	late.Hash = strings.Repeat("e", 64)
	late.Index = 4
	if err := Credit(ctx, db, "node", late); err != nil {
		t.Fatal(err)
	}
	var isLate bool
	if err := db.QueryRow(ctx, `SELECT late FROM community_pool_credits WHERE payment_hash=$1`, late.Hash).Scan(&isLate); err != nil || !isLate {
		t.Fatalf("late=%v %v", isLate, err)
	}
	// Full-history replay after a simulated restart preserves all four credits.
	for _, v := range []Invoice{in, bolt, pending, late} {
		if err := Credit(ctx, db, "node", v); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.QueryRow(ctx, `SELECT count(*) FROM community_pool_credits`).Scan(&count); err != nil || count != 4 {
		t.Fatalf("credits=%d err=%v", count, err)
	}
	wrong := in
	wrong.Hash = strings.Repeat("f", 64)
	wrong.Index = 5
	wrong.OfferID = strings.Repeat("9", 64)
	if err := Credit(ctx, db, "node", wrong); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(ctx, `SELECT count(*) FROM community_pool_credits WHERE payment_hash=$1`, wrong.Hash).Scan(&count); err != nil || count != 0 {
		t.Fatal("conflicting offer and address credited")
	}
}

func TestOnChainReceiptsNeverFundLightningPool(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	seedPool(t, db, "berlin26", strings.Repeat("b", 64))
	in := Invoice{Hash: strings.Repeat("a", 64), Index: 1, Status: "paid", PaidAt: 1700000000, Amount: 10000, OfferID: strings.Repeat("b", 64), PaidOutpoint: json.RawMessage(`{"txid":"abc","outnum":0}`)}
	for i := 0; i < 2; i++ {
		if err := Credit(ctx, db, "node", in); err != nil {
			t.Fatal(err)
		}
	}
	var count int
	if err := db.QueryRow(ctx, `SELECT count(*) FROM community_pool_credits`).Scan(&count); err != nil || count != 0 {
		t.Fatal("on-chain contribution credited")
	}
}

func TestCommunityMilestoneGoals(t *testing.T) {
	for _, c := range []struct{ msat, goal, progress int64 }{{0, 1000000, 0}, {999999999, 1000000, 99}, {1000000000, 2000000, 50}, {1999999999, 2000000, 99}, {9000000000, 10000000, 90}, {10000000000, 10000000, 100}, {15000000000, 10000000, 100}} {
		p := Pool{TotalMSat: c.msat}
		if p.GoalSats() != c.goal || p.Progress() != c.progress {
			t.Errorf("%d msat: goal %d progress %d", c.msat, p.GoalSats(), p.Progress())
		}
	}
}

func TestPaymentHistoryDetailsAndIsolation(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	pool := seedPool(t, db, "berlin26", strings.Repeat("b", 64))
	other := seedPool(t, db, "other", strings.Repeat("c", 64))
	in := Invoice{Hash: strings.Repeat("a", 64), Index: 1, Amount: 1234567, PaidAt: 1700000000, Status: "paid", OfferID: strings.Repeat("b", 64), Description: "Community prize", PayerNote: "Keep building <script>"}
	for i := 0; i < 2; i++ {
		if err := Credit(ctx, db, "node", in); err != nil {
			t.Fatal(err)
		}
	}
	payments, err := ListPayments(ctx, db, pool, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(payments) != 1 || payments[0].Note != in.PayerNote || payments[0].Description != in.Description || payments[0].Sats() != "1234.567" || payments[0].Method() != "BOLT12" {
		t.Fatalf("bad history: %#v", payments)
	}
	payments, err = ListPayments(ctx, db, other, 0)
	if err != nil || len(payments) != 0 {
		t.Fatalf("cross-pool disclosure: %#v %v", payments, err)
	}
	// Enough receipts to cross the page boundary; each credit retains a unique hash.
	for i := 2; i <= 53; i++ {
		in.Hash = fmt.Sprintf("%064x", i)
		in.Index = int64(i)
		in.PaidAt += 1
		if err := Credit(ctx, db, "node", in); err != nil {
			t.Fatal(err)
		}
	}
	payments, err = ListPayments(ctx, db, pool, 0)
	if err != nil || len(payments) != 51 {
		t.Fatalf("page bound: %d %v", len(payments), err)
	}
	next, err := ListPayments(ctx, db, pool, 50)
	if err != nil || len(next) != 3 || next[0].Hash != payments[50].Hash {
		t.Fatalf("pagination: %#v %v", next, err)
	}
}
func TestDraftSetupWithoutCredentials(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	conf := uuid.NewString()
	if _, err := db.Exec(ctx, `INSERT INTO conferences VALUES($1)`, conf); err != nil {
		t.Fatal(err)
	}
	settings := Settings{Domain: "zap.btcplusplus.dev"}
	for i := 0; i < 2; i++ {
		if err := Create(ctx, db, conf, "builders", "Fund Bitcoin builders", "admin", settings); err != nil {
			t.Fatal(err)
		}
	}
	if err := Create(ctx, db, conf, "changed", "Different description", "admin", settings); err == nil {
		t.Fatal("silently accepted a conflicting setup retry")
	}
	p, err := Load(ctx, db, conf)
	if err != nil {
		t.Fatal(err)
	}
	if p.Status != "draft" || p.Offer != "" || p.EndpointCreated || p.DNSPublished || p.NodeID != "" {
		t.Fatalf("setup did more than save a draft: %#v", p)
	}
}

func TestFundingNotificationsFollowCommit(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	pool := seedPool(t, db, "notify", "")
	conn, err := pgx.ConnectConfig(ctx, db.Config().ConnConfig.Copy())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(ctx)
	if _, err = conn.Exec(ctx, "LISTEN btcpp_community_pools"); err != nil {
		t.Fatal(err)
	}
	tx, err := db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, `UPDATE community_prize_pools SET description='uncommitted' WHERE id=$1`, pool); err != nil {
		t.Fatal(err)
	}
	wait, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
	_, err = conn.WaitForNotification(wait)
	cancel()
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("notification before commit: %v", err)
	}
	if err = tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	wait, cancel = context.WithTimeout(ctx, 100*time.Millisecond)
	_, err = conn.WaitForNotification(wait)
	cancel()
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("notification after rollback: %v", err)
	}
	if _, err = db.Exec(ctx, `UPDATE community_prize_pools SET description='committed' WHERE id=$1`, pool); err != nil {
		t.Fatal(err)
	}
	wait, cancel = context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	notification, err := conn.WaitForNotification(wait)
	if err != nil {
		t.Fatal(err)
	}
	if notification.Channel != "btcpp_community_pools" || notification.Payload != "" {
		t.Fatalf("unexpected notification: %#v", notification)
	}
}
