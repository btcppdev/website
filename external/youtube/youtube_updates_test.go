package youtube

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestWritableVideoStatusAlwaysAllowsEmbedding(t *testing.T) {
	publishAt := time.Date(2026, time.September, 8, 15, 30, 0, 0, time.FixedZone("CDT", -5*60*60))
	status := writableVideoStatus("private", publishAt)
	if !status.Embeddable {
		t.Fatal("writable YouTube status did not allow embedding")
	}
	payload, err := json.Marshal(status)
	if err != nil {
		t.Fatal(err)
	}
	got := string(payload)
	for _, want := range []string{`"embeddable":true`, `"privacyStatus":"private"`, `"publishAt":"2026-09-08T20:30:00Z"`} {
		if !strings.Contains(got, want) {
			t.Errorf("YouTube status payload missing %s: %s", want, got)
		}
	}

	unscheduled := writableVideoStatus("unlisted", time.Time{})
	if !unscheduled.Embeddable || unscheduled.PublishAt != "" {
		t.Fatalf("unscheduled YouTube status = %+v", unscheduled)
	}
}

func TestYouTubeMutationsAreBlockedWhenUpdatesDisabled(t *testing.T) {
	Init("", "", "", false)
	t.Cleanup(func() { Init("", "", "", false) })

	checks := []struct {
		name string
		run  func() error
	}{
		{"create playlist", func() error { _, err := CreatePlaylist(context.Background(), "title", "description"); return err }},
		{"add to playlist", func() error { return AddVideoToPlaylist(context.Background(), "playlist", "video") }},
		{"schedule video", func() error { return ScheduleExistingVideo(context.Background(), "video", time.Now().Add(time.Hour)) }},
		{"clear schedule", func() error { return ClearExistingVideoSchedule(context.Background(), "video") }},
		{"upload", func() error {
			_, err := Upload(context.Background(), UploadParams{Title: "title"}, strings.NewReader("video"), 5)
			return err
		}},
		{"thumbnail", func() error {
			return SetThumbnail(context.Background(), "video", "card.png", strings.NewReader("image"))
		}},
		{"thumbnail bytes", func() error { return SetThumbnailBytes(context.Background(), "video", "card.png", []byte("image")) }},
	}
	for _, check := range checks {
		t.Run(check.name, func(t *testing.T) {
			if err := check.run(); !errors.Is(err, ErrUpdatesDisabled) {
				t.Fatalf("error = %v, want ErrUpdatesDisabled", err)
			}
		})
	}
}

func TestYouTubeUpdatesEnabledState(t *testing.T) {
	Init("", "", "", true)
	if !UpdatesEnabled() {
		t.Fatal("expected updates enabled")
	}
	Init("", "", "", false)
	if UpdatesEnabled() {
		t.Fatal("expected updates disabled")
	}
}
