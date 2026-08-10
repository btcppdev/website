package getters

import (
	"context"
	"fmt"
	"testing"
)

func TestListAdminSubscribers(t *testing.T) {
	ctx := postgresSmokeContext(t)
	suffix := postgresSmokeSuffix()
	emails := []string{
		fmt.Sprintf("admin-subs-%s-news@example.test", suffix),
		fmt.Sprintf("admin-subs-%s-active@example.test", suffix),
		fmt.Sprintf("admin-subs-%s-inactive@example.test", suffix),
	}
	t.Cleanup(func() {
		_, _ = ctx.DB.Exec(context.Background(), `DELETE FROM subscribers WHERE email = ANY($1::citext[])`, emails)
	})

	before, err := ListAdminSubscribers(ctx, "", "", "all", 1, 0)
	if err != nil {
		t.Fatalf("ListAdminSubscribers before: %v", err)
	}
	if _, err := NewSubscriberList(ctx, emails[0], []string{"newsletter", "insider"}); err != nil {
		t.Fatalf("create newsletter subscriber: %v", err)
	}
	if _, err := NewSubscriberList(ctx, emails[1], []string{"insider"}); err != nil {
		t.Fatalf("create active subscriber: %v", err)
	}
	if _, err := NewSubscriberList(ctx, emails[2], nil); err != nil {
		t.Fatalf("create inactive subscriber: %v", err)
	}

	search := "admin-subs-" + suffix
	all, err := ListAdminSubscribers(ctx, search, "", "all", 25, 0)
	if err != nil {
		t.Fatalf("ListAdminSubscribers all: %v", err)
	}
	if all.TotalFiltered != 3 || len(all.Subscribers) != 3 {
		t.Fatalf("all subscribers = total:%d rows:%d, want 3", all.TotalFiltered, len(all.Subscribers))
	}
	if all.Summary.TotalStored != before.Summary.TotalStored+3 || all.Summary.ActiveAny != before.Summary.ActiveAny+2 || all.Summary.NewsletterActive != before.Summary.NewsletterActive+1 || all.Summary.Inactive != before.Summary.Inactive+1 {
		t.Fatalf("summary delta = before:%+v after:%+v", before.Summary, all.Summary)
	}

	newsletter, err := ListAdminSubscribers(ctx, search, "newsletter", "all", 25, 0)
	if err != nil {
		t.Fatalf("ListAdminSubscribers newsletter: %v", err)
	}
	if newsletter.TotalFiltered != 1 || len(newsletter.Subscribers) != 1 || newsletter.Subscribers[0].Email != emails[0] {
		t.Fatalf("newsletter filter = %+v", newsletter.Subscribers)
	}

	inactive, err := ListAdminSubscribers(ctx, search, "", "inactive", 25, 0)
	if err != nil {
		t.Fatalf("ListAdminSubscribers inactive: %v", err)
	}
	if inactive.TotalFiltered != 1 || len(inactive.Subscribers) != 1 || inactive.Subscribers[0].Email != emails[2] {
		t.Fatalf("inactive filter = %+v", inactive.Subscribers)
	}
}
