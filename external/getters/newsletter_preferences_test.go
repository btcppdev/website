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
	if _, err := app.DB.Exec(ctx, `INSERT INTO person_emails(person_id,email,is_primary,verified_at) VALUES($1,$2,true,now())`, person, email); err != nil {
		t.Fatal(err)
	}
	set := func(key, name string, subscribed bool) error {
		return SetNewsletterPreference(app, person, "alerts", key, NewsletterPreference{Email: email, Newsletter: name, Subscribed: subscribed})
	}
	check := func(news bool, count int) {
		t.Helper()
		prefs, err := ListNewsletterPreferences(app, person)
		if err != nil {
			t.Fatal(err)
		}
		if len(prefs) != 1 {
			t.Fatalf("preferences=%+v", prefs)
		}
		for _, p := range prefs {
			want := news
			if p.Email != email || p.Newsletter != "newsletter" || p.Subscribed != want {
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
	check(false, 0)
	if err := set("subscribe-news-01", "newsletter", true); err != nil {
		t.Fatal(err)
	}
	if err := set("retired-list-0001", "insider", true); !errors.Is(err, ErrNewsletterInput) {
		t.Fatalf("retired list: %v", err)
	}
	check(true, 1)
	if err := set("unsubscribe-news1", "newsletter", false); err != nil {
		t.Fatal(err)
	}
	// Replaying the old opt-in must preserve a more recent opt-out.
	if err := set("subscribe-news-01", "newsletter", true); err != nil {
		t.Fatal(err)
	}
	check(false, 2)
	if err := set("subscribe-news-01", "newsletter", false); !errors.Is(err, ErrNewsletterConflict) {
		t.Fatalf("conflict=%v", err)
	}
	if err := SetNewsletterPreference(app, person, "another-client", "subscribe-news-01", NewsletterPreference{Email: email, Newsletter: "newsletter", Subscribed: true}); !errors.Is(err, ErrNewsletterConflict) {
		t.Fatalf("client conflict=%v", err)
	}
	if err := SetNewsletterPreference(app, person, "alerts", "unowned-email-01", NewsletterPreference{Email: "not-owned@example.test", Newsletter: "newsletter", Subscribed: true}); !errors.Is(err, ErrNewsletterEmail) {
		t.Fatalf("ownership error=%v", err)
	}
	// PostgreSQL rejects a NUL in the audit client after membership has been changed;
	// the transaction must roll that membership back along with the failed audit.
	if err := SetNewsletterPreference(app, person, "alerts\x00", "audit-failure-01", NewsletterPreference{Email: email, Newsletter: "newsletter", Subscribed: true}); err == nil {
		t.Fatal("expected audit insert failure")
	}
	check(false, 2)
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); errs <- set("concurrent-news01", "newsletter", true) }()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	check(true, 3)
}
