package getters

import (
	"context"
	"errors"
	"sync"
	"testing"
)

func TestNewsletterPreferencesPostgres(t *testing.T) {
	app := databaseSmokeContext(t)
	ctx := context.Background()
	email := "newsletter-consent-" + databaseSmokeSuffix() + "@example.test"
	var person string
	if err := app.DB.QueryRow(ctx, `INSERT INTO people(name) VALUES('Newsletter test') RETURNING id::text`).Scan(&person); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		app.DB.Exec(ctx, `DELETE FROM people WHERE id=$1`, person)
		app.DB.Exec(ctx, `DELETE FROM subscribers WHERE email=$1`, email)
	})
	if _, err := app.DB.Exec(ctx, `INSERT INTO person_emails(person_id,email,is_primary) VALUES($1,$2,true)`, person, email); err != nil {
		t.Fatal(err)
	}
	set := func(key, name string, subscribed bool) error {
		return SetNewsletterPreference(app, person, "alerts", key, NewsletterPreference{Email: email, Newsletter: name, Subscribed: subscribed})
	}
	check := func(news, insider bool, count int) {
		t.Helper()
		prefs, err := ListNewsletterPreferences(app, person)
		if err != nil {
			t.Fatal(err)
		}
		if len(prefs) != 2 {
			t.Fatalf("preferences=%+v", prefs)
		}
		for _, p := range prefs {
			want := news
			if p.Newsletter == "insider" {
				want = insider
			}
			if p.Email != email || p.Subscribed != want {
				t.Fatalf("preferences=%+v", prefs)
			}
		}
		var got int
		if err := app.DB.QueryRow(ctx, `SELECT count(*) FROM newsletter_consent_requests WHERE person_id=$1`, person).Scan(&got); err != nil {
			t.Fatal(err)
		}
		if got != count {
			t.Fatalf("consent count=%d want=%d", got, count)
		}
	}
	check(false, false, 0)
	if err := set("subscribe-news-01", "newsletter", true); err != nil {
		t.Fatal(err)
	}
	if err := set("subscribe-ins-001", "insider", true); err != nil {
		t.Fatal(err)
	}
	check(true, true, 2)
	if err := set("unsubscribe-ins1", "insider", false); err != nil {
		t.Fatal(err)
	}
	// Replaying the old opt-in must preserve a more recent opt-out.
	if err := set("subscribe-ins-001", "insider", true); err != nil {
		t.Fatal(err)
	}
	check(true, false, 3)
	if err := set("subscribe-ins-001", "insider", false); !errors.Is(err, ErrNewsletterConflict) {
		t.Fatalf("conflict=%v", err)
	}
	if err := SetNewsletterPreference(app, person, "another-client", "subscribe-ins-001", NewsletterPreference{Email: email, Newsletter: "insider", Subscribed: true}); !errors.Is(err, ErrNewsletterConflict) {
		t.Fatalf("client conflict=%v", err)
	}
	if err := SetNewsletterPreference(app, person, "alerts", "unowned-email-01", NewsletterPreference{Email: "not-owned@example.test", Newsletter: "insider", Subscribed: true}); !errors.Is(err, ErrNewsletterEmail) {
		t.Fatalf("ownership error=%v", err)
	}
	// PostgreSQL rejects a NUL in the audit client after membership has been changed;
	// the transaction must roll that membership back along with the failed audit.
	if err := SetNewsletterPreference(app, person, "alerts\x00", "audit-failure-01", NewsletterPreference{Email: email, Newsletter: "insider", Subscribed: true}); err == nil {
		t.Fatal("expected audit insert failure")
	}
	check(true, false, 3)
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); errs <- set("concurrent-ins01", "insider", true) }()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	check(true, true, 4)
}
