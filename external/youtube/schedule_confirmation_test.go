package youtube

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestScheduleConfirmationRetriesStaleReads(t *testing.T) {
	at := time.Now().Add(time.Hour)
	calls := 0
	err := waitForVideoSchedule(context.Background(), "video", &at, []time.Duration{0, 0, 0}, func(context.Context, string) (*VideoStatus, error) {
		calls++
		if calls == 1 {
			return nil, errors.New("temporary read failure")
		}
		if calls == 2 {
			return &VideoStatus{PrivacyStatus: "unlisted"}, nil
		}
		return &VideoStatus{PrivacyStatus: "private", PublishAt: &at}, nil
	})
	if err != nil || calls != 3 {
		t.Fatalf("calls=%d error=%v", calls, err)
	}
}

func TestScheduleConfirmationRequiresExactResult(t *testing.T) {
	at := time.Now().Add(time.Hour)
	other := at.Add(time.Minute)
	for _, test := range []struct {
		name   string
		target *time.Time
		status *VideoStatus
	}{
		{"wrong date", &at, &VideoStatus{PrivacyStatus: "private", PublishAt: &other}},
		{"no date", &at, &VideoStatus{PrivacyStatus: "private"}},
		{"public video", &at, &VideoStatus{PrivacyStatus: "public", PublishAt: &at}},
		{"clear still private", nil, &VideoStatus{PrivacyStatus: "private"}},
		{"clear still scheduled", nil, &VideoStatus{PrivacyStatus: "unlisted", PublishAt: &at}},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := waitForVideoSchedule(context.Background(), "video", test.target, []time.Duration{0, 0}, func(context.Context, string) (*VideoStatus, error) { return test.status, nil })
			if err == nil {
				t.Fatal("unconfirmed operation accepted")
			}
		})
	}
	if !(&VideoStatus{PrivacyStatus: "unlisted"}).MatchesSchedule(nil) {
		t.Fatal("cleared schedule rejected")
	}
}

func TestScheduleConfirmationStopsOnCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := waitForVideoSchedule(ctx, "video", nil, []time.Duration{time.Hour}, func(context.Context, string) (*VideoStatus, error) {
		t.Fatal("read after cancellation")
		return nil, nil
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
