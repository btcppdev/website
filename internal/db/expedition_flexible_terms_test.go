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

func TestExpeditionFlexibleTermsMigration(t *testing.T) {
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
	version := "flexible-migration-test-" + time.Now().Format("150405.000000000")
	terms := "Choose full payment at signup or five installments: at booking, January 1, April 1, July 1 and October 1, 2027. Refunds & <insurance> unchanged.\nUnicode: \u2028\u2029"
	privacy := "Keep all privacy terms."
	raw, _ := json.Marshal([]string{terms, privacy})
	digest := sha256.Sum256(raw)
	exec(`INSERT INTO service_legal_revisions(product_code,version,terms_text,privacy_text,content_sha256,effective_at) VALUES($1,$2,$3,$4,$5,now()-interval '1 day')`, product, version, terms, privacy, hex.EncodeToString(digest[:]))
	exec(`INSERT INTO service_legal_current(product_code,version) VALUES($1,$2) ON CONFLICT(product_code) DO UPDATE SET version=excluded.version`, product, version)
	exec(`INSERT INTO expedition_lifecycle_policies(product_code,version,time_zone) VALUES($1,$2,'America/Chicago')`, product, version)
	migration, err := os.ReadFile("../../db/migrations/148_expedition_flexible_installments.sql")
	if err != nil {
		t.Fatal(err)
	}
	exec(`INSERT INTO expedition_inventory(berth,capacity,sales_open,booking_closes_at,hold_seconds) VALUES('shared',18,true,'2027-01-01T00:00:00Z',3600),('triple',3,true,'2027-08-01T00:00:00Z',3600) ON CONFLICT(berth) DO UPDATE SET booking_closes_at=excluded.booking_closes_at`)
	exec(string(migration))
	var extended, custom time.Time
	if err := tx.QueryRow(ctx, `SELECT booking_closes_at FROM expedition_inventory WHERE berth='shared'`).Scan(&extended); err != nil {
		t.Fatal(err)
	}
	if err := tx.QueryRow(ctx, `SELECT booking_closes_at FROM expedition_inventory WHERE berth='triple'`).Scan(&custom); err != nil {
		t.Fatal(err)
	}
	if extended.UTC().Format(time.RFC3339) != "2027-10-02T05:00:00Z" || custom.UTC().Format(time.RFC3339) != "2027-08-01T00:00:00Z" {
		t.Fatal("incorrect inventory cutoff", extended, custom)
	}
	var current, gotTerms, gotPrivacy, gotDigest, zone string
	err = tx.QueryRow(ctx, `SELECT c.version,r.terms_text,r.privacy_text,r.content_sha256,l.time_zone FROM service_legal_current c JOIN service_legal_revisions r USING(product_code,version) JOIN expedition_lifecycle_policies l USING(product_code,version) WHERE c.product_code=$1`, product).Scan(&current, &gotTerms, &gotPrivacy, &gotDigest, &zone)
	if err != nil {
		t.Fatal(err)
	}
	wantTerms := strings.ReplaceAll(terms, "Choose full payment at signup or five installments: at booking, January 1, April 1, July 1 and October 1, 2027.", "Choose full payment at signup or equal installments across signup and the remaining scheduled dates: January 1, April 1, July 1 and October 1, 2027. Dates on or before the signup date (America/Chicago) are omitted. Any rounding difference is included in the final payment. Your payment schedule is fixed when you book. If no scheduled dates remain, payment in full is required. The non-refundable portion remains 20% of the passage fare even if the signup payment is larger.")
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
