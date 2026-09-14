package getters

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
	"testing"
	"time"
)

func TestJudgingNotificationsIgnoreNoOpReadsAndRollback(t *testing.T) {
	f := newMergeAccountsFixture(t)
	c := context.Background()
	if err := ReplaceCompetitionScheduleSegments(f.app, f.competition, []CompetitionScheduleSegmentInput{{SegmentType: JudgeTypeExpo, Title: "Expo", DefaultDurationMinutes: 60}}); err != nil {
		t.Fatal(err)
	}
	listener, err := pgx.ConnectConfig(c, f.app.DB.Config().ConnConfig.Copy())
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close(c)
	if _, err = listener.Exec(c, "LISTEN btcpp_judging_results"); err != nil {
		t.Fatal(err)
	}
	events, err := ListJudgeEvents(f.app, f.competition)
	if err != nil {
		t.Fatal(err)
	}
	expectQuiet := func() {
		t.Helper()
		ctx, cancel := context.WithTimeout(c, 150*time.Millisecond)
		defer cancel()
		if _, err := listener.WaitForNotification(ctx); !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("unexpected notification/error after no-op or rollback: %v", err)
		}
	}
	expectQuiet() // ListJudgeEvents synchronizes metadata, but results did not change.
	tx, err := f.app.DB.Begin(c)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(c, `UPDATE judge_events SET rank_limit=rank_limit+1 WHERE id=$1`, events[0].ID); err != nil {
		t.Fatal(err)
	}
	if err = tx.Rollback(c); err != nil {
		t.Fatal(err)
	}
	expectQuiet()
	if _, err = f.app.DB.Exec(c, `UPDATE judge_events SET rank_limit=rank_limit+1 WHERE id=$1`, events[0].ID); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(c, 3*time.Second)
	defer cancel()
	notification, err := listener.WaitForNotification(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if notification.Channel != "btcpp_judging_results" || notification.Payload != "changed" {
		t.Fatalf("notification: %+v", notification)
	}
}
