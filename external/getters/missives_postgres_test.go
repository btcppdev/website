package getters

import (
	"context"
	"fmt"
	"testing"
	"time"

	"btcpp-web/internal/mtypes"
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

func TestTemplatedMissiveDedupeKey(t *testing.T) {
	ctx := postgresSmokeContext(t)
	suffix := postgresSmokeSuffix()
	dedupeKey := "weekly-newsletter:test-" + suffix
	t.Cleanup(func() {
		_, _ = ctx.DB.Exec(context.Background(), `DELETE FROM missives WHERE dedupe_key = $1`, dedupeKey)
	})

	input := MissiveInput{
		Title:       "Weekly dedupe test " + suffix,
		Markdown:    "Test body",
		SendAt:      "2026-08-11T10:00:00-05:00",
		Newsletters: []string{"newsletter"},
		DedupeKey:   dedupeKey,
	}
	created, err := CreateTemplatedMissive(ctx, input)
	if err != nil {
		t.Fatalf("CreateTemplatedMissive: %v", err)
	}
	found, err := GetTemplatedLetterByDedupeKey(ctx, dedupeKey)
	if err != nil {
		t.Fatalf("GetTemplatedLetterByDedupeKey: %v", err)
	}
	if found == nil || found.UID != created.UID {
		t.Fatalf("found = %#v, want MISS-%d", found, created.UID)
	}
	if _, err := CreateTemplatedMissive(ctx, input); err == nil {
		t.Fatal("second CreateTemplatedMissive succeeded with the same dedupe key")
	}
}

func TestDeleteTemplatedDraftOnlyDeletesUnsent(t *testing.T) {
	ctx := postgresSmokeContext(t)
	suffix := postgresSmokeSuffix()
	makeDraft := func(title string) *mtypes.Letter {
		draft, err := CreateTemplatedMissive(ctx, MissiveInput{
			Title:       title,
			Markdown:    "Delete guard test",
			SendAt:      "now",
			Newsletters: []string{"newsletter"},
		})
		if err != nil {
			t.Fatalf("CreateTemplatedMissive: %v", err)
		}
		t.Cleanup(func() {
			_, _ = ctx.DB.Exec(context.Background(), `DELETE FROM missives WHERE id = $1`, draft.PageID)
		})
		return draft
	}

	unsent := makeDraft("Unsent delete test " + suffix)
	deleted, err := DeleteTemplatedDraft(ctx, unsent.UID)
	if err != nil || !deleted {
		t.Fatalf("DeleteTemplatedDraft(unsent) = %v, %v", deleted, err)
	}

	sent := makeDraft("Sent delete test " + suffix)
	if err := MarkLetterSent(ctx, sent, time.Now()); err != nil {
		t.Fatalf("MarkLetterSent: %v", err)
	}
	deleted, err = DeleteTemplatedDraft(ctx, sent.UID)
	if err != nil {
		t.Fatalf("DeleteTemplatedDraft(sent): %v", err)
	}
	if deleted {
		t.Fatal("DeleteTemplatedDraft deleted a sent missive")
	}
}

func TestUpdateOnlyForMissiveEditsReusableTemplateOnly(t *testing.T) {
	ctx := postgresSmokeContext(t)
	suffix := postgresSmokeSuffix()
	letter, err := insertMissivePostgres(ctx, MissiveInput{
		Title:       "Original " + suffix,
		Markdown:    "Hi {{ .Email }}",
		Newsletters: []string{},
		OnlyFor:     "smoke-inline-" + suffix,
	})
	if err != nil {
		t.Fatalf("insert reusable missive: %v", err)
	}
	t.Cleanup(func() {
		_, _ = ctx.DB.Exec(context.Background(), `DELETE FROM missives WHERE id = $1::uuid`, letter.PageID)
	})
	if err := UpdateOnlyForMissive(ctx, letter.PageID, "Updated {{ .Email }}", "Hello {{ .URI }}"); err != nil {
		t.Fatalf("UpdateOnlyForMissive: %v", err)
	}
	updated, err := GetLetter(ctx, letter.UID)
	if err != nil {
		t.Fatalf("GetLetter: %v", err)
	}
	if updated.Title != "Updated {{ .Email }}" || updated.Markdown != "Hello {{ .URI }}" {
		t.Fatalf("updated reusable missive = %#v", updated)
	}

	templated, err := CreateTemplatedMissive(ctx, MissiveInput{
		Title: "Protected " + suffix, Markdown: "body", Newsletters: []string{"newsletter"},
	})
	if err != nil {
		t.Fatalf("CreateTemplatedMissive: %v", err)
	}
	t.Cleanup(func() {
		_, _ = ctx.DB.Exec(context.Background(), `DELETE FROM missives WHERE id = $1::uuid`, templated.PageID)
	})
	if err := UpdateOnlyForMissive(ctx, templated.PageID, "Should fail", "body"); err == nil {
		t.Fatal("UpdateOnlyForMissive updated a newsletter-builder missive")
	}
}

func TestWeeklyNewsletterUpdatesQueriesCurrentSchema(t *testing.T) {
	ctx := postgresSmokeContext(t)
	updates, err := WeeklyNewsletterUpdates(ctx, time.Now().Add(24*time.Hour))
	if err != nil {
		t.Fatalf("WeeklyNewsletterUpdates: %v", err)
	}
	if updates == nil {
		t.Fatal("WeeklyNewsletterUpdates returned nil without an error")
	}
	if updates.TalkOfWeek == nil {
		return
	}
	letter, err := CreateWeeklyNewsletterMissive(ctx, MissiveInput{
		Title:       "Weekly feature smoke " + postgresSmokeSuffix(),
		Markdown:    "feature smoke",
		SendAt:      "now",
		Newsletters: []string{"newsletter"},
	}, updates.TalkOfWeek.TalkID)
	if err != nil {
		t.Fatalf("CreateWeeklyNewsletterMissive: %v", err)
	}
	t.Cleanup(func() {
		_, _ = ctx.DB.Exec(context.Background(), `DELETE FROM missives WHERE id = $1::uuid`, letter.PageID)
	})
	var recordedTalkID string
	if err := ctx.DB.QueryRow(ctx.DatabaseContext(), `
		SELECT conf_talk_id::text
		FROM weekly_newsletter_featured_talks
		WHERE missive_id = $1::uuid
	`, letter.PageID).Scan(&recordedTalkID); err != nil {
		t.Fatalf("query recorded weekly feature: %v", err)
	}
	if recordedTalkID != updates.TalkOfWeek.TalkID {
		t.Fatalf("recorded Talk of the Week = %q, want %q", recordedTalkID, updates.TalkOfWeek.TalkID)
	}
}

func TestWeeklyNewsletterTalkWindowNeverIncludesFuturePublications(t *testing.T) {
	builtAt := time.Date(2026, time.August, 10, 15, 0, 0, 0, time.UTC)
	issueSendAt := time.Date(2026, time.August, 11, 15, 0, 0, 0, time.UTC)

	start, end := weeklyNewsletterTalkWindow(issueSendAt, builtAt)
	if !end.Equal(builtAt) {
		t.Fatalf("talk window ends at %s, want draft build time %s", end, builtAt)
	}
	if want := builtAt.AddDate(0, 0, -7); !start.Equal(want) {
		t.Fatalf("talk window starts at %s, want %s", start, want)
	}
}
