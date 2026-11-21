package db

import (
	"context"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"path/filepath"
	"testing"
)

func TestEvergreenMigrationPreservesOffersAndSales(t *testing.T) {
	uri := os.Getenv("EVERGREEN_MIGRATION_TEST_DATABASE_URL")
	if uri == "" {
		t.Skip("empty scratch database required")
	}
	c := context.Background()
	pool, err := pgxpool.New(c, uri)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	var populated bool
	if err = pool.QueryRow(c, `SELECT EXISTS(SELECT 1 FROM information_schema.tables WHERE table_schema='public')`).Scan(&populated); err != nil || populated {
		t.Fatal("requires empty database", err)
	}
	source := "../../db/migrations"
	master := t.TempDir()
	files, err := os.ReadDir(source)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		if f.Name() >= "169_" {
			continue
		}
		raw, e := os.ReadFile(filepath.Join(source, f.Name()))
		if e != nil {
			t.Fatal(e)
		}
		if e = os.WriteFile(filepath.Join(master, f.Name()), raw, 0600); e != nil {
			t.Fatal(e)
		}
	}
	if _, err = MigrateDir(c, pool, master, nil); err != nil {
		t.Fatal(err)
	}
	var conf string
	if err = pool.QueryRow(c, `INSERT INTO conferences(tag,timezone,student_ticket_body) VALUES('migration-example','UTC','Bring student ID') RETURNING id::text`).Scan(&conf); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(c, `INSERT INTO conference_tickets(conference_id,ticket_key,tier,base_price,local_price,student_price,max_count,currency,sales_end_at) VALUES($1,'early','Early',200,80,50,2,'EUR',now()+interval '1 day'),($1,'standard','Standard',300,100,60,100,'EUR',now()+interval '2 days'),($1,'undated','Legacy undated',999,333,222,10,'EUR',NULL)`, conf); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(c, `INSERT INTO registrations(ref_id,checkout_id,conference_id,type,email,platform) VALUES('migration-student','migration-checkout',$1,'student','test@example.test','stripe')`, conf); err != nil {
		t.Fatal(err)
	}
	if n, e := MigrateDir(c, pool, source, nil); e != nil || n != 1 {
		t.Fatal("upgrade", n, e)
	}
	var price, cap int
	var description string
	var endIsNull bool
	if err = pool.QueryRow(c, `SELECT base_price,max_count,description,sales_end_at IS NULL FROM conference_tickets WHERE conference_id=$1 AND evergreen AND ticket_kind='student'`, conf).Scan(&price, &cap, &description, &endIsNull); err != nil {
		t.Fatal(err)
	}
	if price != 50 || cap != 100 || description != "Bring student ID" || !endIsNull {
		t.Fatalf("migrated offer %d %d %q %v", price, cap, description, endIsNull)
	}
	var mapped bool
	if err = pool.QueryRow(c, `SELECT t.evergreen AND t.ticket_kind='student' FROM registrations r JOIN conference_tickets t ON t.id=r.ticket_id WHERE r.ref_id='migration-student'`).Scan(&mapped); err != nil || !mapped {
		t.Fatal("prior student sales not mapped", err)
	}
	if n, e := MigrateDir(c, pool, source, nil); e != nil || n != 0 {
		t.Fatal("replay", n, e)
	}
}
