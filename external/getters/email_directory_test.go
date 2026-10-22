package getters

import (
	"btcpp-web/internal/mtypes"
	"context"
	"testing"
)

func TestEmailDirectoryPreferenceChangesPostgres(t *testing.T) {
	app := databaseSmokeContext(t)
	ctx := context.Background()
	email := "directory-" + databaseSmokeSuffix() + "@example.test"
	sub, err := NewSubscriberList(app, email, []string{"newsletter", "test-news"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { app.DB.Exec(ctx, `DELETE FROM subscribers WHERE email=$1`, email) })
	count := func(want int) {
		t.Helper()
		var got int
		if err := app.DB.QueryRow(ctx, `SELECT count(*) FROM email_preference_events e JOIN email_contacts c ON c.id=e.contact_id WHERE c.email=$1`, email).Scan(&got); err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Fatalf("preference events=%d want=%d", got, want)
		}
	}
	count(2)
	if err = UpdateSubs(app, sub); err != nil {
		t.Fatal(err)
	}
	count(2)
	sub.Subs = []*mtypes.Subscription{{Name: "test-news"}}
	if err = UpdateSubs(app, sub); err != nil {
		t.Fatal(err)
	}
	count(3)
	var status string
	if err = app.DB.QueryRow(ctx, `SELECT status FROM email_memberships m JOIN email_contacts c ON c.id=m.contact_id WHERE c.email=$1 AND m.newsletter='newsletter' AND m.service='btcpp'`, email).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "unsubscribed" {
		t.Fatalf("status=%s", status)
	}
	if err = UpdateSubs(app, sub); err != nil {
		t.Fatal(err)
	}
	count(3)
}
