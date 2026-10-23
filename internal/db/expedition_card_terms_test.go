package db

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestExpeditionCardTermsMigration(t *testing.T) {
	uri := os.Getenv("EXPEDITION_COMMERCE_TEST_DATABASE_URL")
	if uri == "" {
		t.Skip("isolated migrated PostgreSQL required")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, uri)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := tx.Exec(ctx, q, args...); err != nil {
			t.Fatal(err)
		}
	}
	product := "expedition.antarctica2028.passage"
	version := "card-migration-test-" + time.Now().Format("150405.000000000")
	terms := "Launch purchases are Bitcoin-only. Refunds & <insurance> unchanged.\nUnicode: \u2028\u2029"
	privacy := "Keep all privacy terms."
	raw, _ := json.Marshal([]string{terms, privacy})
	digest := sha256.Sum256(raw)
	exec(`INSERT INTO service_legal_revisions(product_code,version,terms_text,privacy_text,content_sha256,effective_at) VALUES($1,$2,$3,$4,$5,now()-interval '1 day')`, product, version, terms, privacy, hex.EncodeToString(digest[:]))
	exec(`INSERT INTO service_legal_current(product_code,version) VALUES($1,$2) ON CONFLICT(product_code) DO UPDATE SET version=excluded.version`, product, version)
	exec(`INSERT INTO expedition_lifecycle_policies(product_code,version,time_zone) VALUES($1,$2,'America/Chicago')`, product, version)
	migration, err := os.ReadFile("../../db/migrations/143_expedition_card_payment_terms.sql")
	if err != nil {
		t.Fatal(err)
	}
	exec(string(migration))
	var current, gotTerms, gotPrivacy, gotDigest, zone string
	err = tx.QueryRow(ctx, `SELECT c.version,r.terms_text,r.privacy_text,r.content_sha256,l.time_zone FROM service_legal_current c JOIN service_legal_revisions r USING(product_code,version) JOIN expedition_lifecycle_policies l USING(product_code,version) WHERE c.product_code=$1`, product).Scan(&current, &gotTerms, &gotPrivacy, &gotDigest, &zone)
	if err != nil {
		t.Fatal(err)
	}
	wantTerms := strings.ReplaceAll(terms, "Launch purchases are Bitcoin-only.", "Payment is available in bitcoin or by card via Stripe.")
	raw, _ = json.Marshal([]string{wantTerms, privacy})
	digest = sha256.Sum256(raw)
	if current == version || gotTerms != wantTerms || gotPrivacy != privacy || gotDigest != hex.EncodeToString(digest[:]) || zone != "America/Chicago" {
		t.Fatalf("incorrect policy revision: %s %s %s", current, gotTerms, gotDigest)
	}
	var original string
	if err = tx.QueryRow(ctx, `SELECT terms_text FROM service_legal_revisions WHERE product_code=$1 AND version=$2`, product, version).Scan(&original); err != nil || original != terms {
		t.Fatal("historical terms changed", err)
	}
	exec(string(migration)) // already updated: no second revision or conflict
}
