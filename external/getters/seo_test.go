package getters

import (
	"context"
	"testing"
)

func TestDatabaseSmokePublicProjectSitemap(t *testing.T) {
	app := databaseSmokeContext(t)
	conf, _ := insertSmokeConference(t, app)
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := app.DB.Exec(context.Background(), q, args...); err != nil {
			t.Fatal(err)
		}
	}
	scalar := func(q string, args ...any) string {
		t.Helper()
		var id string
		if err := app.DB.QueryRow(context.Background(), q, args...).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	exec(`UPDATE conferences SET publication_status='published' WHERE id=$1`, conf)
	competition := scalar(`INSERT INTO competitions(conference_id,title,visibility,public_gallery_enabled) VALUES($1,'SEO test','public',true) RETURNING id::text`, conf)
	submitted := scalar(`INSERT INTO projects(competition_id,slug,title,status) VALUES($1,'public','Public','submitted') RETURNING id::text`, competition)
	exec(`INSERT INTO projects(competition_id,slug,title,status) VALUES($1,'draft','Draft','created'),($1,'hidden','Hidden','hidden')`, competition)
	check := func(want bool) {
		t.Helper()
		urls, err := PublicProjectSitemapURLs(app)
		if err != nil {
			t.Fatal(err)
		}
		if want {
			if len(urls[conf]) != 1 || urls[conf][0] != submitted {
				t.Fatalf("wrong public projects: %v", urls[conf])
			}
		} else if len(urls[conf]) != 0 {
			t.Fatalf("private content exposed: %v", urls[conf])
		}
	}
	check(true)
	exec(`UPDATE competitions SET public_gallery_enabled=false WHERE id=$1`, competition)
	check(false)
	exec(`UPDATE competitions SET public_gallery_enabled=true,visibility='hidden' WHERE id=$1`, competition)
	check(false)
	exec(`UPDATE competitions SET visibility='public' WHERE id=$1`, competition)
	exec(`UPDATE conferences SET publication_status='draft' WHERE id=$1`, conf)
	check(false)
}
