package youtube

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

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
